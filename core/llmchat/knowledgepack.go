package llmchat

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Substituter produces the text blob that replaces a single
// `{{token}}` placeholder in the system prompt. Substituters are
// invoked at most once per cache TTL window (default 5 minutes); the
// resulting blob is cached so per-turn LLM calls do not pay disk IO.
type Substituter func(ctx context.Context) (string, error)

// Knowledge-pack constants. Pinned wire/config defaults; renaming
// breaks the operator's runtime config.
const (
	// defaultKnowledgePackBudget is the per-substituted-blob token cap.
	// 65536 tokens ≈ 256KB UTF-8 by the 4-bytes-per-token rule of
	// thumb. The cap protects the LLM's context window — exceeding it
	// would push real conversation messages out of context.
	defaultKnowledgePackBudget = 65536

	// envKnowledgePackBudget overrides the default per-blob cap.
	envKnowledgePackBudget = "LLMCHAT_KNOWLEDGE_PACK_BUDGET"

	// defaultKnowledgePackTTL is how long each substituter's output
	// stays cached before a fresh call. SIGHUP forces an immediate
	// refresh regardless of TTL.
	defaultKnowledgePackTTL = 5 * time.Minute

	// approximateBytesPerToken is the budget-to-bytes conversion. Real
	// tokenisation depends on the model; for budgeting we assume 4
	// bytes/token which holds for ASCII-heavy content.
	approximateBytesPerToken = 4
)

// KnowledgePackLoader wraps a base PromptLoader (typically the
// filePromptLoader from prompt.go) and substitutes registered
// `{{token}}` placeholders with locally-sourced content blobs.
//
// Phase-4 placeholder pass-through is preserved when no substituters
// are registered for a given token — this is rail #1 (substituters
// WRAP, never REPLACE the prompt loader).
type KnowledgePackLoader struct {
	inner  PromptLoader
	budget int
	ttl    time.Duration

	mu    sync.RWMutex
	subs  map[string]Substituter
	cache map[string]knowledgePackCacheEntry

	nowFn func() time.Time
}

type knowledgePackCacheEntry struct {
	value    string
	loadedAt time.Time
}

// KPOption configures a KnowledgePackLoader at construction time.
type KPOption func(*KnowledgePackLoader)

// WithBudget overrides the per-blob token budget. Pass a non-positive
// value to keep the default.
func WithBudget(budget int) KPOption {
	return func(l *KnowledgePackLoader) {
		if budget > 0 {
			l.budget = budget
		}
	}
}

// WithTTL overrides the cache TTL. Pass a non-positive value to keep
// the default.
func WithTTL(ttl time.Duration) KPOption {
	return func(l *KnowledgePackLoader) {
		if ttl > 0 {
			l.ttl = ttl
		}
	}
}

// WithNowFn injects a clock for deterministic cache-expiry tests.
func WithNowFn(now func() time.Time) KPOption {
	return func(l *KnowledgePackLoader) {
		if now != nil {
			l.nowFn = now
		}
	}
}

// NewKnowledgePackLoader wraps an inner PromptLoader. The budget is
// resolved from the env var LLMCHAT_KNOWLEDGE_PACK_BUDGET (with the
// default fallback when unset/invalid) unless WithBudget is supplied.
func NewKnowledgePackLoader(inner PromptLoader, opts ...KPOption) *KnowledgePackLoader {
	l := &KnowledgePackLoader{
		inner:  inner,
		budget: resolveBudgetFromEnv(),
		ttl:    defaultKnowledgePackTTL,
		subs:   make(map[string]Substituter),
		cache:  make(map[string]knowledgePackCacheEntry),
		nowFn:  time.Now,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

// resolveBudgetFromEnv parses LLMCHAT_KNOWLEDGE_PACK_BUDGET. Invalid
// values fall back to the default with a slog.Warn so the operator
// notices but the service stays operational (rail #3 — never disable
// the budget in code).
func resolveBudgetFromEnv() int {
	raw := os.Getenv(envKnowledgePackBudget)
	if raw == "" {
		return defaultKnowledgePackBudget
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		slog.Warn("llmchat/knowledgepack: invalid LLMCHAT_KNOWLEDGE_PACK_BUDGET; using default",
			"raw", raw, "default", defaultKnowledgePackBudget)
		return defaultKnowledgePackBudget
	}
	return v
}

// Register binds a Substituter to a placeholder token. Tokens are the
// inner string of `{{...}}` (e.g. `api_summary` for `{{api_summary}}`).
// Re-registering an existing token replaces the previous substituter.
func (l *KnowledgePackLoader) Register(token string, sub Substituter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subs[token] = sub
	delete(l.cache, token) // re-registration invalidates cache
}

// Load returns the inner prompt with all registered placeholders
// substituted from the (cached) substituter outputs. Tokens with no
// registered substituter pass through unchanged.
func (l *KnowledgePackLoader) Load(ctx context.Context) (string, error) {
	base, err := l.inner.Load(ctx)
	if err != nil {
		return "", err
	}

	// Snapshot the token set under the read lock so we don't hold the
	// lock during substituter calls (which may do disk IO).
	l.mu.RLock()
	tokens := make([]string, 0, len(l.subs))
	for t := range l.subs {
		tokens = append(tokens, t)
	}
	l.mu.RUnlock()

	for _, token := range tokens {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		value, err := l.valueFor(ctx, token)
		if err != nil {
			return "", err
		}
		base = strings.ReplaceAll(base, "{{"+token+"}}", value)
	}
	return base, nil
}

// valueFor returns the cached value for a token, calling the
// substituter on cache miss/expiry. The lookup is RLock-guarded for
// the fast path and re-checks under Lock to avoid a thundering herd
// of substituter calls under cache stampede.
func (l *KnowledgePackLoader) valueFor(ctx context.Context, token string) (string, error) {
	now := l.nowFn()

	// Fast path: read-locked cache hit.
	l.mu.RLock()
	if entry, ok := l.cache[token]; ok && now.Sub(entry.loadedAt) < l.ttl {
		l.mu.RUnlock()
		return entry.value, nil
	}
	sub, ok := l.subs[token]
	l.mu.RUnlock()
	if !ok {
		return "", nil
	}

	// Slow path: write lock + double-check.
	l.mu.Lock()
	if entry, ok := l.cache[token]; ok && now.Sub(entry.loadedAt) < l.ttl {
		l.mu.Unlock()
		return entry.value, nil
	}
	l.mu.Unlock()

	raw, err := sub(ctx)
	if err != nil {
		return "", err
	}
	value := l.applyBudget(token, raw)

	l.mu.Lock()
	l.cache[token] = knowledgePackCacheEntry{value: value, loadedAt: now}
	l.mu.Unlock()
	return value, nil
}

// applyBudget truncates a substituted blob to the configured cap and
// emits a slog.Warn when truncation occurs.
func (l *KnowledgePackLoader) applyBudget(token, raw string) string {
	maxBytes := l.budget * approximateBytesPerToken
	if len(raw) <= maxBytes {
		return raw
	}
	slog.Warn("llmchat/knowledgepack: budget_truncated",
		"token", token,
		"original_bytes", len(raw),
		"budget_bytes", maxBytes)
	return raw[:maxBytes]
}

// RefreshAll invalidates every cached substituter output. The next
// Load() call will re-invoke each substituter. Bound to SIGHUP via
// knowledgepack_signal_unix.go on POSIX targets; the Windows build
// supplies an empty stub (no SIGHUP signal class).
func (l *KnowledgePackLoader) RefreshAll() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for token := range l.cache {
		slog.Info("llmchat/knowledgepack: cache_refreshed", "token", token)
	}
	l.cache = make(map[string]knowledgePackCacheEntry)
}
