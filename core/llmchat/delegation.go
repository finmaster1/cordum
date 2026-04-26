// Package-level delegation client for the chat-assistant service.
//
// The cordum-llm-chat process holds the service-account API key
// (`X-API-Key`) but MUST NOT use it on per-tool-call paths (epic rail
// #8 + task rail #4). Instead, every chat session mints a per-user
// child delegation token (chain depth 1, EdDSA JWT, 15-minute default
// TTL) via the gateway's `POST /api/v1/agents/{id}/delegate` endpoint.
// CallTool then carries that token in `Authorization: Bearer ...`.
//
// User-principal threading into the delegation chain is phase-4's
// concern: this client exposes the API and does the self-delegation
// for chain_depth=1; the chat loop wires the per-WS-connection user
// principal as the subject claim when it enters the loop.
package llmchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrDelegationUnauthorized is the typed error surfaced for a 401 from
// the gateway's delegation endpoint. Common cause: the service API key
// is missing/expired or the chat-assistant agent identity is not
// registered yet (bootstrap.go covers the latter on first boot).
var ErrDelegationUnauthorized = errors.New("llmchat/delegation: unauthorized")

// ErrDelegationForbidden is the typed error surfaced for a 403 from
// the gateway. Distinct from Unauthorized so the operator can
// distinguish "wrong key" from "key valid but action denied".
var ErrDelegationForbidden = errors.New("llmchat/delegation: forbidden")

// SessionDelegation is the issued-token record stored alongside a chat
// session. The chat loop consults `Token` for outbound CallTool
// requests; `JTI` is recorded to the session log for revoke-on-close;
// `ExpiresAt` drives the auto-refresh threshold.
type SessionDelegation struct {
	Token      string
	JTI        string
	ExpiresAt  time.Time
	ChainDepth int
}

// DelegationConfig is the boot-time wiring for DelegationClient. The
// caller fills it from env vars in main.go.
type DelegationConfig struct {
	BaseURL string
	// AgentID is the chat-assistant agent identity (the delegating
	// agent). The same id is also the target — this is a self-
	// delegation for chain-depth-1.
	AgentID string
	// APIKey is the service-account credential carried as `X-API-Key`
	// to /agents/{id}/delegate. NEVER forwarded to MCP CallTool.
	APIKey string
	// Tenant is forwarded as `X-Cordum-Tenant` so the gateway resolves
	// the tenant before the rate-limit + audit code paths see it.
	Tenant string
	// IssueTTL is the requested delegation-token TTL. Plan default
	// 15min (900s); the gateway clamps to its own max.
	IssueTTL time.Duration
	// RetryDelay is the initial 5xx retry backoff. Doubles each attempt
	// up to 3 attempts. Tests inject 1ms; production uses 100ms.
	RetryDelay time.Duration
	// HTTPClient lets tests inject a transport. nil = http.DefaultClient
	// with explicit per-request deadlines.
	HTTPClient *http.Client
}

// DelegationClient mints + revokes per-session delegation JWTs against
// the cordum gateway. One client per cordum-llm-chat process; safe to
// share across goroutines (the per-principal cache is mutex-guarded).
type DelegationClient struct {
	cfg        DelegationConfig
	httpClient *http.Client

	mu    sync.Mutex
	cache map[string]SessionDelegation // keyed by user principal
}

// NewDelegationClient validates cfg + applies defaults. BaseURL/AgentID/
// APIKey are required at deploy-time but not enforced here so tests can
// run without them.
func NewDelegationClient(cfg DelegationConfig) *DelegationClient {
	if cfg.IssueTTL <= 0 {
		cfg.IssueTTL = 15 * time.Minute
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 100 * time.Millisecond
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &DelegationClient{
		cfg:        cfg,
		httpClient: httpClient,
		cache:      make(map[string]SessionDelegation),
	}
}

// IssueForSession mints a new delegation token. The user principal is
// carried as a header (`X-Cordum-User-Principal`) so the gateway audit
// records who the chat-assistant acted on behalf of; the token itself
// is a self-delegation for chain-depth-1 — phase 4 will plumb the
// user's session JWT into ParentToken once that wire is laid.
func (c *DelegationClient) IssueForSession(ctx context.Context, userPrincipal string) (SessionDelegation, error) {
	body := map[string]any{
		"target_agent_id": c.cfg.AgentID,
		"allowed_actions": []string{"read", "submit_job", "query_policy"},
		"allowed_topics":  []string{"job.*"},
		"ttl_seconds":     int(c.cfg.IssueTTL.Seconds()),
		"parent_token":    "",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return SessionDelegation{}, fmt.Errorf("llmchat/delegation: marshal: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/agents/%s/delegate", c.cfg.BaseURL, c.cfg.AgentID)
	resp, err := c.doWithRetry(ctx, http.MethodPost, url, raw, userPrincipal)
	if err != nil {
		return SessionDelegation{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return SessionDelegation{}, fmt.Errorf("llmchat/delegation: read response: %w", err)
	}
	var parsed struct {
		Token      string `json:"token"`
		JTI        string `json:"jti"`
		ExpiresAt  string `json:"expires_at"`
		ChainDepth int    `json:"chain_depth"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return SessionDelegation{}, fmt.Errorf("llmchat/delegation: decode: %w", err)
	}
	exp, err := time.Parse(time.RFC3339Nano, parsed.ExpiresAt)
	if err != nil {
		// Some gateway versions emit RFC3339 without nano precision.
		if exp, err = time.Parse(time.RFC3339, parsed.ExpiresAt); err != nil {
			return SessionDelegation{}, fmt.Errorf("llmchat/delegation: parse expires_at %q: %w", parsed.ExpiresAt, err)
		}
	}

	out := SessionDelegation{
		Token:      parsed.Token,
		JTI:        parsed.JTI,
		ExpiresAt:  exp,
		ChainDepth: parsed.ChainDepth,
	}
	c.mu.Lock()
	c.cache[userPrincipal] = out
	c.mu.Unlock()
	return out, nil
}

// ForSession returns a usable delegation for the user principal,
// re-minting if the existing one is within 60s of expiry. The caller
// passes the most-recent SessionDelegation (typically from session
// state); a stale `current` triggers a refresh.
func (c *DelegationClient) ForSession(ctx context.Context, userPrincipal string, current SessionDelegation) (SessionDelegation, error) {
	if current.Token != "" && time.Until(current.ExpiresAt) > 60*time.Second {
		return current, nil
	}
	return c.IssueForSession(ctx, userPrincipal)
}

// Revoke invalidates a delegation token by JTI. Best-effort: a 5xx is
// retried twice but a final failure is logged + swallowed by the
// caller (typically WS disconnect handler) since the token TTL will
// eventually expire it anyway.
func (c *DelegationClient) Revoke(ctx context.Context, jti, reason string) error {
	body := map[string]any{
		"jti":     jti,
		"reason":  reason,
		"cascade": false,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("llmchat/delegation: marshal revoke: %w", err)
	}
	url := c.cfg.BaseURL + "/api/v1/agents/revoke-delegation"
	resp, err := c.doWithRetry(ctx, http.MethodPost, url, raw, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<14))
	return nil
}

// doWithRetry implements 3-attempt 5xx exponential-backoff retry with
// 4xx surfacing immediately. The userPrincipal arg, when non-empty, is
// forwarded as `X-Cordum-User-Principal` so the gateway audit records
// it.
func (c *DelegationClient) doWithRetry(ctx context.Context, method, url string, body []byte, userPrincipal string) (*http.Response, error) {
	delay := c.cfg.RetryDelay
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			delay *= 2
		}
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("llmchat/delegation: build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if c.cfg.APIKey != "" {
			req.Header.Set("X-API-Key", c.cfg.APIKey)
		}
		if c.cfg.Tenant != "" {
			req.Header.Set("X-Cordum-Tenant", c.cfg.Tenant)
		}
		if userPrincipal != "" {
			req.Header.Set("X-Cordum-User-Principal", userPrincipal)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: %s", ErrDelegationUnauthorized, strings.TrimSpace(string(preview)))
		case resp.StatusCode == http.StatusForbidden:
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: %s", ErrDelegationForbidden, strings.TrimSpace(string(preview)))
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return resp, nil
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			preview, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("llmchat/delegation: status %d: %s", resp.StatusCode, strings.TrimSpace(string(preview)))
		default:
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<14))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		}
	}
	return nil, fmt.Errorf("llmchat/delegation: retry exhausted: %w", lastErr)
}
