package llmchat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"
)

// PromptLoader supplies the system prompt that grounds the LLM in
// Cordum's domain at the start of every Turn.
type PromptLoader interface {
	Load(ctx context.Context) (string, error)
}

// promptCacheTTL is how long the file-loader keeps a successful read
// in memory before re-reading from disk. Operators editing the prompt
// see updates within 5 minutes without restart.
const promptCacheTTL = 5 * time.Minute

// promptPathEnv overrides the default path. Empty = use defaultPromptPath.
const (
	promptPathEnv     = "LLMCHAT_SYSTEM_PROMPT_PATH"
	defaultPromptPath = "config/llmchat/system-prompt.md"
)

// filePromptLoader reads the system prompt from a file with a 5-minute
// cache. Missing/empty file falls back to DefaultSystemPrompt() so the
// service stays operational on a fresh deploy before the prompt file is
// shipped.
type filePromptLoader struct {
	path string

	mu       sync.RWMutex
	cached   string
	loadedAt time.Time
}

// NewFilePromptLoader constructs a file-backed loader. Pass an empty
// path to consult the LLMCHAT_SYSTEM_PROMPT_PATH env var (with a
// project-default fallback).
func NewFilePromptLoader(path string) PromptLoader {
	if path == "" {
		path = os.Getenv(promptPathEnv)
	}
	if path == "" {
		path = defaultPromptPath
	}
	return &filePromptLoader{path: path}
}

// Load returns the cached prompt when fresh; otherwise reads from disk
// and refreshes the cache. File missing/empty → DefaultSystemPrompt()
// + slog.Warn (so operators notice but service stays up).
//
// Template placeholder pass-through (per Yaron 2026-04-25 directive
// "LLM should know all API + cordum.io info"): the loader does NOT
// substitute `{{api_summary}}` or `{{cordum_io_summary}}` tokens.
// A separately-filed knowledge-pack task (task-558966d0 et al.)
// owns the substituters that read the OpenAPI spec + Coretex-site
// MDX content. Pass-through here keeps the loader minimal until
// those readers ship.
func (l *filePromptLoader) Load(ctx context.Context) (string, error) {
	l.mu.RLock()
	if l.cached != "" && time.Since(l.loadedAt) < promptCacheTTL {
		out := l.cached
		l.mu.RUnlock()
		return out, nil
	}
	l.mu.RUnlock()

	if err := ctx.Err(); err != nil {
		return "", err
	}

	body, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Warn("llmchat: system prompt file missing, using default",
				"path", l.path,
				"hint", "phase 8 ships config/llmchat/system-prompt.md")
			return DefaultSystemPrompt(), nil
		}
		return "", fmt.Errorf("llmchat/prompt: read %s: %w", l.path, err)
	}
	text := string(body)
	if text == "" {
		slog.Warn("llmchat: system prompt file empty, using default", "path", l.path)
		return DefaultSystemPrompt(), nil
	}

	l.mu.Lock()
	l.cached = text
	l.loadedAt = time.Now()
	l.mu.Unlock()
	return text, nil
}

// DefaultSystemPrompt is the safe fallback used when the configured prompt file
// is missing or empty. It reflects the 2026-04-28 informational-only scope:
// answer configuration/docs/API questions from local context and do not invoke
// tools or mutate Cordum state.
func DefaultSystemPrompt() string {
	return `You are the Cordum chat assistant.

Scope: informational Q&A only. Answer questions about Cordum's API, configuration, workflow concepts, approval gates, troubleshooting, and cordum.io documentation using the local knowledge context below. Do not call tools, do not submit jobs, do not approve or reject work, and do not mutate Cordum state. If a user asks you to change state, explain the relevant CLI or dashboard path instead.

Knowledge context:

{{api_summary}}

{{cordum_io_summary}}

Safety rules:
- Never invent job IDs, workflow IDs, policy names, API fields, or configuration keys. If the provided context is insufficient, say what is missing and where the operator should verify it.
- Never echo secrets or credentials. Treat API keys, passwords, bearer tokens, JWTs, and private certificates as <redacted>.
- Prefer concise, actionable answers with exact endpoint names, config keys, or dashboard paths when they are present in the knowledge context.`
}
