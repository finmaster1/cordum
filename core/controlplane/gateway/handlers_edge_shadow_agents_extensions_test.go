// EDGE-143.5 — tests for §10.2 query-param validation on the shadow
// agents list handler. Lives in its own file to avoid collision with
// task-4cd8299f on handlers_edge_shadow_agents_test.go (per chat
// msg-901f7405 file-collision warning).
package gateway

import (
	"net/http"
	"testing"
)

// TestShadowAgentListExtensions_Validation hits parseShadowFindingListQuery
// through the full handler with §10.2 query params: each invalid input
// returns 400+edgeErrCodeInvalidRequest; valid inputs return 200.
func TestShadowAgentListExtensions_Validation(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		want   int
		reason string
	}{
		// Invalid §10.2 params → 400.
		{"invalid_source_type", "/api/v1/edge/shadow-agents?source_type=cosmic", http.StatusBadRequest, "source_type not in enum"},
		{"invalid_ci_provider", "/api/v1/edge/shadow-agents?ci_provider=bamboo", http.StatusBadRequest, "ci_provider not in enum"},
		{"repo_without_ci_provider", "/api/v1/edge/shadow-agents?repo=acme/x", http.StatusBadRequest, "repo requires ci_provider"},
		{"confidence_min_too_high", "/api/v1/edge/shadow-agents?confidence_min=1.5", http.StatusBadRequest, "confidence_min > 1"},
		{"confidence_min_negative", "/api/v1/edge/shadow-agents?confidence_min=-0.1", http.StatusBadRequest, "confidence_min < 0"},
		{"confidence_min_not_a_number", "/api/v1/edge/shadow-agents?confidence_min=abc", http.StatusBadRequest, "confidence_min malformed"},
		{"confidence_min_nan", "/api/v1/edge/shadow-agents?confidence_min=NaN", http.StatusBadRequest, "confidence_min NaN bypass"},
		{"confidence_min_inf", "/api/v1/edge/shadow-agents?confidence_min=+Inf", http.StatusBadRequest, "confidence_min Inf bypass"},
		{"first_seen_after_malformed", "/api/v1/edge/shadow-agents?first_seen_after=tomorrow", http.StatusBadRequest, "first_seen_after not RFC3339"},
		{"last_seen_before_malformed", "/api/v1/edge/shadow-agents?last_seen_before=2026-99-99T00:00:00Z", http.StatusBadRequest, "last_seen_before invalid date"},
		{"invalid_signal_shape", "/api/v1/edge/shadow-agents?signal=HAS-CAPS", http.StatusBadRequest, "signal regex"},
		{"include_managed_skip_bad", "/api/v1/edge/shadow-agents?include_managed_skip=maybe", http.StatusBadRequest, "boolean parse"},

		// Valid §10.2 params → 200.
		{"valid_source_type", "/api/v1/edge/shadow-agents?source_type=kubernetes", http.StatusOK, "happy"},
		{"valid_ci_provider_plus_repo", "/api/v1/edge/shadow-agents?ci_provider=github_actions&repo=acme/x", http.StatusOK, "composite ok"},
		{"valid_confidence_min", "/api/v1/edge/shadow-agents?confidence_min=0.5", http.StatusOK, "in range"},
		{"valid_first_seen_after", "/api/v1/edge/shadow-agents?first_seen_after=2026-01-01T00:00:00Z", http.StatusOK, "RFC3339"},
		{"valid_repeated_signals", "/api/v1/edge/shadow-agents?signal=k8s_heartbeat_missing&signal=ci_fork_pr", http.StatusOK, "any-of"},
		{"valid_include_managed_skip", "/api/v1/edge/shadow-agents?include_managed_skip=true", http.StatusOK, "boolean true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newShadowGateway(t)
			rec := getShadow(t, s, "tenant-a", tc.path)
			if rec.Code != tc.want {
				t.Fatalf("%s: got %d, want %d (%s); body=%s", tc.name, rec.Code, tc.want, tc.reason, rec.Body.String())
			}
		})
	}
}

// TestShadowAgentListExtensions_SignalCap asserts the per-request signal
// list is capped at 16 entries (DoS prevention per design §10.5 spirit).
func TestShadowAgentListExtensions_SignalCap(t *testing.T) {
	s := newShadowGateway(t)
	path := "/api/v1/edge/shadow-agents?"
	for i := 0; i < 17; i++ {
		if i > 0 {
			path += "&"
		}
		path += "signal=sig_" + string(rune('a'+i%26)) + "_x"
	}
	rec := getShadow(t, s, "tenant-a", path)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("17 signals = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
