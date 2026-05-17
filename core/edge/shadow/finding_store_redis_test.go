package shadow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	edgecore "github.com/cordum/cordum/core/edge"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T, opts ...StoreOption) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), PoolSize: 1})
	t.Cleanup(func() { _ = client.Close(); mr.Close() })
	now := time.Date(2026, 5, 17, 13, 0, 0, 0, time.UTC)
	var clockCounter, idCounter int
	defaults := []StoreOption{
		WithClock(func() time.Time {
			t := now.Add(time.Duration(clockCounter) * time.Second)
			clockCounter++
			return t
		}),
		WithIDGen(func() string {
			idCounter++
			// 32-hex-char deterministic id keyed by counter; the
			// findingIDPrefix is applied by the normaliser.
			return strings.Repeat("0", 28) + fmt.Sprintf("%04x", idCounter)
		}),
	}
	s, err := NewRedisStore(client, append(defaults, opts...)...)
	if err != nil {
		t.Fatalf("NewRedisStore: %v", err)
	}
	return s, mr
}

func minimalCreateReq(tenant, owner, principal, product string, risk FindingRisk, evType, summary string) CreateFindingRequest {
	return CreateFindingRequest{
		TenantID:         tenant,
		OwnerPrincipalID: owner,
		PrincipalID:      principal,
		AgentProduct:     product,
		Risk:             risk,
		EvidenceType:     evType,
		EvidenceSummary:  summary,
		DetectedAt:       time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
	}
}

func TestCreateFinding_PersistsAllRequiredFields(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	req := minimalCreateReq("tenant-a", "owner-1", "principal-1", "claude-code", FindingRiskHigh, "config_file", "2 mcp servers configured")
	req.AgentID = "agent-xyz"
	req.Hostname = "dev-mac-01"
	req.RedactedPath = "C:\\Users\\yaron\\.claude.json"
	req.Metadata = map[string]string{"detector_version": "1.2.3"}

	got, err := s.CreateFinding(ctx, req)
	if err != nil {
		t.Fatalf("CreateFinding: %v", err)
	}
	if got.FindingID == "" || !strings.HasPrefix(got.FindingID, findingIDPrefix) {
		t.Fatalf("FindingID %q lacks prefix %q", got.FindingID, findingIDPrefix)
	}
	if got.TenantID != "tenant-a" || got.OwnerPrincipalID != "owner-1" || got.PrincipalID != "principal-1" {
		t.Fatalf("identity fields unexpected: %+v", got)
	}
	if got.Status != FindingStatusDetected {
		t.Fatalf("Status = %q, want detected", got.Status)
	}
	if got.RedactedPath == "" || strings.Contains(got.RedactedPath, "yaron") {
		t.Fatalf("RedactedPath not home-stripped: %q", got.RedactedPath)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.IsZero() {
		t.Fatalf("timestamps zero: %+v", got)
	}
}

func TestCreateFinding_RejectsMissingEvidence(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	req := minimalCreateReq("t1", "o1", "p1", "claude-code", FindingRiskLow, "config_file", "")
	// no summary, no artifact pointer
	_, err := s.CreateFinding(ctx, req)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("missing-evidence error = %v, want ErrValidation", err)
	}
}

func TestCreateFinding_RedactsSecretsInSummary(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	req := minimalCreateReq("t1", "o1", "p1", "claude-code", FindingRiskHigh, "config_file",
		"sk-ant-abcdef1234567890ABCDEFGHIJ saw key in config")
	got, err := s.CreateFinding(ctx, req)
	if err != nil {
		t.Fatalf("CreateFinding: %v", err)
	}
	if strings.Contains(got.EvidenceSummary, "sk-ant-") {
		t.Fatalf("EvidenceSummary still contains raw secret: %q", got.EvidenceSummary)
	}
	if !strings.Contains(got.EvidenceSummary, "<REDACTED>") {
		t.Fatalf("EvidenceSummary should have REDACTED marker: %q", got.EvidenceSummary)
	}
}

func TestCreateFinding_RejectsOversizedSummary(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	huge := strings.Repeat("a", MaxEvidenceSummaryBytes+1)
	req := minimalCreateReq("t1", "o1", "p1", "claude-code", FindingRiskLow, "config_file", huge)
	_, err := s.CreateFinding(ctx, req)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("oversized-summary error = %v, want ErrValidation", err)
	}
}

func TestCreateFinding_ArtifactPointerTenantMustMatch(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	req := minimalCreateReq("t1", "o1", "p1", "claude-code", FindingRiskMedium, "config_file", "")
	req.EvidenceArtifact = &EvidencePointer{
		TenantID:       "other-tenant",
		URI:            "s3://shadow-bucket/x.json",
		SHA256:         "deadbeef",
		RetentionClass: edgecore.RetentionClassStandard,
		RedactionLevel: edgecore.RedactionLevelStandard,
		CreatedAt:      time.Date(2026, 5, 17, 11, 0, 0, 0, time.UTC),
	}
	_, err := s.CreateFinding(ctx, req)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("mismatched-tenant-pointer error = %v, want ErrValidation", err)
	}
}

func TestGetFinding_TenantIsolation(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	a, err := s.CreateFinding(ctx, minimalCreateReq("tenant-a", "o", "p", "claude-code", FindingRiskHigh, "config_file", "summary"))
	if err != nil {
		t.Fatalf("CreateFinding: %v", err)
	}
	// Same id, different tenant lookup → ErrNotFound.
	if _, err := s.GetFinding(ctx, "tenant-b", a.FindingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant get = %v, want ErrNotFound", err)
	}
	got, err := s.GetFinding(ctx, "tenant-a", a.FindingID)
	if err != nil {
		t.Fatalf("same-tenant get: %v", err)
	}
	if got.FindingID != a.FindingID {
		t.Fatalf("got %q, want %q", got.FindingID, a.FindingID)
	}
}

func TestListFindings_FiltersAndTenantIsolation(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	// Two tenants, mixed risk/status/agent_product.
	for i := 0; i < 5; i++ {
		req := minimalCreateReq("tenant-a", "owner-1", "p", "claude-code", FindingRiskHigh, "config_file", "summary-a")
		if _, err := s.CreateFinding(ctx, req); err != nil {
			t.Fatalf("CreateFinding tenant-a: %v", err)
		}
	}
	for i := 0; i < 3; i++ {
		req := minimalCreateReq("tenant-a", "owner-2", "p", "codex", FindingRiskMedium, "process_name", "summary-codex")
		if _, err := s.CreateFinding(ctx, req); err != nil {
			t.Fatalf("CreateFinding tenant-a codex: %v", err)
		}
	}
	for i := 0; i < 4; i++ {
		req := minimalCreateReq("tenant-b", "owner-1", "p", "claude-code", FindingRiskHigh, "config_file", "summary-b")
		if _, err := s.CreateFinding(ctx, req); err != nil {
			t.Fatalf("CreateFinding tenant-b: %v", err)
		}
	}

	tcs := []struct {
		name    string
		q       ListFindingsQuery
		wantMin int
		wantMax int
	}{
		{name: "tenant-a all", q: ListFindingsQuery{TenantID: "tenant-a"}, wantMin: 8, wantMax: 8},
		{name: "tenant-b all", q: ListFindingsQuery{TenantID: "tenant-b"}, wantMin: 4, wantMax: 4},
		{name: "tenant-a risk=high", q: ListFindingsQuery{TenantID: "tenant-a", Risk: FindingRiskHigh}, wantMin: 5, wantMax: 5},
		{name: "tenant-a agent=codex", q: ListFindingsQuery{TenantID: "tenant-a", AgentProduct: "codex"}, wantMin: 3, wantMax: 3},
		{name: "tenant-a owner-2", q: ListFindingsQuery{TenantID: "tenant-a", OwnerPrincipalID: "owner-2"}, wantMin: 3, wantMax: 3},
		{name: "tenant-a status=detected", q: ListFindingsQuery{TenantID: "tenant-a", Status: FindingStatusDetected}, wantMin: 8, wantMax: 8},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.ListFindings(ctx, tc.q)
			if err != nil {
				t.Fatalf("ListFindings: %v", err)
			}
			n := len(page.Findings)
			if n < tc.wantMin || n > tc.wantMax {
				t.Fatalf("got %d findings, want [%d, %d]", n, tc.wantMin, tc.wantMax)
			}
			// Tenant isolation invariant: every returned finding's tenant
			// matches the query tenant.
			for _, f := range page.Findings {
				if f.TenantID != tc.q.TenantID {
					t.Errorf("got finding from tenant %q, want %q", f.TenantID, tc.q.TenantID)
				}
			}
		})
	}
}

func TestListFindings_PaginationViaCursor(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 12; i++ {
		req := minimalCreateReq("tenant-a", "o", "p", "claude-code", FindingRiskHigh, "config_file", "summary")
		if _, err := s.CreateFinding(ctx, req); err != nil {
			t.Fatalf("CreateFinding: %v", err)
		}
	}
	page1, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-a", Limit: 5})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Findings) != 5 {
		t.Fatalf("page1 len = %d, want 5", len(page1.Findings))
	}
	if page1.NextCursor == "" {
		t.Fatalf("expected NextCursor on full page")
	}
	page2, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-a", Limit: 5, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Findings) == 0 {
		t.Fatalf("page2 empty; want >0 with NextCursor from page1")
	}
	// No overlap between pages.
	seen := map[string]bool{}
	for _, f := range page1.Findings {
		seen[f.FindingID] = true
	}
	for _, f := range page2.Findings {
		if seen[f.FindingID] {
			t.Fatalf("page2 returns id %q already seen on page1", f.FindingID)
		}
	}
}

func TestListFindings_InvalidCursor(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	_, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-a", Cursor: "not-a-cursor"})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid-cursor error = %v, want ErrInvalidCursor", err)
	}
}

// TestClampListPageSize pins the bound used at every make() site that
// allocates per-request scratch buffers from a caller-supplied page
// limit. Resolves CodeQL go/allocation-size-overflow alerts on
// finding_store_redis.go:394 + 491 + 492 (PR #276) by making the
// bound a single named helper that static analysis can recognize
// as a sanitizer. See task-8002b1ee.
func TestClampListPageSize(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero falls back to default", 0, DefaultListPageSize},
		{"negative falls back to default", -1, DefaultListPageSize},
		{"large negative falls back to default", -1 << 30, DefaultListPageSize},
		{"one is preserved", 1, 1},
		{"default value passes through", DefaultListPageSize, DefaultListPageSize},
		{"mid-range value passes through", 100, 100},
		{"max boundary passes through", MaxListPageSize, MaxListPageSize},
		{"one above max is capped", MaxListPageSize + 1, MaxListPageSize},
		{"large value is capped", 10_000, MaxListPageSize},
		{"adversarial maxint is capped", 1 << 30, MaxListPageSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := clampListPageSize(tc.in)
			if got != tc.want {
				t.Fatalf("clampListPageSize(%d) = %d, want %d", tc.in, got, tc.want)
			}
			if got < 1 || got > MaxListPageSize {
				t.Fatalf("clampListPageSize(%d) = %d is outside [1, %d]; the bound is what CodeQL relies on", tc.in, got, MaxListPageSize)
			}
		})
	}
}

// TestListFindings_LimitCapAcrossPaths verifies the page-size bound
// holds end-to-end on BOTH the single-signal path (ListFindings) and
// the multi-signal path (listFindingsByMultiSignal). Adversarial
// limits beyond MaxListPageSize must not over-allocate; both paths
// must cap at MaxListPageSize. Pairs with TestClampListPageSize to
// guard the EDGE-141 + EDGE-143.5 surfaces flagged by CodeQL.
func TestListFindings_LimitCapAcrossPaths(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	const total = MaxListPageSize + 25
	for i := 0; i < total; i++ {
		req := minimalCreateReq("tenant-cap", "owner-1", "principal-1", "claude-code", FindingRiskHigh, "config_file", "summary")
		req.SignalSet = []string{"namespace_untenanted", "unmanaged_workload"}
		if _, err := s.CreateFinding(ctx, req); err != nil {
			t.Fatalf("CreateFinding[%d]: %v", i, err)
		}
	}
	t.Run("single-signal path caps at MaxListPageSize", func(t *testing.T) {
		page, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-cap", Limit: 1 << 30})
		if err != nil {
			t.Fatalf("ListFindings: %v", err)
		}
		if len(page.Findings) > MaxListPageSize {
			t.Fatalf("single-signal page len = %d, want <= %d", len(page.Findings), MaxListPageSize)
		}
		if len(page.Findings) != MaxListPageSize {
			t.Fatalf("single-signal page len = %d, want exactly %d (enough data exists)", len(page.Findings), MaxListPageSize)
		}
	})
	t.Run("multi-signal path caps at MaxListPageSize", func(t *testing.T) {
		page, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-cap", Limit: 1 << 30, Signals: []string{"namespace_untenanted", "unmanaged_workload"}})
		if err != nil {
			t.Fatalf("ListFindings(multi-signal): %v", err)
		}
		if len(page.Findings) > MaxListPageSize {
			t.Fatalf("multi-signal page len = %d, want <= %d", len(page.Findings), MaxListPageSize)
		}
		if len(page.Findings) != MaxListPageSize {
			t.Fatalf("multi-signal page len = %d, want exactly %d (enough data exists)", len(page.Findings), MaxListPageSize)
		}
	})
	t.Run("zero limit falls back to default", func(t *testing.T) {
		page, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-cap"})
		if err != nil {
			t.Fatalf("ListFindings(zero-limit): %v", err)
		}
		if len(page.Findings) != DefaultListPageSize {
			t.Fatalf("zero-limit page len = %d, want %d", len(page.Findings), DefaultListPageSize)
		}
	})
}

func TestResolveFinding_TerminalAndIdempotent(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	f, err := s.CreateFinding(ctx, minimalCreateReq("tenant-a", "o", "p", "claude-code", FindingRiskHigh, "config_file", "summary"))
	if err != nil {
		t.Fatalf("CreateFinding: %v", err)
	}
	resolved, err := s.ResolveFinding(ctx, "tenant-a", f.FindingID, ResolveRequest{
		ResolvedBy: "alice",
		Reason:     "operator uninstalled the shadow agent",
	})
	if err != nil {
		t.Fatalf("ResolveFinding: %v", err)
	}
	if resolved.Status != FindingStatusResolved {
		t.Fatalf("Status = %q, want resolved", resolved.Status)
	}
	if resolved.ResolvedAt == nil || resolved.ResolvedBy != "alice" {
		t.Fatalf("resolution fields not set: %+v", resolved)
	}
	// Idempotent: re-resolve is a no-op success.
	again, err := s.ResolveFinding(ctx, "tenant-a", f.FindingID, ResolveRequest{ResolvedBy: "alice", Reason: "again"})
	if err != nil {
		t.Fatalf("re-resolve: %v", err)
	}
	if again.Status != FindingStatusResolved {
		t.Fatalf("re-resolve Status = %q, want resolved", again.Status)
	}
	// Cannot suppress a resolved finding.
	if _, err := s.SuppressFinding(ctx, "tenant-a", f.FindingID, SuppressRequest{SuppressedBy: "alice"}); !errors.Is(err, ErrTerminalConflict) {
		t.Fatalf("suppress-after-resolve = %v, want ErrTerminalConflict", err)
	}
}

func TestSuppressFinding_TerminalAndIdempotent(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	f, err := s.CreateFinding(ctx, minimalCreateReq("tenant-a", "o", "p", "claude-code", FindingRiskMedium, "config_file", "summary"))
	if err != nil {
		t.Fatalf("CreateFinding: %v", err)
	}
	until := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	suppressed, err := s.SuppressFinding(ctx, "tenant-a", f.FindingID, SuppressRequest{
		SuppressedBy:    "bob",
		Reason:          "approved exception per change ticket",
		SuppressedUntil: &until,
	})
	if err != nil {
		t.Fatalf("SuppressFinding: %v", err)
	}
	if suppressed.Status != FindingStatusSuppressed {
		t.Fatalf("Status = %q, want suppressed", suppressed.Status)
	}
	if suppressed.SuppressedUntil == nil || !suppressed.SuppressedUntil.Equal(until) {
		t.Fatalf("SuppressedUntil = %v, want %v", suppressed.SuppressedUntil, until)
	}
	// Listing by status=suppressed must include it; status=detected must not.
	listSup, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-a", Status: FindingStatusSuppressed})
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	if len(listSup.Findings) != 1 || listSup.Findings[0].FindingID != f.FindingID {
		t.Fatalf("suppressed list = %+v, want [%s]", listSup.Findings, f.FindingID)
	}
	listDet, err := s.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-a", Status: FindingStatusDetected})
	if err != nil {
		t.Fatalf("ListFindings detected: %v", err)
	}
	if len(listDet.Findings) != 0 {
		t.Fatalf("detected list = %+v, want empty", listDet.Findings)
	}
}

func TestTerminalRetention_HidesExpiredAndCleansIndex(t *testing.T) {
	now := time.Date(2026, 5, 17, 13, 0, 0, 0, time.UTC)
	clock := now
	store, _ := newTestStore(t, WithTerminalRetention(time.Hour), WithClock(func() time.Time { return clock }))
	ctx := context.Background()
	f, err := store.CreateFinding(ctx, minimalCreateReq("tenant-a", "o", "p", "claude-code", FindingRiskHigh, "config_file", "summary"))
	if err != nil {
		t.Fatalf("CreateFinding: %v", err)
	}
	if _, err := store.ResolveFinding(ctx, "tenant-a", f.FindingID, ResolveRequest{ResolvedBy: "alice", Reason: "done"}); err != nil {
		t.Fatalf("ResolveFinding: %v", err)
	}
	// Within retention: still listable + gettable.
	got, err := store.GetFinding(ctx, "tenant-a", f.FindingID)
	if err != nil || got == nil {
		t.Fatalf("within-retention get = (%v, %v), want non-nil + nil err", got, err)
	}
	// Past retention: hidden from get + list.
	clock = clock.Add(2 * time.Hour)
	if _, err := store.GetFinding(ctx, "tenant-a", f.FindingID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("post-retention get = %v, want ErrNotFound", err)
	}
	page, err := store.ListFindings(ctx, ListFindingsQuery{TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("ListFindings: %v", err)
	}
	if len(page.Findings) != 0 {
		t.Fatalf("post-retention list = %+v, want empty", page.Findings)
	}
}

func TestRedisStore_NilClientReturnsStoreUnavailable(t *testing.T) {
	got, err := NewRedisStore(nil)
	if err != nil {
		t.Fatalf("NewRedisStore(nil) err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("NewRedisStore(nil) = %v, want nil", got)
	}
	var s *RedisStore
	if _, err := s.CreateFinding(context.Background(), CreateFindingRequest{}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("nil-store create = %v, want ErrStoreUnavailable", err)
	}
}

func TestRedisStore_StoreClosedSurfacesError(t *testing.T) {
	s, mr := newTestStore(t)
	ctx := context.Background()
	mr.Close()
	// CreateFinding probes via GET first; expect a non-nil error.
	_, err := s.CreateFinding(ctx, minimalCreateReq("t", "o", "p", "claude-code", FindingRiskHigh, "config_file", "summary"))
	if err == nil {
		t.Fatalf("expected error after miniredis close, got nil")
	}
}
