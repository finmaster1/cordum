package gateway

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cordum/cordum/core/audit"
)

// captureAuditSender records every SIEMEvent forwarded to it so tests
// can assert the producer-side TenantID. Mirrors the recordingSender
// pattern used in other gateway tests but lives in this file to keep
// the audit-focused fixtures self-contained.
type captureAuditSender struct {
	mu     sync.Mutex
	events []audit.SIEMEvent
}

func (c *captureAuditSender) Send(e audit.SIEMEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureAuditSender) Close() error { return nil }

func (c *captureAuditSender) snapshot() []audit.SIEMEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]audit.SIEMEvent, len(c.events))
	copy(out, c.events)
	return out
}

// TestMiddlewareAuditReadAttributesTenantIDWhenAuthCtxNil asserts the
// producer-side fix at middleware.go's auditReadMiddleware: when the
// request has no auth context (anonymous read) the emitted SIEMEvent
// MUST carry TenantID = model.DefaultTenant, NOT empty string. Without
// this, the audit chain sink-level fallback would still attribute the
// event but at the higher slog.Warn level (per task-3fad45d3 Phase 4
// downgrade) — surfacing as a per-request producer-bug log line on
// every anonymous read. Producer-side attribution silences the warn.
func TestMiddlewareAuditReadAttributesTenantIDWhenAuthCtxNil(t *testing.T) {
	t.Parallel()
	cap := &captureAuditSender{}
	// sampleRate=1.0 forces every GET to audit so we don't depend on the
	// crypto/rand sampling path. /api/policy is on the always-audit list
	// per isSensitiveRead, so even sampleRate=0 would emit; we set 1.0
	// for the test signal regardless.
	handler := auditReadMiddleware(cap, 1.0, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/policy", nil)
	// Intentionally no auth context on req.Context() — simulates the
	// anonymous-read code path.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	events := cap.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	if events[0].TenantID != "default" {
		t.Fatalf("TenantID = %q, want %q (model.DefaultTenant — anonymous-read must default)",
			events[0].TenantID, "default")
	}
}

// TestMiddlewareAuditReadHonorsXTenantIDHeader asserts the second tier
// of the resolver priority chain: when authCtx is nil but the request
// carries an X-Tenant-ID header (some pre-auth or anonymous-but-known
// callers do this), the audit event lands on that tenant chain rather
// than the default. Preserves per-tenant chain isolation even on
// unauthenticated paths.
func TestMiddlewareAuditReadHonorsXTenantIDHeader(t *testing.T) {
	t.Parallel()
	cap := &captureAuditSender{}
	handler := auditReadMiddleware(cap, 1.0, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/policy", nil)
	req.Header.Set("X-Tenant-ID", "tenant-anonymous-but-known")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	events := cap.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(events))
	}
	if events[0].TenantID != "tenant-anonymous-but-known" {
		t.Fatalf("TenantID = %q, want %q (X-Tenant-ID header should beat default)",
			events[0].TenantID, "tenant-anonymous-but-known")
	}
}
