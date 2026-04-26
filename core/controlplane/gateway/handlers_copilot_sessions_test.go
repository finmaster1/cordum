package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cordum/cordum/core/controlplane/copilot"
	"github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/model"
	pb "github.com/cordum/cordum/core/protocol/pb/v1"
)

type stubCopilotSessionStore struct {
	session       *copilot.CopilotSession
	err           error
	lastSessionID string
	lastUserID    string
}

func (s *stubCopilotSessionStore) GetSession(_ context.Context, sessionID, userID string) (*copilot.CopilotSession, error) {
	s.lastSessionID = sessionID
	s.lastUserID = userID
	return s.session, s.err
}

func TestHandleGetCopilotSession_HappyPathAggregatesSessionJobsAndDecisions(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.auth = governanceAuth{}
	now := time.Date(2026, time.April, 26, 8, 0, 0, 0, time.UTC)
	store := &stubCopilotSessionStore{
		session: &copilot.CopilotSession{
			ID:        "sess-abc123",
			Title:     "Investigate deployment",
			UserID:    "alice",
			CreatedAt: now.Add(-time.Hour),
			UpdatedAt: now,
			Messages: []copilot.CopilotMessage{
				{
					ID:        "msg-1",
					Role:      "user",
					Content:   "show the failed deployment",
					Timestamp: now.Add(-30 * time.Minute),
					JobIDs:    []string{"job-1"},
				},
				{
					ID:        "msg-2",
					Role:      "assistant",
					Content:   "Found one failed job",
					Timestamp: now.Add(-29 * time.Minute),
					JobIDs:    []string{"job-1"},
				},
			},
			Metadata: map[string]string{"source": "copilot"},
		},
	}
	s.copilotStore = store

	ctx := context.Background()
	jobReq := &pb.JobRequest{
		JobId:    "job-1",
		Topic:    "job.deploy",
		TenantId: "tenant-a",
		Labels:   map[string]string{"session_id": "sess-abc123"},
	}
	if err := s.jobStore.SetJobMeta(ctx, jobReq); err != nil {
		t.Fatalf("SetJobMeta() error = %v", err)
	}
	if err := s.jobStore.SetState(ctx, "job-1", model.JobStatePending); err != nil {
		t.Fatalf("SetState() error = %v", err)
	}
	if err := s.decisionLogStore.AppendDecision(ctx, model.DecisionLogRecord{
		JobID:     "job-1",
		Tenant:    "tenant-a",
		Topic:     "job.deploy",
		AgentID:   "agent-7",
		RuleID:    "rule-allow",
		Verdict:   model.SafetyAllow,
		Reason:    "allowed",
		Timestamp: now.UnixMilli(),
	}); err != nil {
		t.Fatalf("AppendDecision() error = %v", err)
	}

	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/copilot/sessions/sess-abc123", nil), &auth.AuthContext{
		Tenant:      "tenant-a",
		PrincipalID: "alice",
		Role:        "viewer",
	})
	req.SetPathValue("sessionId", "sess-abc123")
	rr := httptest.NewRecorder()

	s.handleGetCopilotSession(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if store.lastSessionID != "sess-abc123" {
		t.Fatalf("lastSessionID=%q want sess-abc123", store.lastSessionID)
	}
	if store.lastUserID != "alice" {
		t.Fatalf("lastUserID=%q want alice", store.lastUserID)
	}

	var resp struct {
		Session struct {
			ID       string `json:"id"`
			UserID   string `json:"userId"`
			Messages []struct {
				ID     string   `json:"id"`
				Role   string   `json:"role"`
				JobIDs []string `json:"jobIds"`
			} `json:"messages"`
		} `json:"session"`
		Jobs []struct {
			ID     string `json:"id"`
			Topic  string `json:"topic"`
			Status string `json:"status"`
		} `json:"jobs"`
		Decisions []struct {
			JobID       string `json:"jobId"`
			MatchedRule string `json:"matchedRule"`
			Verdict     string `json:"verdict"`
			AgentID     string `json:"agentId"`
		} `json:"decisions"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Session.ID != "sess-abc123" || resp.Session.UserID != "alice" {
		t.Fatalf("unexpected session: %#v", resp.Session)
	}
	if len(resp.Session.Messages) != 2 || resp.Session.Messages[0].JobIDs[0] != "job-1" {
		t.Fatalf("unexpected messages: %#v", resp.Session.Messages)
	}
	if len(resp.Jobs) != 1 || resp.Jobs[0].ID != "job-1" || resp.Jobs[0].Status != "pending" {
		t.Fatalf("unexpected jobs: %#v", resp.Jobs)
	}
	if len(resp.Decisions) != 1 || resp.Decisions[0].MatchedRule != "rule-allow" || resp.Decisions[0].Verdict != "allow" {
		t.Fatalf("unexpected decisions: %#v", resp.Decisions)
	}
	if resp.Truncated {
		t.Fatalf("Truncated=true want false")
	}
}

func TestCollectCopilotSessionDecisionsPaginatesPastUnrelatedTenantDecisions(t *testing.T) {
	s, _, _ := newTestGateway(t)
	ctx := context.Background()
	now := time.Date(2026, time.April, 26, 8, 0, 0, 0, time.UTC)
	for i := 0; i < copilotSessionAggregateLimit; i++ {
		if err := s.decisionLogStore.AppendDecision(ctx, model.DecisionLogRecord{
			JobID:     "unrelated-job-" + strconv.Itoa(i),
			Tenant:    "tenant-a",
			Topic:     "job.other",
			AgentID:   "agent-noise",
			RuleID:    "rule-noise",
			Verdict:   model.SafetyAllow,
			Reason:    "noise",
			Timestamp: now.Add(time.Duration(i+2) * time.Millisecond).UnixMilli(),
		}); err != nil {
			t.Fatalf("AppendDecision(unrelated %d) error = %v", i, err)
		}
	}
	if err := s.decisionLogStore.AppendDecision(ctx, model.DecisionLogRecord{
		JobID:     "session-job",
		Tenant:    "tenant-a",
		Topic:     "job.deploy",
		AgentID:   "agent-session",
		RuleID:    "rule-session",
		Verdict:   model.SafetyDeny,
		Reason:    "session decision",
		Timestamp: now.Add(time.Millisecond).UnixMilli(),
	}); err != nil {
		t.Fatalf("AppendDecision(session) error = %v", err)
	}

	decisions, truncated, err := s.collectCopilotSessionDecisions(ctx, "tenant-a", map[string]struct{}{"session-job": {}})
	if err != nil {
		t.Fatalf("collectCopilotSessionDecisions() error = %v", err)
	}
	if truncated {
		t.Fatalf("truncated=true want false after exhausting unrelated first page")
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions)=%d want 1: %#v", len(decisions), decisions)
	}
	if decisions[0].JobID != "session-job" || decisions[0].MatchedRule != "rule-session" || decisions[0].Verdict != "deny" {
		t.Fatalf("unexpected decision: %#v", decisions[0])
	}
}

func TestHandleGetCopilotSession_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		storeErr   error
		wantStatus int
	}{
		{name: "not found", storeErr: copilot.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "cross tenant", storeErr: copilot.ErrCrossTenant, wantStatus: http.StatusForbidden},
		{name: "store failure", storeErr: errors.New("redis unavailable"), wantStatus: http.StatusInternalServerError},
		{name: "not implemented", storeErr: copilot.ErrNotImplemented, wantStatus: http.StatusNotImplemented},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _ := newTestGateway(t)
			s.auth = governanceAuth{}
			s.copilotStore = &stubCopilotSessionStore{err: tt.storeErr}
			req := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/copilot/sessions/sess-abc123", nil), &auth.AuthContext{
				Tenant:      "tenant-a",
				PrincipalID: "alice",
				Role:        "viewer",
			})
			req.SetPathValue("sessionId", "sess-abc123")
			rr := httptest.NewRecorder()

			s.handleGetCopilotSession(rr, req)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status=%d want %d body=%s", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestHandleGetCopilotSession_RejectsInvalidSessionID(t *testing.T) {
	tests := []string{
		"",
		"..",
		"sess%2Fabc",
		"sess abc",
		strings.Repeat("a", 129),
	}

	for _, sessionID := range tests {
		t.Run(sessionID, func(t *testing.T) {
			s, _, _ := newTestGateway(t)
			s.auth = governanceAuth{}
			s.copilotStore = &stubCopilotSessionStore{}
			req := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/copilot/sessions/invalid", nil), &auth.AuthContext{
				Tenant:      "tenant-a",
				PrincipalID: "alice",
				Role:        "viewer",
			})
			req.SetPathValue("sessionId", sessionID)
			rr := httptest.NewRecorder()

			s.handleGetCopilotSession(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("sessionID=%q status=%d body=%s", sessionID, rr.Code, rr.Body.String())
			}
		})
	}
}

func TestHandleGetCopilotSession_RequiresAuthContext(t *testing.T) {
	s, _, _ := newTestGateway(t)
	s.auth = governanceAuth{}
	s.copilotStore = &stubCopilotSessionStore{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/copilot/sessions/sess-abc123", nil)
	req.SetPathValue("sessionId", "sess-abc123")
	rr := httptest.NewRecorder()

	s.handleGetCopilotSession(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}
