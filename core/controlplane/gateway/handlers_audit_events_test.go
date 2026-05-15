package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cordum/cordum/core/audit"
	"github.com/cordum/cordum/core/controlplane/gateway/auth"
)

// seedAuditEvents appends customizable SIEM events through the real Chainer
// to the gateway's shared Redis client and installs the chainer on the
// server. Returns the appended events (with Seq/EventHash populated by
// Append) so tests can assert against the wire payload.
func seedAuditEvents(t *testing.T, s *server, tenant string, events []audit.SIEMEvent) []audit.SIEMEvent {
	t.Helper()
	chainer := audit.NewChainer(s.redisClient(), "")
	s.auditChainer = chainer
	out := make([]audit.SIEMEvent, 0, len(events))
	for i := range events {
		ev := events[i]
		ev.TenantID = tenant
		if ev.Timestamp.IsZero() {
			ev.Timestamp = time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		}
		if err := chainer.Append(context.Background(), &ev); err != nil {
			t.Fatalf("seed append[%d]: %v", i, err)
		}
		out = append(out, ev)
	}
	return out
}

func decodeAuditEventsResponse(t *testing.T, rec *httptest.ResponseRecorder) auditEventsResponse {
	t.Helper()
	var out auditEventsResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	return out
}

// TestHandleAuditEvents_RequiresAuditReadPerm pins the legacy-role wall:
// when RBAC is not entitled, a viewer must be rejected by the legacyRoles
// gate even though the basic-role mapping grants audit.read. Matches the
// admin-only convention used by /audit/verify and /policy/audit.
func TestHandleAuditEvents_RequiresAuditReadPerm(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.auth = &tenantStrictAuth{tenant: "default", role: "viewer"}
	authCtx := &auth.AuthContext{Tenant: "default", Role: "viewer", PrincipalID: "v1"}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=default", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKey{}, authCtx))
	rec := httptest.NewRecorder()
	s.handleListAuditEvents(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleAuditEvents_TenantScoped pins that streams for tenants other
// than the caller's authenticated tenant are never returned, even when the
// auth provider rejects cross-tenant override. Mirrors handlers_audit_verify
// tenant scoping.
func TestHandleAuditEvents_TenantScoped(t *testing.T) {
	s, _, _ := newTestGateway(t)
	tenantA := []audit.SIEMEvent{
		{EventType: audit.EventSafetyDecision, Severity: audit.SeverityInfo, Action: "a1"},
		{EventType: audit.EventSafetyDecision, Severity: audit.SeverityInfo, Action: "a2"},
		{EventType: audit.EventSafetyDecision, Severity: audit.SeverityInfo, Action: "a3"},
	}
	tenantB := []audit.SIEMEvent{
		{EventType: audit.EventSafetyDecision, Severity: audit.SeverityInfo, Action: "b1"},
		{EventType: audit.EventSafetyDecision, Severity: audit.SeverityInfo, Action: "b2"},
	}
	seedAuditEvents(t, s, "tenant-a", tenantA)
	seedAuditEvents(t, s, "tenant-b", tenantB)

	req := adminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=tenant-a", nil))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	s.handleListAuditEvents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeAuditEventsResponse(t, rec)
	if len(resp.Items) != 3 {
		t.Fatalf("returned %d items, want 3 (tenant-a only); items=%+v", len(resp.Items), resp.Items)
	}
	for _, it := range resp.Items {
		if it.TenantID != "tenant-a" {
			t.Errorf("item tenant_id=%q, want tenant-a", it.TenantID)
		}
		if strings.HasPrefix(it.Action, "b") {
			t.Errorf("leaked tenant-b event into tenant-a response: %+v", it)
		}
	}
}

// TestHandleAuditEvents_CursorPaginationStable seeds 25 events and walks
// the cursor across pages of 10/10/5. Pin that pages are reverse-chrono
// (highest Seq first) and that the final page's cursor is empty.
func TestHandleAuditEvents_CursorPaginationStable(t *testing.T) {
	s, _, _ := newTestGateway(t)
	events := make([]audit.SIEMEvent, 25)
	for i := range events {
		events[i] = audit.SIEMEvent{
			EventType: audit.EventSafetyDecision,
			Severity:  audit.SeverityInfo,
			Action:    "evt-" + itoa(i),
			JobID:     "job-" + itoa(i),
		}
	}
	seedAuditEvents(t, s, "default", events)

	// Page 1
	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=default&limit=10", nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page1 status=%d: %s", rec.Code, rec.Body.String())
	}
	page1 := decodeAuditEventsResponse(t, rec)
	if len(page1.Items) != 10 {
		t.Fatalf("page1 returned %d items, want 10", len(page1.Items))
	}
	if page1.NextCursor == "" {
		t.Fatalf("page1 missing next_cursor, want a non-empty cursor with 15 events remaining")
	}
	// Reverse-chronological: first item's Seq must be the highest (25).
	if page1.Items[0].Seq != 25 {
		t.Errorf("page1 first item Seq=%d, want 25 (reverse-chrono)", page1.Items[0].Seq)
	}
	if page1.Items[len(page1.Items)-1].Seq != 16 {
		t.Errorf("page1 last item Seq=%d, want 16 (reverse-chrono)", page1.Items[len(page1.Items)-1].Seq)
	}

	// Page 2
	rec = httptest.NewRecorder()
	req = adminCtx(httptest.NewRequest(http.MethodGet,
		"/api/v1/audit/events?tenant=default&limit=10&cursor="+page1.NextCursor, nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page2 status=%d: %s", rec.Code, rec.Body.String())
	}
	page2 := decodeAuditEventsResponse(t, rec)
	if len(page2.Items) != 10 {
		t.Fatalf("page2 returned %d items, want 10", len(page2.Items))
	}
	if page2.Items[0].Seq != 15 || page2.Items[len(page2.Items)-1].Seq != 6 {
		t.Errorf("page2 seq range = [%d,%d], want [15,6]",
			page2.Items[0].Seq, page2.Items[len(page2.Items)-1].Seq)
	}

	// Page 3 — last 5
	rec = httptest.NewRecorder()
	req = adminCtx(httptest.NewRequest(http.MethodGet,
		"/api/v1/audit/events?tenant=default&limit=10&cursor="+page2.NextCursor, nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page3 status=%d: %s", rec.Code, rec.Body.String())
	}
	page3 := decodeAuditEventsResponse(t, rec)
	if len(page3.Items) != 5 {
		t.Fatalf("page3 returned %d items, want 5", len(page3.Items))
	}
	if page3.NextCursor != "" {
		t.Errorf("page3 next_cursor=%q, want empty (end of stream)", page3.NextCursor)
	}
	if page3.Items[0].Seq != 5 || page3.Items[len(page3.Items)-1].Seq != 1 {
		t.Errorf("page3 seq range = [%d,%d], want [5,1]",
			page3.Items[0].Seq, page3.Items[len(page3.Items)-1].Seq)
	}

	// Page seq sets must be disjoint and union to {1..25}.
	seen := make(map[int64]bool)
	for _, p := range [][]auditEventResponseItem{page1.Items, page2.Items, page3.Items} {
		for _, it := range p {
			if seen[it.Seq] {
				t.Errorf("seq %d returned twice across pages", it.Seq)
			}
			seen[it.Seq] = true
		}
	}
	if len(seen) != 25 {
		t.Errorf("union of pages = %d seqs, want 25", len(seen))
	}
}

// TestHandleAuditEvents_EventTypeFilter pins ?event_type= narrows the
// returned set. Seed a mix of event types; query a single type; assert
// only matching are returned.
func TestHandleAuditEvents_EventTypeFilter(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedAuditEvents(t, s, "default", []audit.SIEMEvent{
		{EventType: audit.EventMCPToolInvocation, Severity: audit.SeverityInfo, Action: "a"},
		{EventType: audit.EventEdgePolicyDecision, Severity: audit.SeverityInfo, Action: "b"},
		{EventType: audit.EventMCPToolInvocation, Severity: audit.SeverityInfo, Action: "c"},
		{EventType: audit.EventSafetyDecision, Severity: audit.SeverityInfo, Action: "d"},
	})

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet,
		"/api/v1/audit/events?tenant=default&event_type="+audit.EventMCPToolInvocation, nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeAuditEventsResponse(t, rec)
	if len(resp.Items) != 2 {
		t.Fatalf("returned %d items, want 2 (mcp.tool_invocation only); items=%+v",
			len(resp.Items), resp.Items)
	}
	for _, it := range resp.Items {
		if it.EventType != audit.EventMCPToolInvocation {
			t.Errorf("item event_type=%q, want %q", it.EventType, audit.EventMCPToolInvocation)
		}
	}
}

// TestHandleAuditEvents_TimeRangeFilter pins ?from / ?to inclusively
// bound results by timestamp. Seed events spanning a 10ms window; query
// the middle 4ms; assert only the middle events surface.
func TestHandleAuditEvents_TimeRangeFilter(t *testing.T) {
	s, _, _ := newTestGateway(t)
	base := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	ev := make([]audit.SIEMEvent, 10)
	for i := range ev {
		ev[i] = audit.SIEMEvent{
			EventType: audit.EventSafetyDecision,
			Severity:  audit.SeverityInfo,
			Action:    "x",
			Timestamp: base.Add(time.Duration(i) * time.Minute),
		}
	}
	seedAuditEvents(t, s, "default", ev)

	from := base.Add(3 * time.Minute).Format(time.RFC3339)
	to := base.Add(6 * time.Minute).Format(time.RFC3339)
	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet,
		"/api/v1/audit/events?tenant=default&from="+from+"&to="+to, nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeAuditEventsResponse(t, rec)
	// Events at minute 3, 4, 5, 6 — inclusive.
	if len(resp.Items) != 4 {
		t.Errorf("returned %d items, want 4 (minutes 3..6 inclusive); items=%+v",
			len(resp.Items), resp.Items)
	}
}

// TestHandleAuditEvents_AuditChainerNotInstalled mirrors the verify
// handler's 503 fail-loud guard: when no chainer is configured, the
// endpoint must surface the misconfiguration rather than returning a
// false-green empty list.
func TestHandleAuditEvents_AuditChainerNotInstalled(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.auditChainer = nil

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=default", nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "audit_chainer_not_installed") &&
		!strings.Contains(body, "audit chainer not installed") {
		t.Errorf("body=%q, want audit_chainer_not_installed marker", body)
	}
}

// TestHandleAuditEvents_RedactsSecretsInExtra pins defense-in-depth
// redaction: known-secret keys (token, password, api_key, secret,
// private_key) are stripped from Extra before serialization, even when
// the emit site failed to redact. The wire response MUST NEVER contain
// the secret value anywhere.
func TestHandleAuditEvents_RedactsSecretsInExtra(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedAuditEvents(t, s, "default", []audit.SIEMEvent{
		{
			EventType: audit.EventSafetyDecision,
			Severity:  audit.SeverityInfo,
			Action:    "leak-test",
			Extra: map[string]string{
				"token":       "sk-secret-xyz",
				"password":    "p@ssw0rd",
				"api_key":     "ak_live_123",
				"apiKey":      "AK-CAMEL",
				"private_key": "-----BEGIN PRIVATE KEY-----", // no-secret-lint — synthetic PEM marker used to assert redaction
				"SECRET":      "uppercase-secret",
				"safe_field":  "kept",
				"resource_id": "rid-9",
			},
		},
	})

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=default", nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeAuditEventsResponse(t, rec)
	if len(resp.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(resp.Items))
	}
	extra := resp.Items[0].Extra
	for _, k := range []string{"token", "password", "api_key", "apiKey", "private_key", "SECRET"} {
		if _, present := extra[k]; present {
			t.Errorf("secret key %q leaked into response (value=%q)", k, extra[k])
		}
	}
	if v, ok := extra["safe_field"]; !ok || v != "kept" {
		t.Errorf("safe_field stripped; want kept, got %q", v)
	}
	if v, ok := extra["resource_id"]; !ok || v != "rid-9" {
		t.Errorf("resource_id stripped; want rid-9, got %q", v)
	}
	// Defense-in-depth body-level scan: secret values MUST NOT appear in
	// the wire payload anywhere, regardless of whether the key survived.
	for _, secret := range []string{
		"sk-secret-xyz", "p@ssw0rd", "ak_live_123", "AK-CAMEL",
		"-----BEGIN PRIVATE KEY-----", "uppercase-secret", // no-secret-lint — body-scan also expects the synthetic PEM marker not to leak
	} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("secret value %q leaked into response body", secret)
		}
	}
}

// TestHandleAuditEvents_BoundedLimit pins the hard cap: a request asking
// for many more rows than the maximum is clamped silently to the maximum
// — clients cannot DoS the gateway by demanding unbounded fetches.
func TestHandleAuditEvents_BoundedLimit(t *testing.T) {
	s, _, _ := newTestGateway(t)
	events := make([]audit.SIEMEvent, MaxAuditEventsLimit+25)
	for i := range events {
		events[i] = audit.SIEMEvent{
			EventType: audit.EventSafetyDecision,
			Severity:  audit.SeverityInfo,
			Action:    "x",
		}
	}
	seedAuditEvents(t, s, "default", events)

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet,
		"/api/v1/audit/events?tenant=default&limit=99999", nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeAuditEventsResponse(t, rec)
	if len(resp.Items) != MaxAuditEventsLimit {
		t.Errorf("returned %d items, want clamped to MaxAuditEventsLimit=%d",
			len(resp.Items), MaxAuditEventsLimit)
	}
}

// TestHandleAuditEvents_NoEventsForFreshTenant pins that a tenant with a
// chainer installed but no events ever appended returns 200 with an
// empty page, NOT 404 or 500. The 503 fail-loud applies only when the
// chainer itself is uninstalled.
func TestHandleAuditEvents_NoEventsForFreshTenant(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.auditChainer = audit.NewChainer(s.redisClient(), "")

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=default", nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200: %s", rec.Code, rec.Body.String())
	}
	resp := decodeAuditEventsResponse(t, rec)
	if len(resp.Items) != 0 {
		t.Errorf("got %d items, want 0 for fresh tenant", len(resp.Items))
	}
	if resp.NextCursor != "" {
		t.Errorf("next_cursor=%q, want empty for fresh tenant", resp.NextCursor)
	}
}

// TestHandleAuditEvents_MetaAudit pins the audit-of-audit closure: every
// /audit/events call appends one audit.read.events SIEMEvent to the
// tenant's stream. Without this, the read surface is invisible to the
// chain — a compliance hole.
func TestHandleAuditEvents_MetaAudit(t *testing.T) {
	s, _, _ := newTestGateway(t)
	seedAuditEvents(t, s, "default", []audit.SIEMEvent{
		{EventType: audit.EventSafetyDecision, Severity: audit.SeverityInfo, Action: "seed"},
	})
	// Seed appended exactly 1 event.
	streamKey := audit.NewChainer(s.redisClient(), "").StreamKey("default")
	before, err := s.redisClient().XLen(context.Background(), streamKey).Result()
	if err != nil {
		t.Fatalf("xlen pre-call: %v", err)
	}
	if before != 1 {
		t.Fatalf("expected 1 seeded event, got %d", before)
	}

	rec := httptest.NewRecorder()
	req := adminCtx(httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?tenant=default", nil))
	s.handleListAuditEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d: %s", rec.Code, rec.Body.String())
	}

	after, err := s.redisClient().XLen(context.Background(), streamKey).Result()
	if err != nil {
		t.Fatalf("xlen post-call: %v", err)
	}
	if after != before+1 {
		t.Errorf("stream grew by %d after call, want +1 (audit.read.events meta-event)",
			after-before)
	}
}

// TestHandleAuditEvents_HeavyFilterPagesForward pins the cursor-forward
// guarantee under heavy filter pressure: when a single XRevRange scan
// (limit * auditEventsFetchMultiplier * maxBatchRoundtrips entries)
// produces zero matches because every entry is dropped by the filter
// chain, the response must hand back a cursor that lets the client
// resume DEEPER in the stream. Returning an empty cursor here would
// silently lose any matching events older than the scan window.
//
// Regression for the adversarial-self-review finding: the original
// readAuditEventsPage only tracked lastEmittedID, so a heavily-filtered
// page exited with cursor="" and the client incorrectly stopped paging.
func TestHandleAuditEvents_HeavyFilterPagesForward(t *testing.T) {
	s, _, _ := newTestGateway(t)
	const pageLimit = 2
	// Seed enough non-matching entries to force the search to exit on
	// maxBatchRoundtrips without finding any matches in the first page:
	// scanWindow = pageLimit * auditEventsFetchMultiplier * maxBatchRoundtrips = 2 * 4 * 8 = 64.
	// Pad to 80 non-matching so the matches sit beyond the first page's window.
	const nonMatching = pageLimit*auditEventsFetchMultiplier*8 + 16
	events := make([]audit.SIEMEvent, 0, nonMatching+pageLimit)
	// Matching events (older) first — they were appended earlier.
	for i := 0; i < pageLimit; i++ {
		events = append(events, audit.SIEMEvent{
			EventType: audit.EventMCPToolInvocation,
			Severity:  audit.SeverityInfo,
			Action:    "matching-" + itoa(i),
		})
	}
	// Non-matching events newest (appended after matches).
	for i := 0; i < nonMatching; i++ {
		events = append(events, audit.SIEMEvent{
			EventType: audit.EventSafetyDecision,
			Severity:  audit.SeverityInfo,
			Action:    "filler-" + itoa(i),
		})
	}
	seedAuditEvents(t, s, "default", events)

	cursor := ""
	seen := 0
	const maxPages = 30
	for page := 0; page < maxPages && seen < pageLimit; page++ {
		url := "/api/v1/audit/events?tenant=default&event_type=" +
			audit.EventMCPToolInvocation + "&limit=" + itoa(pageLimit)
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		rec := httptest.NewRecorder()
		req := adminCtx(httptest.NewRequest(http.MethodGet, url, nil))
		s.handleListAuditEvents(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d: status=%d body=%s", page, rec.Code, rec.Body.String())
		}
		resp := decodeAuditEventsResponse(t, rec)
		seen += len(resp.Items)
		if resp.NextCursor == "" {
			if seen < pageLimit {
				t.Fatalf("page %d returned empty cursor with %d/%d matches seen "+
					"— heavy-filter pagination dropped events (the bug this test guards)",
					page, seen, pageLimit)
			}
			break
		}
		if resp.NextCursor == cursor {
			t.Fatalf("page %d cursor did not advance (cursor=%q) — would loop forever",
				page, cursor)
		}
		cursor = resp.NextCursor
	}
	if seen != pageLimit {
		t.Errorf("saw %d matches across paginated scan, want %d", seen, pageLimit)
	}
}

// itoa is a small i→string helper to keep test bodies readable without
// pulling strconv into every line.
func itoa(n int) string { return fmt.Sprintf("%d", n) }
