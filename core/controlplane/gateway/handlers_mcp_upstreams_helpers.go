package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/cordum/cordum/core/controlplane/gateway/auth"
	edgecore "github.com/cordum/cordum/core/edge"
)

func (s *server) mcpUpstreamRegistryOrUnavailable(w http.ResponseWriter, r *http.Request) edgecore.MCPUpstreamRegistry {
	if s != nil && s.mcpUpstreamRegistry != nil {
		return s.mcpUpstreamRegistry
	}
	if s != nil && s.jobStore != nil && s.jobStore.Client() != nil {
		s.mcpUpstreamRegistry = edgecore.NewRedisMCPUpstreamRegistryFromClient(s.jobStore.Client())
		return s.mcpUpstreamRegistry
	}
	writeEdgeError(w, r, http.StatusServiceUnavailable, edgeErrCodeStoreUnavailable, "mcp upstream registry unavailable", nil)
	return nil
}

func filterMCPUpstreamsByEnabledQuery(r *http.Request, items []edgecore.UpstreamServer) []edgecore.UpstreamServer {
	raw := strings.TrimSpace(r.URL.Query().Get("enabled"))
	if raw == "" {
		return items
	}
	wanted, err := strconv.ParseBool(raw)
	if err != nil {
		return items
	}
	out := make([]edgecore.UpstreamServer, 0, len(items))
	for _, item := range items {
		if item.Enabled == wanted {
			out = append(out, item)
		}
	}
	return out
}

func writeMCPUpstreamStoreError(w http.ResponseWriter, r *http.Request, err error, op, tenantID, name string) {
	status, code, message := http.StatusInternalServerError, edgeErrCodeInternalError, "mcp upstream registry error"
	switch {
	case errors.Is(err, edgecore.ErrUpstreamNotFound):
		status, code, message = http.StatusNotFound, edgeErrCodeNotFound, "mcp upstream not found"
	case errors.Is(err, edgecore.ErrUpstreamAlreadyExists):
		status, code, message = http.StatusConflict, edgeErrCodeConflict, "mcp upstream already exists"
	case errors.Is(err, edgecore.ErrUpstreamNotAllowlisted):
		status, code, message = http.StatusForbidden, edgeErrCodeAccessDenied, "mcp upstream not allowlisted"
	case errors.Is(err, edgecore.ErrInvalidUpstream), errors.Is(err, edgecore.ErrInvalidTransport), errors.Is(err, edgecore.ErrUnsafeEndpoint), errors.Is(err, edgecore.ErrSecretMustUseRef), errors.Is(err, edgecore.ErrShellMetacharsRejected):
		status, code, message = http.StatusBadRequest, edgeErrCodeInvalidRequest, "invalid mcp upstream"
	}
	logMCPUpstreamOutcome(op, tenantID, name, "deny", message)
	writeEdgeError(w, r, status, code, message, nil)
}

func writeMCPUpstreamValidationError(w http.ResponseWriter, r *http.Request, err error, tenantID, name string) {
	if isMCPUpstreamValidateOnly(r) {
		writeJSON(w, mcpUpstreamValidationResponse{Valid: false, Reason: mcpUpstreamReason(err)})
		logMCPUpstreamOutcome("validate", tenantID, name, "deny", mcpUpstreamReason(err))
		return
	}
	writeMCPUpstreamStoreError(w, r, err, "validate", tenantID, name)
}

func validateMCPUpstreamTenant(r *http.Request, headerTenant, bodyTenant string) error {
	bodyTenant = strings.TrimSpace(bodyTenant)
	if bodyTenant == "" || bodyTenant == headerTenant {
		return nil
	}
	ctx := auth.FromRequest(r)
	if bodyTenant == "*" && ctx != nil && ctx.AllowCrossTenant && headerTenant == "*" {
		return nil
	}
	return fmt.Errorf("edge tenant body/header mismatch")
}

func (s *server) mcpUpstreamPolicyInputs(r *http.Request, tenantID string) (string, []string) {
	policyMode := strings.TrimSpace(r.URL.Query().Get("policy_mode"))
	allowlist := splitCSVParams(r.URL.Query()["allowed_upstream"])
	allowlist = append(allowlist, splitCSVParams(r.URL.Query()["allowed_upstreams"])...)
	if len(allowlist) > 0 || policyMode != "" || s == nil || s.configSvc == nil {
		return policyMode, allowlist
	}
	return policyMode, s.mcpAllowedUpstreamsFromConfig(r, tenantID)
}

func (s *server) mcpAllowedUpstreamsFromConfig(r *http.Request, tenantID string) []string {
	effective, err := s.configSvc.Effective(r.Context(), tenantID, "", "", "")
	if err != nil {
		return nil
	}
	return extractStringSlice(effective, "safety", "mcp", "allowed_upstreams")
}

func splitCSVParams(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	return out
}

func extractStringSlice(data map[string]any, keys ...string) []string {
	var current any = data
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[key]
	}
	switch v := current.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

func isMCPUpstreamValidateOnly(r *http.Request) bool {
	q := r.URL.Query()
	return strings.EqualFold(q.Get("validate-only"), "true") || strings.EqualFold(q.Get("validate_only"), "true")
}

func mcpUpstreamReason(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, edgecore.ErrUnsafeEndpoint):
		return "unsafe endpoint"
	case errors.Is(err, edgecore.ErrSecretMustUseRef):
		return "secret references must use secret://"
	case errors.Is(err, edgecore.ErrShellMetacharsRejected):
		return "shell metacharacters rejected"
	case errors.Is(err, edgecore.ErrUpstreamNotAllowlisted):
		return "upstream not allowlisted"
	case errors.Is(err, edgecore.ErrInvalidTransport):
		return "invalid transport"
	default:
		return "invalid upstream"
	}
}

func logMCPUpstreamOutcome(op, tenantID, name, decision, reason string) {
	slog.Info("mcp upstream registry", "event", "mcp-upstream-"+op, "tenant_id", tenantID, "name", name, "decision", decision, "reason", reason)
}
