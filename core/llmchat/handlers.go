package llmchat

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisPinger is the slice of *redis.Client we exercise in /readyz.
// Splitting the dependency out of the concrete type lets tests pass a
// miniredis-backed client without dragging the full Redis surface into
// the handler tests; production wiring still uses *redis.Client.
type redisPinger interface {
	Ping(ctx context.Context) *redis.StatusCmd
}

// Handlers exposes the cordum-llm-chat process-level HTTP handlers
// (/healthz, /readyz). Phase 1 of epic-ac495830 keeps the surface
// intentionally small — chat endpoints, session admin, and audit
// emitters land in follow-up tasks.
type Handlers struct {
	provider       Provider
	redis          redisPinger
	knowledgeCheck func(context.Context) error
	timeout        time.Duration
}

// HandlerOption customizes process-level health/readiness handlers.
type HandlerOption func(*Handlers)

// WithKnowledgeCheck wires the local knowledge-pack accessibility check used by
// /healthz and /readyz. A nil check means knowledge-pack checks are disabled
// (for tests or deployments that deliberately run without the pack).
func WithKnowledgeCheck(check func(context.Context) error) HandlerOption {
	return func(h *Handlers) {
		h.knowledgeCheck = check
	}
}

// NewHandlers wires a Handlers from its dependencies. The timeout
// caps each individual readiness probe so a slow vLLM cannot stall
// the reverse proxy in front of the service.
func NewHandlers(provider Provider, redisClient *redis.Client, probeTimeout time.Duration, opts ...HandlerOption) *Handlers {
	if probeTimeout <= 0 {
		probeTimeout = 2 * time.Second
	}
	h := &Handlers{
		provider: provider,
		redis:    redisClient,
		timeout:  probeTimeout,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// healthBody is the payload returned by /healthz.
type healthBody struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// readyBody is the payload returned by /readyz. The fields are pinned
// because the dashboard chat-button availability gate (epic rail #5)
// keys off the `vllm` field — renaming it would silently break the
// widget's hide-on-unhealthy logic.
type readyBody struct {
	Status    string `json:"status"`
	Redis     string `json:"redis,omitempty"`
	Vllm      string `json:"vllm"`
	Knowledge string `json:"knowledge,omitempty"`
}

// Livez reports process liveness. It does not consult dependencies so a
// transient backend hiccup does not flap the liveness probe and force a pod
// restart.
func (h *Handlers) Livez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthBody{
		Status:  "ok",
		Service: "cordum-llm-chat",
	})
}

// Healthz reports the user-facing chat surface health. It fails closed when the
// inference backend is unavailable or when the local knowledge pack cannot be
// read. Redis is intentionally left to /readyz so operators can distinguish
// process+product health from full traffic-readiness.
func (h *Handlers) Healthz(w http.ResponseWriter, r *http.Request) {
	body := readyBody{Status: "ok", Vllm: "ok", Knowledge: "ok"}
	h.probeProviderAndKnowledge(r.Context(), &body)
	writeDependencyBody(w, body)
}

// Readyz reports readiness. It probes Redis and the LLM provider in
// parallel under a per-probe deadline; if either fails the service
// reports 503 so upstreams can drain traffic. The body's `redis` and
// `vllm` fields carry the per-component result so an operator can
// see which dependency is degraded.
func (h *Handlers) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	body := readyBody{Status: "ok", Redis: "ok", Vllm: "ok", Knowledge: "ok"}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		redisErr error
	)

	if h.redis != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()
			if err := h.redis.Ping(probeCtx).Err(); err != nil {
				mu.Lock()
				redisErr = err
				mu.Unlock()
			}
		}()
	} else {
		body.Redis = "fail: redis client not configured"
	}

	h.probeProviderAndKnowledge(ctx, &body)
	wg.Wait()

	if redisErr != nil {
		body.Redis = "fail: " + redisErr.Error()
	}

	writeDependencyBody(w, body)
}

func (h *Handlers) probeProviderAndKnowledge(ctx context.Context, body *readyBody) {
	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		providerErr  error
		knowledgeErr error
	)

	if h.provider != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()
			if err := h.provider.HealthCheck(probeCtx); err != nil {
				mu.Lock()
				providerErr = err
				mu.Unlock()
			}
		}()
	} else {
		body.Vllm = "fail: provider not configured"
	}

	if h.knowledgeCheck != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			probeCtx, cancel := context.WithTimeout(ctx, h.timeout)
			defer cancel()
			if err := h.knowledgeCheck(probeCtx); err != nil {
				mu.Lock()
				knowledgeErr = err
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if providerErr != nil {
		body.Vllm = "fail: " + providerErr.Error()
	}
	if knowledgeErr != nil {
		body.Knowledge = "fail: " + knowledgeErr.Error()
	}
}

func writeDependencyBody(w http.ResponseWriter, body readyBody) {
	status := http.StatusOK
	if (body.Redis != "" && body.Redis != "ok") || body.Vllm != "ok" || body.Knowledge != "ok" {
		status = http.StatusServiceUnavailable
		body.Status = "degraded"
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Warn("llmchat: encode response failed", "error", err)
	}
}
