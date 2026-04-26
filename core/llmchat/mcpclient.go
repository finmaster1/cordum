package llmchat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cordum/cordum/core/mcp"
)

// ErrApprovalRequired is the sentinel returned when the MCP server replies
// with the Cordum-reserved JSON-RPC -32099 code. Callers `errors.Is`-match
// it to surface an approval-pending state to the user; `errors.As` into
// *ApprovalRequiredError to recover the approval_id + tool name for the
// UI/audit trail.
var ErrApprovalRequired = errors.New("mcp: approval required")

// ErrConnectionLost is returned when the SSE connection to the MCP server
// drops mid-call. Callers may surface this distinctly from a regular
// network error so the agent loop can retry after the client has
// reconnected.
var ErrConnectionLost = errors.New("mcp: connection lost")

// ErrClosed is returned by client methods invoked after Close.
var ErrClosed = errors.New("mcp: client closed")

// ApprovalRequiredError carries the structured payload from a -32099 JSON-RPC
// response. It satisfies the standard error interface AND `errors.Is` returns
// true for ErrApprovalRequired so call-sites can use either match form.
type ApprovalRequiredError struct {
	ApprovalID string `json:"approval_id"`
	Reason     string `json:"reason"`
	Tool       string `json:"tool"`
}

// Error renders a human-readable error string.
func (e *ApprovalRequiredError) Error() string {
	if e == nil {
		return ErrApprovalRequired.Error()
	}
	return fmt.Sprintf("mcp: approval required for %s (approval_id=%s)", e.Tool, e.ApprovalID)
}

// Is matches the package-level sentinel so `errors.Is(err, ErrApprovalRequired)`
// is true regardless of whether the caller has the typed error or the
// sentinel.
func (e *ApprovalRequiredError) Is(target error) bool {
	return target == ErrApprovalRequired
}

// MCPClientConfig is the boot-time configuration for an MCPClient.
type MCPClientConfig struct {
	// BaseURL is the MCP server root, e.g. https://gateway.internal:8443.
	// The client appends /mcp/sse and /mcp/message itself.
	BaseURL string

	// APIKey is the service-account credential. Forwarded as `X-API-Key`
	// on the SSE bootstrap and on per-call POSTs UNLESS a per-call bearer
	// token is supplied to CallTool, in which case the bearer takes over
	// and X-API-Key is omitted (rail #3 — service API key never leaks
	// into delegated tool-call paths).
	APIKey string

	// TenantID, when non-empty, is sent as `X-Cordum-Tenant`. Optional —
	// some deployments derive tenant from the API key alone.
	TenantID string

	// AgentID, when non-empty, is sent as `X-Agent-Id` so the gateway
	// resolves the chat-assistant identity for scope filtering.
	AgentID string

	// ClientName + ClientVersion populate the Initialize ClientInfo
	// payload so the server-side audit trail can attribute requests.
	ClientName    string
	ClientVersion string

	// ToolsCacheTTL caps how long a successful ListTools response is
	// reused before the next call refetches. Plan default is 60s; tests
	// override.
	ToolsCacheTTL time.Duration

	// ReconnectInitial is the first SSE-reconnect backoff. Doubles on
	// each consecutive failure up to ReconnectMax.
	ReconnectInitial time.Duration

	// ReconnectMax caps the SSE-reconnect backoff.
	ReconnectMax time.Duration

	// PostTimeout is the per-call POST deadline applied when the caller
	// did not provide a context with its own deadline.
	PostTimeout time.Duration

	// HTTPClient lets tests inject a transport. nil = a default client
	// with a bounded Timeout (per-call ctx still drives cancellation).
	HTTPClient *http.Client
}

// MCPClient talks to a Cordum MCP server via /mcp/sse + /mcp/message. It
// reuses the JSON-RPC primitives from core/mcp so the wire format stays
// in lock-step with the server.
//
// Concurrency: Initialize, ListTools, CallTool are safe to call from
// multiple goroutines; the SSE reader runs in its own goroutine started
// by NewMCPClient.
type MCPClient struct {
	cfg        MCPClientConfig
	httpClient *http.Client

	requestID atomic.Uint64

	mu             sync.Mutex
	sessionID      string
	cachedTools    *mcp.ToolListResult
	cachedToolsAt  time.Time
	sessionReady   chan struct{}
	sessionVersion uint64

	closed atomic.Bool
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	sseConnections atomic.Int64
}

// NewMCPClient validates cfg, applies defaults, and starts the background
// SSE reader. It returns immediately; the SSE bootstrap completes
// asynchronously and Initialize/ListTools/CallTool transparently wait for
// the first session ID before issuing their POST.
func NewMCPClient(cfg MCPClientConfig) (*MCPClient, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("llmchat/mcpclient: BaseURL is required")
	}
	if cfg.ToolsCacheTTL <= 0 {
		cfg.ToolsCacheTTL = 60 * time.Second
	}
	if cfg.ReconnectInitial <= 0 {
		cfg.ReconnectInitial = 500 * time.Millisecond
	}
	if cfg.ReconnectMax <= 0 {
		cfg.ReconnectMax = 30 * time.Second
	}
	if cfg.PostTimeout <= 0 {
		cfg.PostTimeout = 30 * time.Second
	}
	if cfg.ClientName == "" {
		cfg.ClientName = "cordum-llm-chat"
	}
	if cfg.ClientVersion == "" {
		cfg.ClientVersion = "0.1.0"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.PostTimeout}
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := &MCPClient{
		cfg:          cfg,
		httpClient:   httpClient,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
		sessionReady: make(chan struct{}),
	}
	go c.sseReadLoop()
	return c, nil
}

// SSEConnections returns the cumulative count of successful SSE
// connect attempts. Tests assert reconnects via this counter; production
// callers may use it for /readyz observability.
func (c *MCPClient) SSEConnections() int64 {
	return c.sseConnections.Load()
}

// Close terminates the background SSE goroutine and signals all in-flight
// calls to abort with ErrClosed. Idempotent.
func (c *MCPClient) Close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.cancel()
	<-c.done
}

// Initialize sends MCP `initialize` and returns the server's
// InitializeResult. It is safe to call multiple times — but the server
// state is per-session, so re-Initialize after a reconnect happens
// implicitly via the SSE bootstrap, not via repeated Initialize calls.
func (c *MCPClient) Initialize(ctx context.Context) (*mcp.InitializeResult, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}
	params := mcp.InitializeParams{
		ProtocolVersion: mcp.DefaultProtocolVersion,
		ClientInfo: &mcp.Implementation{
			Name:    c.cfg.ClientName,
			Version: c.cfg.ClientVersion,
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("llmchat/mcpclient: marshal initialize: %w", err)
	}
	resp, err := c.post(ctx, mcp.MethodInitialize, raw, "")
	if err != nil {
		return nil, err
	}
	var res mcp.InitializeResult
	if err := decodeResult(resp, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// ListTools returns the cached tool list when fresh, otherwise issues a
// `tools/list` call and caches the result. The cache TTL is configurable
// via MCPClientConfig.ToolsCacheTTL.
func (c *MCPClient) ListTools(ctx context.Context) (*mcp.ToolListResult, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}
	c.mu.Lock()
	if c.cachedTools != nil && time.Since(c.cachedToolsAt) < c.cfg.ToolsCacheTTL {
		out := *c.cachedTools
		c.mu.Unlock()
		return &out, nil
	}
	c.mu.Unlock()

	resp, err := c.post(ctx, mcp.MethodToolsList, nil, "")
	if err != nil {
		return nil, err
	}
	var res mcp.ToolListResult
	if err := decodeResult(resp, &res); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.cachedTools = &res
	c.cachedToolsAt = time.Now()
	c.mu.Unlock()
	out := res
	return &out, nil
}

// CallTool issues a `tools/call` request. When bearerToken is non-empty,
// it is sent as `Authorization: Bearer <token>` and X-API-Key is OMITTED
// — the per-session delegation token always supplants the service-account
// key on tool-call paths (rail #3).
//
// On a -32099 approval-required reply, the returned error wraps an
// *ApprovalRequiredError; callers MUST `errors.As` to recover the
// approval_id, or `errors.Is(err, ErrApprovalRequired)` to detect the
// case without extracting the ID.
func (c *MCPClient) CallTool(
	ctx context.Context,
	name string,
	arguments json.RawMessage,
	bearerToken string,
) (*mcp.ToolCallResult, error) {
	if c.closed.Load() {
		return nil, ErrClosed
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("llmchat/mcpclient: tool name is required")
	}
	params := mcp.ToolCallParams{Name: name, Arguments: arguments}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("llmchat/mcpclient: marshal tool call: %w", err)
	}
	resp, err := c.post(ctx, mcp.MethodToolsCall, raw, bearerToken)
	if err != nil {
		return nil, err
	}
	var res mcp.ToolCallResult
	if err := decodeResult(resp, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// post is the shared request path for Initialize / ListTools / CallTool.
// It builds a JSONRPCMessage, sets the right auth header per rail #3, and
// returns the unmarshalled response (or a typed error for the JSON-RPC
// Error path).
func (c *MCPClient) post(
	ctx context.Context,
	method string,
	params json.RawMessage,
	bearerToken string,
) (*mcp.JSONRPCMessage, error) {
	sessionID, err := c.waitForSession(ctx)
	if err != nil {
		return nil, err
	}
	if c.cfg.PostTimeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, c.cfg.PostTimeout)
			defer cancel()
		}
	}

	id := c.nextRequestID()
	idJSON, _ := json.Marshal(id)
	msg := mcp.JSONRPCMessage{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      idJSON,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("llmchat/mcpclient: marshal envelope: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/mcp/message", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llmchat/mcpclient: build request: %w", err)
	}
	if c.ctx.Err() != nil {
		return nil, ErrClosed
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.applyAuthHeaders(req, bearerToken)
	c.applyContextHeaders(req, sessionID)

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		// Distinguish ctx cancellation from generic transport error.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("llmchat/mcpclient: %s POST: %w", method, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<24)) // 16 MiB cap
	if err != nil {
		return nil, fmt.Errorf("llmchat/mcpclient: read response: %w", err)
	}
	if httpResp.StatusCode == http.StatusUnauthorized || httpResp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("llmchat/mcpclient: %s POST status %d: %s", method, httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if httpResp.StatusCode >= 400 {
		return nil, fmt.Errorf("llmchat/mcpclient: %s POST status %d: %s", method, httpResp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var resp mcp.JSONRPCMessage
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("llmchat/mcpclient: decode response: %w", err)
	}
	if resp.Error != nil {
		return nil, mapJSONRPCError(resp.Error)
	}
	return &resp, nil
}

// applyAuthHeaders enforces the auth hierarchy: a non-empty bearer token
// supplants X-API-Key entirely (rail #3 — the service API key never
// leaves the chat-assistant container on tool-call paths). When no
// bearer is given the service API key flows through.
func (c *MCPClient) applyAuthHeaders(req *http.Request, bearerToken string) {
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
		req.Header.Del("X-API-Key")
		return
	}
	if c.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", c.cfg.APIKey)
	}
}

// applyContextHeaders attaches MCP session + tenant + agent metadata so
// the gateway resolves identity + tenant at the auth middleware layer.
func (c *MCPClient) applyContextHeaders(req *http.Request, sessionID string) {
	if sessionID != "" {
		req.Header.Set("X-MCP-Session-ID", sessionID)
	}
	if c.cfg.TenantID != "" {
		req.Header.Set("X-Cordum-Tenant", c.cfg.TenantID)
	}
	if c.cfg.AgentID != "" {
		req.Header.Set("X-Agent-Id", c.cfg.AgentID)
	}
}

// waitForSession blocks until the SSE bootstrap has captured a session ID
// or ctx is cancelled / the client closed.
func (c *MCPClient) waitForSession(ctx context.Context) (string, error) {
	for {
		c.mu.Lock()
		ready := c.sessionReady
		sessionID := c.sessionID
		c.mu.Unlock()
		if sessionID != "" {
			return sessionID, nil
		}
		select {
		case <-ready:
			// The SSE loop may invalidate the session immediately after
			// waking waiters. Re-read under the mutex so POSTs bind to a
			// concrete session ID instead of racing into an empty/stale
			// X-MCP-Session-ID header.
			continue
		case <-ctx.Done():
			return "", ctx.Err()
		case <-c.ctx.Done():
			return "", ErrClosed
		}
	}
}

func (c *MCPClient) nextRequestID() uint64 {
	return c.requestID.Add(1)
}

// sseReadLoop is the background goroutine that maintains the SSE
// connection to /mcp/sse, parses session/data frames, and reconnects with
// exponential backoff on any error.
func (c *MCPClient) sseReadLoop() {
	defer close(c.done)
	backoff := c.cfg.ReconnectInitial
	for {
		if c.ctx.Err() != nil {
			return
		}
		err := c.runSSE()
		if c.ctx.Err() != nil {
			return
		}
		if err == nil {
			backoff = c.cfg.ReconnectInitial
		}
		if err != nil {
			slog.Warn("llmchat/mcpclient: SSE disconnected", "error", err, "backoff", backoff)
		}
		// Resolve any pending waiters for the previous session — the
		// session ID is no longer valid after a disconnect; the next
		// successful connect minted a new one will re-arm sessionReady.
		c.invalidateSession()

		select {
		case <-c.ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > c.cfg.ReconnectMax {
			backoff = c.cfg.ReconnectMax
		}
	}
}

// runSSE opens one SSE connection and drains it; returns when the stream
// ends (EOF, error, or ctx cancel). The exponential-backoff caller
// decides whether to reconnect.
func (c *MCPClient) runSSE() error {
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, c.cfg.BaseURL+"/mcp/sse", nil)
	if err != nil {
		return fmt.Errorf("build sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", c.cfg.APIKey)
	}
	if c.cfg.TenantID != "" {
		req.Header.Set("X-Cordum-Tenant", c.cfg.TenantID)
	}
	if c.cfg.AgentID != "" {
		req.Header.Set("X-Agent-Id", c.cfg.AgentID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sse status %d", resp.StatusCode)
	}
	if hdr := resp.Header.Get("X-MCP-Session-ID"); hdr != "" {
		c.setSessionID(hdr)
	}
	c.sseConnections.Add(1)

	reader := bufio.NewReader(resp.Body)
	var buf bytes.Buffer
	for {
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			buf.Write(line)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if !isBlankSSELine(line) {
			continue
		}
		c.consumeFrame(buf.Bytes())
		buf.Reset()
	}
}

// consumeFrame parses one SSE event block and reacts to its type.
// `event: session` carries the initial `{sessionId: "..."}` payload;
// `data: <json>` lines for plain JSON-RPC messages are accepted but
// ignored at this scope (phase 5 wires server-push handling).
func (c *MCPClient) consumeFrame(frame []byte) {
	var event string
	var dataLines [][]byte
	for _, raw := range bytes.Split(frame, []byte{'\n'}) {
		line := bytes.TrimRight(raw, "\r")
		if len(line) == 0 {
			continue
		}
		if bytes.HasPrefix(line, []byte(":")) {
			continue // SSE comment, e.g. keepalive `: ping`
		}
		if bytes.HasPrefix(line, []byte("event:")) {
			event = string(bytes.TrimSpace(line[len("event:"):]))
			continue
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			dataLines = append(dataLines, bytes.TrimSpace(line[len("data:"):]))
			continue
		}
	}
	if event == "session" && len(dataLines) > 0 {
		var payload struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(bytes.Join(dataLines, []byte{'\n'}), &payload); err == nil && payload.SessionID != "" {
			c.setSessionID(payload.SessionID)
		}
	}
	// Server-push JSON-RPC frames (resources/list updates etc.) land
	// here for phase 5 to wire; phase 2 ignores them.
}

// setSessionID stores a new session id and wakes any waitForSession
// callers parked on the current sessionReady channel. The close is
// guarded by a select-default so a duplicate setSessionID (e.g. when
// the server sends both X-MCP-Session-ID header AND the event:session
// frame for the same connect) cannot panic on a re-close.
func (c *MCPClient) setSessionID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID == id {
		return
	}
	c.sessionID = id
	c.sessionVersion++
	select {
	case <-c.sessionReady:
		// already closed by a prior setSessionID — no-op.
	default:
		close(c.sessionReady)
	}
}

// invalidateSession clears the session id on disconnect. The
// sessionReady channel is only re-armed when it has been closed (i.e.
// the previous session was established and waiters proceeded). If it is
// still open — meaning no session was ever established or the previous
// invalidation already replaced it — leaving it untouched preserves the
// existing parked waiters.
func (c *MCPClient) invalidateSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = ""
	select {
	case <-c.sessionReady:
		// Previous channel was closed (waiters proceeded). Mint a
		// fresh open channel so future waitForSession calls park.
		c.sessionReady = make(chan struct{})
	default:
		// Still open — keep it so existing waiters do not lose their
		// reference. The next setSessionID will close it.
	}
}

// mapJSONRPCError converts a server-side JSONRPCError into the typed
// Go error the agent loop expects. -32099 → *ApprovalRequiredError, all
// others → a plain error carrying code + message + data.
func mapJSONRPCError(err *mcp.JSONRPCError) error {
	if err == nil {
		return nil
	}
	if err.Code == -32099 {
		ae := &ApprovalRequiredError{}
		if err.Data != nil {
			raw, _ := json.Marshal(err.Data)
			_ = json.Unmarshal(raw, ae)
		}
		return ae
	}
	return fmt.Errorf("llmchat/mcpclient: jsonrpc error code=%d msg=%q", err.Code, err.Message)
}

// decodeResult unmarshals a successful JSONRPCMessage's Result into the
// target, accepting either a json.RawMessage Result (test fakes) or any
// other JSON-decodable value.
func decodeResult(msg *mcp.JSONRPCMessage, target any) error {
	if msg == nil {
		return fmt.Errorf("llmchat/mcpclient: nil response")
	}
	if msg.Result == nil {
		return fmt.Errorf("llmchat/mcpclient: response carries no result")
	}
	raw, err := json.Marshal(msg.Result)
	if err != nil {
		return fmt.Errorf("llmchat/mcpclient: re-encode result: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("llmchat/mcpclient: decode result: %w", err)
	}
	return nil
}

func isBlankSSELine(line []byte) bool {
	s := bytes.TrimRight(line, "\r\n")
	return len(s) == 0
}
