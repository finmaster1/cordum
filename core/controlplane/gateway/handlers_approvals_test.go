package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cordum/cordum/core/controlplane/scheduler"
	"github.com/cordum/cordum/core/model"
	pb "github.com/cordum/cordum/core/protocol/pb/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupApprovalJob creates a job in APPROVAL state with the given safety decision.
func setupApprovalJob(t *testing.T, s *server, jobID, tenant string, sd model.SafetyDecisionRecord) {
	t.Helper()
	ctx := context.Background()

	req := &pb.JobRequest{
		JobId: jobID,
		Topic: "test.topic",
		Labels: map[string]string{
			"tenant_id": tenant,
		},
	}
	if err := s.jobStore.SetJobMeta(ctx, req); err != nil {
		t.Fatalf("SetJobMeta: %v", err)
	}
	if err := s.jobStore.SetJobRequest(ctx, req); err != nil {
		t.Fatalf("SetJobRequest: %v", err)
	}
	if err := s.jobStore.SetState(ctx, jobID, model.JobStateApproval); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if err := s.jobStore.SetSafetyDecision(ctx, jobID, sd); err != nil {
		t.Fatalf("SetSafetyDecision: %v", err)
	}
	if tenant != "" {
		if err := s.jobStore.SetTenant(ctx, jobID, tenant); err != nil {
			t.Fatalf("SetTenant: %v", err)
		}
	}
}

// setupApprovalJobWithHash creates an approval job and sets the correct hash
// so the approve handler can validate it.
func setupApprovalJobWithHash(t *testing.T, s *server, jobID, tenant string) {
	t.Helper()
	ctx := context.Background()

	sd := model.SafetyDecisionRecord{
		Decision:         model.SafetyRequireApproval,
		ApprovalRequired: true,
		PolicySnapshot:   "snap-test",
	}
	setupApprovalJob(t, s, jobID, tenant, sd)

	// Recompute the hash from the stored request so it matches at approve time.
	jobReq, err := s.jobStore.GetJobRequest(ctx, jobID)
	require.NoError(t, err)
	hash, err := scheduler.HashJobRequest(jobReq)
	require.NoError(t, err)
	sd.JobHash = hash
	require.NoError(t, s.jobStore.SetSafetyDecision(ctx, jobID, sd))
}

func TestListApprovalsBasic(t *testing.T) {
	s, _, _ := newTestGateway(t)

	// Create two jobs in APPROVAL state.
	for _, id := range []string{"job-list-1", "job-list-2"} {
		setupApprovalJob(t, s, id, "", model.SafetyDecisionRecord{
			Decision:         model.SafetyRequireApproval,
			ApprovalRequired: true,
			PolicySnapshot:   "snap-test",
			JobHash:          "hash-" + id,
		})
	}

	req := httptest.NewRequest("GET", "/api/v1/approvals?include_resolved=false", nil)
	rr := httptest.NewRecorder()
	s.handleListApprovals(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	items := resp["items"].([]any)
	assert.Len(t, items, 2, "should list both approval items")

	// Verify each item has safety decision fields populated.
	for _, raw := range items {
		item := raw.(map[string]any)
		assert.NotNil(t, item["job"], "each item should have a job field")
		assert.NotEmpty(t, item["decision"], "each item should have a decision")
	}
}

func TestListApprovalsEmptySafetyDecision(t *testing.T) {
	s, _, _ := newTestGateway(t)

	// Create a job with an empty safety decision (all zero values).
	setupApprovalJob(t, s, "job-empty-sd", "", model.SafetyDecisionRecord{})

	req := httptest.NewRequest("GET", "/api/v1/approvals?include_resolved=false", nil)
	rr := httptest.NewRecorder()
	s.handleListApprovals(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	items := resp["items"].([]any)
	// Item should still be included with empty/zero fields (not corrupted).
	assert.Len(t, items, 1, "item with empty safety decision should still be listed")
}

func TestApproveJobWithTraceIDError(t *testing.T) {
	s, _, sc := newTestGateway(t)
	sc.setSnapshots([]string{"snap-test"})

	jobID := "job-trace-err"
	setupApprovalJobWithHash(t, s, jobID, "")

	// Do NOT set a trace ID — GetTraceID will return redis.Nil.
	// The approval should still succeed with an empty trace ID.
	body := `{"reason":"test approve"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs/"+jobID+"/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("job_id", jobID)
	rr := httptest.NewRecorder()
	s.handleApproveJob(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "approval should succeed even when trace ID lookup fails; body: %s", rr.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp["job_id"])
	// trace_id should be empty string (not nil, not error).
	assert.Equal(t, "", resp["trace_id"], "trace_id should be empty when lookup fails")
}

func TestRejectJobWithEmptySafetyDecision(t *testing.T) {
	s, _, _ := newTestGateway(t)
	ctx := context.Background()

	jobID := "job-reject-empty-sd"
	// Set up with minimal safety decision — simulates the case where
	// safety decision data is missing/incomplete.
	setupApprovalJob(t, s, jobID, "", model.SafetyDecisionRecord{})

	body := `{"reason":"denied by admin"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs/"+jobID+"/reject", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("job_id", jobID)
	rr := httptest.NewRecorder()
	s.handleRejectJob(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "rejection should succeed with empty safety decision; body: %s", rr.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, jobID, resp["job_id"])

	// Verify the job was actually rejected.
	state, err := s.jobStore.GetState(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, model.JobStateDenied, state)
}

func TestListApprovalsTenantFiltering(t *testing.T) {
	s, _, _ := newTestGateway(t)

	// Create jobs for two different tenants.
	for i, tenant := range []string{"tenant-a", "tenant-b", "tenant-a"} {
		jobID := "job-tenant-" + string(rune('0'+i))
		setupApprovalJob(t, s, jobID, tenant, model.SafetyDecisionRecord{
			Decision:         model.SafetyRequireApproval,
			ApprovalRequired: true,
			PolicySnapshot:   "snap-test",
			JobHash:          "hash" + string(rune('0'+i)),
		})
	}

	// With no auth (s.auth == nil), all tenants pass — should see 3 items.
	req := httptest.NewRequest("GET", "/api/v1/approvals?include_resolved=false", nil)
	rr := httptest.NewRecorder()
	s.handleListApprovals(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	items := resp["items"].([]any)
	assert.Len(t, items, 3, "with no auth, all tenant items should be returned")
}

func TestLockReleaseBoundedContext(t *testing.T) {
	s, _, _ := newTestGateway(t)

	jobID := "job-lock-test"

	// withApprovalLock should acquire, execute, and release the lock cleanly.
	var called bool
	err := s.withApprovalLock(context.Background(), jobID, func(ctx context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called, "lock closure should have been called")

	// Verify the lock was released by successfully re-acquiring it.
	err = s.withApprovalLock(context.Background(), jobID, func(ctx context.Context) error {
		return nil
	})
	require.NoError(t, err, "should be able to re-acquire lock after release")
}

func TestApproveJobIdempotent(t *testing.T) {
	s, _, sc := newTestGateway(t)
	sc.setSnapshots([]string{"snap-test"})

	jobID := "job-idempotent"
	setupApprovalJobWithHash(t, s, jobID, "")

	// First approval.
	body := `{"reason":"approved"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs/"+jobID+"/approve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("job_id", jobID)
	rr := httptest.NewRecorder()
	s.handleApproveJob(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "first approval: %s", rr.Body.String())

	// Second approval (idempotent) — should return OK, not 409.
	req2 := httptest.NewRequest("POST", "/api/v1/jobs/"+jobID+"/approve", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.SetPathValue("job_id", jobID)
	rr2 := httptest.NewRecorder()
	s.handleApproveJob(rr2, req2)
	require.Equal(t, http.StatusOK, rr2.Code, "idempotent approval: %s", rr2.Body.String())

	var resp2 map[string]any
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &resp2))
	assert.Equal(t, "already_approved", resp2["status"])
}

func TestRejectJobIdempotent(t *testing.T) {
	s, _, _ := newTestGateway(t)
	ctx := context.Background()

	jobID := "job-reject-idempotent"
	setupApprovalJob(t, s, jobID, "", model.SafetyDecisionRecord{
		Decision:         model.SafetyRequireApproval,
		ApprovalRequired: true,
		PolicySnapshot:   "snap-test",
		JobHash:          "hash1",
	})

	// First rejection.
	body := `{"reason":"denied"}`
	req := httptest.NewRequest("POST", "/api/v1/jobs/"+jobID+"/reject", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("job_id", jobID)
	rr := httptest.NewRecorder()
	s.handleRejectJob(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "first rejection: %s", rr.Body.String())

	// Verify state is denied.
	state, err := s.jobStore.GetState(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, model.JobStateDenied, state)

	// Second rejection (idempotent) — should return OK with already_rejected.
	req2 := httptest.NewRequest("POST", "/api/v1/jobs/"+jobID+"/reject", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.SetPathValue("job_id", jobID)
	rr2 := httptest.NewRecorder()
	s.handleRejectJob(rr2, req2)
	require.Equal(t, http.StatusOK, rr2.Code, "idempotent rejection: %s", rr2.Body.String())

	var resp2 map[string]any
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &resp2))
	assert.Equal(t, "already_rejected", resp2["status"])
}

func TestApproveJobNotFound(t *testing.T) {
	s, _, _ := newTestGateway(t)

	req := httptest.NewRequest("POST", "/api/v1/jobs/nonexistent/approve", nil)
	req.SetPathValue("job_id", "nonexistent")
	rr := httptest.NewRecorder()
	s.handleApproveJob(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestRejectJobNotFound(t *testing.T) {
	s, _, _ := newTestGateway(t)

	req := httptest.NewRequest("POST", "/api/v1/jobs/nonexistent/reject", nil)
	req.SetPathValue("job_id", "nonexistent")
	rr := httptest.NewRecorder()
	s.handleRejectJob(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}
