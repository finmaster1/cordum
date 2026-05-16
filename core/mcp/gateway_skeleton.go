package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cordum/cordum/core/edge"
)

// EDGE-100 MCP Gateway skeleton.
//
// This file is intentionally a skeleton: it registers the route family
// `/api/v1/mcp/gateway/*` on a supplied `*http.ServeMux`, wires
// EdgeSession + AgentExecution + connect-event creation on client connect,
// and keeps upstream forwarding disabled by default. EDGE-101 will populate
// the upstream registry; EDGE-104 wires the client attach flow; EDGE-105
// adds dashboard surfaces. None of those concerns belong here.
//
// Per governor amendment comment-a305f5a3:
//   - Routes register from `core/controlplane/gateway/gateway.go` main mux
//     by calling RegisterGatewayRoutes; no new gateway_mcp.go or
//     handlers_mcp_gateway.go is introduced under core/controlplane/gateway.
//   - GatewayEnabled gating is per-tenant via MCPPolicy.GatewayEnabled
//     (core/infra/config/safety_policy.go); deps surface it as a callback
//     so we don't couple the library to the config package.

// GatewayDeps is the injectable dependency surface for the MCP Gateway
// skeleton. Use function fields rather than interfaces for the boundary
// pieces (tenant resolution, gateway gating) so callers can adapt their
// existing gateway helpers without inventing wrapper types; use the
// edge.Store interface directly for the persistence boundary so the
// gateway shares the same evidence store as the rest of /api/v1/edge.
type GatewayDeps struct {
	// Store is the Edge evidence store. The gateway only invokes
	// CreateSession, CreateExecution, and AppendEvent on it.
	Store edge.Store

	// GatewayEnabled returns the per-tenant EDGE-100 gateway flag. Callers
	// typically resolve this through MCPPolicy.GatewayEnabled.
	GatewayEnabled func(ctx context.Context, tenantID string) (bool, error)

	// ResolveTenant maps an incoming request to (tenantID, principalID)
	// using the API gateway's existing X-Tenant-ID + auth-context plumbing.
	// Returning a non-nil error MUST be treated as a 401/403 by the caller
	// — the gateway will not infer a tenant from request body claims.
	ResolveTenant func(r *http.Request) (tenantID, principalID string, err error)

	// Logger is optional; if nil a discard logger is substituted.
	Logger *slog.Logger
}

// RegisterGatewayRoutes binds the EDGE-100 MCP Gateway skeleton route family
// onto the supplied mux. Returns an error if deps are missing pieces required
// for safe operation. Idempotent across separate mux instances; not safe to
// call twice on the same mux (http.ServeMux panics on duplicate patterns).
func RegisterGatewayRoutes(mux *http.ServeMux, deps GatewayDeps) error {
	if mux == nil {
		return errors.New("mcp gateway: mux is required")
	}
	if deps.Store == nil {
		return errors.New("mcp gateway: deps.Store is required")
	}
	if deps.GatewayEnabled == nil {
		return errors.New("mcp gateway: deps.GatewayEnabled is required")
	}
	if deps.ResolveTenant == nil {
		return errors.New("mcp gateway: deps.ResolveTenant is required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}

	g := &Gateway{deps: deps}
	mux.HandleFunc("GET /api/v1/mcp/gateway/health", g.HandleHealth)
	mux.HandleFunc("GET /api/v1/mcp/gateway/config", g.HandleConfig)
	mux.HandleFunc("POST /api/v1/mcp/gateway/upstream/", g.HandleUpstream)
	mux.HandleFunc("POST /api/v1/mcp/gateway/clients/connect", g.HandleClientConnect)
	return nil
}

// NewGateway constructs the EDGE-100 MCP Gateway skeleton's handler state
// without registering routes. Callers that need to wire each handler
// through the API gateway's existing s.registerRoute + s.instrumented
// helpers (rather than RegisterGatewayRoutes' raw mux.HandleFunc path)
// should use this constructor and bind each Handle* method individually.
func NewGateway(deps GatewayDeps) (*Gateway, error) {
	if deps.Store == nil {
		return nil, errors.New("mcp gateway: deps.Store is required")
	}
	if deps.GatewayEnabled == nil {
		return nil, errors.New("mcp gateway: deps.GatewayEnabled is required")
	}
	if deps.ResolveTenant == nil {
		return nil, errors.New("mcp gateway: deps.ResolveTenant is required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}
	return &Gateway{deps: deps}, nil
}

// Gateway is the EDGE-100 MCP Gateway skeleton's exported handler state.
// All four Handle* methods are bound by RegisterGatewayRoutes for direct
// testing, and individually wirable by the API gateway's main mux for
// production deployment.
type Gateway struct {
	deps GatewayDeps
}

// handleHealth always reports 200; the body indicates whether upstream
// forwarding is enabled for the requesting tenant so operators can probe
// a disabled deployment without having to hit /upstream and parse 503s.
// Never touches the Store.
func (g *Gateway) HandleHealth(w http.ResponseWriter, r *http.Request) {
	tenantID, _, _ := g.deps.ResolveTenant(r)
	enabled := false
	if tenantID != "" {
		ok, err := g.deps.GatewayEnabled(r.Context(), tenantID)
		if err == nil {
			enabled = ok
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"gateway_enabled": enabled,
		"component":       "mcp-gateway",
	})
}

// handleConfig returns the effective per-tenant gateway config. Only
// non-secret fields are emitted; this route never echoes upstream
// credentials, bearer tokens, or API keys.
func (g *Gateway) HandleConfig(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := g.deps.ResolveTenant(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "tenant_missing", "X-Tenant-ID required")
		return
	}
	enabled, err := g.deps.GatewayEnabled(r.Context(), tenantID)
	if err != nil {
		g.deps.Logger.Warn("mcp gateway: enable-lookup failed", "tenant", tenantID, "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"gateway_enabled":     enabled,
		"upstream_count":      0,       // EDGE-101 populates from registry
		"upstream_forwarding": "no-op", // skeleton only; EDGE-101 enables
	})
}

// handleUpstream covers the /upstream/* route family. The skeleton always
// returns 503 with one of two diagnostic strings:
//   - "gateway disabled" when GatewayEnabled is false for the tenant
//   - "no upstream configured" when GatewayEnabled is true (registry is
//     empty in P1 until EDGE-101 populates it)
//
// In either case NO Store writes occur; a misconfigured deploy fails loud.
func (g *Gateway) HandleUpstream(w http.ResponseWriter, r *http.Request) {
	tenantID, _, err := g.deps.ResolveTenant(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "tenant_missing", "X-Tenant-ID required")
		return
	}
	enabled, _ := g.deps.GatewayEnabled(r.Context(), tenantID)
	if !enabled {
		writeError(w, http.StatusServiceUnavailable, "gateway_disabled",
			"mcp gateway disabled for tenant — enable via MCPPolicy.GatewayEnabled")
		return
	}
	writeError(w, http.StatusServiceUnavailable, "no_upstream_configured",
		"mcp gateway: no upstream configured — register via EDGE-101 upstream registry")
}

// handleClientConnect resolves tenant + principal from the request, creates
// an EdgeSession + AgentExecution, and emits an
// EventKindMCPServerConnected event with the resolved tenant/principal —
// never from request-body claims (task rail #3). On any failure after
// session creation succeeds, an EventKindMCPServerFailed event is emitted
// so partial-state is observable in audit.
func (g *Gateway) HandleClientConnect(w http.ResponseWriter, r *http.Request) {
	tenantID, principalID, err := g.deps.ResolveTenant(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "tenant_missing", "X-Tenant-ID required")
		return
	}

	now := time.Now().UTC()
	sessionID, sErr := newGatewayID("egs")
	if sErr != nil {
		writeError(w, http.StatusInternalServerError, "id_failed", "session id generation failed")
		return
	}
	executionID, eErr := newGatewayID("aex")
	if eErr != nil {
		writeError(w, http.StatusInternalServerError, "id_failed", "execution id generation failed")
		return
	}

	session := edge.EdgeSession{
		SessionID:     sessionID,
		TenantID:      tenantID,
		PrincipalID:   principalID,
		PrincipalType: edge.PrincipalTypeService,
		AgentProduct:  "cordum-mcp-gateway",
		Mode:          edge.SessionModeLocalDev,
		PolicyMode:    edge.PolicyModeObserve,
		StartedAt:     now,
		Status:        edge.SessionStatusRunning,
		RiskSummary: edge.RiskSummary{
			MaxRisk: edge.RiskLevelLow,
		},
	}
	if err := g.deps.Store.CreateSession(r.Context(), session); err != nil {
		g.emitFailed(r.Context(), tenantID, principalID, sessionID, executionID, "create_session: "+err.Error(), now)
		writeError(w, http.StatusInternalServerError, "session_create_failed", "could not create edge session")
		return
	}

	execution := edge.AgentExecution{
		ExecutionID: executionID,
		SessionID:   sessionID,
		TenantID:    tenantID,
		Adapter:     edge.AdapterMCPGateway,
		Mode:        edge.ExecutionModeLocalDev,
		StartedAt:   now,
		Status:      edge.ExecutionStatusRunning,
	}
	if err := g.deps.Store.CreateExecution(r.Context(), execution); err != nil {
		g.emitFailed(r.Context(), tenantID, principalID, sessionID, executionID, "create_execution: "+err.Error(), now)
		writeError(w, http.StatusInternalServerError, "execution_create_failed", "could not create agent execution")
		return
	}

	connectEvent := edge.AgentActionEvent{
		EventID:     mustGatewayID("aev"),
		SessionID:   sessionID,
		ExecutionID: executionID,
		TenantID:    tenantID,
		PrincipalID: principalID,
		Timestamp:   now,
		Layer:       edge.LayerMCP,
		Kind:        edge.EventKindMCPServerConnected,
		Decision:    edge.DecisionRecorded,
		Status:      edge.ActionStatusOK,
	}
	if _, err := g.deps.Store.AppendEvent(r.Context(), connectEvent); err != nil {
		g.deps.Logger.Warn("mcp gateway: connect event append failed",
			"tenant", tenantID, "session", sessionID, "err", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":   sessionID,
		"execution_id": executionID,
		"tenant_id":    tenantID,
	})
}

// emitFailed records a connect-failure event. Best-effort: a Store write
// failure here is logged but does not propagate, since the caller is
// already on an error path returning to the client. sessionID and
// executionID are minted up-front in HandleClientConnect so the failure
// event carries valid IDs even when the corresponding CreateSession or
// CreateExecution call rejected the record — RedisStore AppendEvent
// validation requires both fields non-empty.
func (g *Gateway) emitFailed(ctx context.Context, tenantID, principalID, sessionID, executionID, reason string, ts time.Time) {
	failedEvent := edge.AgentActionEvent{
		EventID:     mustGatewayID("aev"),
		SessionID:   sessionID,
		ExecutionID: executionID,
		TenantID:    tenantID,
		PrincipalID: principalID,
		Timestamp:   ts,
		Layer:       edge.LayerMCP,
		Kind:        edge.EventKindMCPServerFailed,
		Decision:    edge.DecisionDeny,
		Status:      edge.ActionStatusFailed,
	}
	// Reason is logged structurally; we deliberately do NOT serialise it into
	// the event body to avoid leaking transport-layer error strings into the
	// evidence stream. Operators correlate via Timestamp + tenant.
	if _, err := g.deps.Store.AppendEvent(ctx, failedEvent); err != nil {
		g.deps.Logger.Warn("mcp gateway: failed-event append also failed",
			"tenant", tenantID, "reason", reason, "err", err)
	}
}

// writeJSON serialises v as JSON with Content-Type and the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError emits the gateway's standard {error, code, message} JSON
// envelope at the given status.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error":   strings.ToLower(http.StatusText(status)),
		"code":    code,
		"message": message,
	})
}

// newGatewayID returns a short prefixed hex id. crypto/rand is the source so
// IDs are unguessable even when emitted in audit-readable form.
func newGatewayID(prefix string) (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b[:]), nil
}

// mustGatewayID returns a newGatewayID or falls back to a timestamp suffix.
// Only used on the event-emission path where a Store-side validation will
// still catch malformed IDs.
func mustGatewayID(prefix string) string {
	id, err := newGatewayID(prefix)
	if err == nil {
		return id
	}
	return prefix + "_fallback_" + time.Now().UTC().Format("20060102T150405.000000000")
}
