package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cordum/cordum/core/controlplane/copilot"
	"github.com/cordum/cordum/core/controlplane/gateway/auth"
	"github.com/cordum/cordum/core/model"
)

const copilotSessionAggregateLimit = 500

var copilotSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type copilotSessionDetailResponse struct {
	Session   copilot.CopilotSession       `json:"session"`
	Jobs      []copilotSessionJobView      `json:"jobs"`
	Decisions []copilotSessionDecisionView `json:"decisions"`
	Truncated bool                         `json:"truncated"`
}

type copilotSessionJobView struct {
	ID           string            `json:"id"`
	Type         string            `json:"type,omitempty"`
	Topic        string            `json:"topic,omitempty"`
	Status       string            `json:"status"`
	Pool         string            `json:"pool,omitempty"`
	Capabilities []string          `json:"capabilities"`
	RiskTags     []string          `json:"riskTags"`
	Metadata     map[string]string `json:"metadata"`
	CreatedAt    string            `json:"createdAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt,omitempty"`
}

type copilotSessionDecisionView struct {
	JobID            string                 `json:"jobId"`
	Topic            string                 `json:"topic,omitempty"`
	MatchedRule      string                 `json:"matchedRule,omitempty"`
	RuleName         string                 `json:"ruleName,omitempty"`
	Verdict          string                 `json:"verdict"`
	Reason           string                 `json:"reason,omitempty"`
	Constraints      any                    `json:"constraints,omitempty"`
	ApprovalStatus   model.ApprovalStatus   `json:"approvalStatus,omitempty"`
	ApprovalDecision model.ApprovalDecision `json:"approvalDecision,omitempty"`
	AgentID          string                 `json:"agentId,omitempty"`
	PolicyVersion    string                 `json:"policyVersion,omitempty"`
	Timestamp        string                 `json:"timestamp"`
}

func (s *server) handleGetCopilotSession(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	sessionID := strings.TrimSpace(r.PathValue("sessionId"))
	if !copilotSessionIDPattern.MatchString(sessionID) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid session id")
		return
	}

	authCtx := auth.FromRequest(r)
	if authCtx == nil || strings.TrimSpace(authCtx.PrincipalID) == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if !s.requirePermissionOrRole(w, r, auth.PermJobsRead) {
		return
	}

	tenant, err := s.resolveTenant(r, "")
	if err != nil {
		writeErrorJSON(w, http.StatusForbidden, "tenant access denied")
		return
	}

	userID := strings.TrimSpace(authCtx.PrincipalID)
	slog.Info("copilot session detail start",
		"session_id", sessionID,
		"principal", copilotLogPrincipal(userID),
		"tenant", tenant,
	)

	store := s.copilotStore
	if store == nil {
		store = copilot.NotImplementedStore{}
	}
	sess, err := store.GetSession(r.Context(), sessionID, userID)
	if err != nil {
		s.handleCopilotSessionStoreError(w, r, sessionID, userID, tenant, started, err)
		return
	}
	if sess == nil {
		s.handleCopilotSessionStoreError(w, r, sessionID, userID, tenant, started, copilot.ErrNotFound)
		return
	}

	jobs, jobIDs, jobsTruncated, err := s.collectCopilotSessionJobs(r.Context(), sessionID, tenant, sess)
	if err != nil {
		writeInternalError(w, r, "get copilot session jobs", err)
		return
	}
	decisions, decisionsTruncated, err := s.collectCopilotSessionDecisions(r.Context(), tenant, jobIDs)
	if err != nil {
		writeInternalError(w, r, "get copilot session decisions", err)
		return
	}
	truncated := jobsTruncated || decisionsTruncated
	if truncated {
		slog.Warn("copilot session detail truncated",
			"session_id", sessionID,
			"principal", copilotLogPrincipal(userID),
			"tenant", tenant,
			"limit", copilotSessionAggregateLimit,
		)
	}

	writeJSON(w, copilotSessionDetailResponse{
		Session:   *sess,
		Jobs:      jobs,
		Decisions: decisions,
		Truncated: truncated,
	})
	slog.Info("copilot session detail end",
		"session_id", sessionID,
		"principal", copilotLogPrincipal(userID),
		"tenant", tenant,
		"latency_ms", time.Since(started).Milliseconds(),
	)
}

func (s *server) handleCopilotSessionStoreError(w http.ResponseWriter, r *http.Request, sessionID, userID, tenant string, started time.Time, err error) {
	slog.Info("copilot session detail end",
		"session_id", sessionID,
		"principal", copilotLogPrincipal(userID),
		"tenant", tenant,
		"latency_ms", time.Since(started).Milliseconds(),
		"error", err,
	)
	switch {
	case errors.Is(err, copilot.ErrNotFound):
		writeErrorJSON(w, http.StatusNotFound, "copilot session not found")
	case errors.Is(err, copilot.ErrCrossTenant):
		writeErrorJSON(w, http.StatusForbidden, "access denied")
	case errors.Is(err, copilot.ErrNotImplemented):
		writeErrorJSON(w, http.StatusNotImplemented, "copilot_store_not_ready")
	default:
		writeInternalError(w, r, "get copilot session", err)
	}
}

func (s *server) collectCopilotSessionJobs(ctx context.Context, sessionID, tenant string, sess *copilot.CopilotSession) ([]copilotSessionJobView, map[string]struct{}, bool, error) {
	jobIDs := orderedCopilotSessionJobIDs(sess)
	jobSet := make(map[string]struct{}, len(jobIDs))
	for _, id := range jobIDs {
		jobSet[id] = struct{}{}
	}

	if s.jobStore == nil {
		return []copilotSessionJobView{}, jobSet, false, nil
	}

	recent, err := s.jobStore.ListRecentJobs(ctx, copilotSessionAggregateLimit)
	if err != nil {
		return nil, nil, false, err
	}
	for _, record := range recent {
		if record.ID == "" || record.Tenant != tenant {
			continue
		}
		if _, ok := jobSet[record.ID]; ok {
			continue
		}
		meta, err := s.jobStore.GetAllMeta(ctx, record.ID)
		if err != nil || len(meta) == 0 {
			continue
		}
		labels := parseCopilotLabels(meta["labels"])
		if labels["session_id"] == sessionID || labels["copilot_session_id"] == sessionID {
			jobSet[record.ID] = struct{}{}
			jobIDs = append(jobIDs, record.ID)
		}
	}

	truncated := false
	if len(jobIDs) > copilotSessionAggregateLimit {
		jobIDs = jobIDs[:copilotSessionAggregateLimit]
		truncated = true
	}

	metas, err := s.jobStore.GetJobMetas(ctx, jobIDs)
	if err != nil {
		return nil, nil, false, err
	}

	jobs := make([]copilotSessionJobView, 0, len(jobIDs))
	for _, id := range jobIDs {
		meta := metas[id]
		if len(meta) == 0 {
			slog.Warn("copilot session job reference missing",
				"session_id", sessionID,
				"tenant", tenant,
				"job_id", id,
			)
			continue
		}
		if tenant != "" && meta["tenant"] != "" && meta["tenant"] != tenant {
			continue
		}
		jobs = append(jobs, copilotJobViewFromMeta(id, meta))
	}
	return jobs, jobSetFromViews(jobs), truncated, nil
}

func (s *server) collectCopilotSessionDecisions(ctx context.Context, tenant string, jobIDs map[string]struct{}) ([]copilotSessionDecisionView, bool, error) {
	if s.decisionLogStore == nil || len(jobIDs) == 0 {
		return []copilotSessionDecisionView{}, false, nil
	}
	page, err := s.decisionLogStore.QueryDecisions(ctx, model.DecisionQuery{
		Tenant: tenant,
		Since:  1,
		Until:  time.Now().UTC().Add(24 * time.Hour).UnixMilli(),
		Limit:  copilotSessionAggregateLimit,
	})
	if err != nil {
		return nil, false, err
	}
	decisions := make([]copilotSessionDecisionView, 0, len(page.Items))
	truncated := page.NextCursor != ""
	for _, record := range page.Items {
		if _, ok := jobIDs[record.JobID]; !ok {
			continue
		}
		if len(decisions) == copilotSessionAggregateLimit {
			truncated = true
			break
		}
		verdict, err := record.Verdict.DecisionLogWireValue()
		if err != nil {
			return nil, false, err
		}
		decisions = append(decisions, copilotSessionDecisionView{
			JobID:            record.JobID,
			Topic:            record.Topic,
			MatchedRule:      record.RuleID,
			Verdict:          verdict,
			Reason:           record.Reason,
			Constraints:      record.Constraints,
			ApprovalStatus:   record.ApprovalStatus,
			ApprovalDecision: record.ApprovalDecision,
			AgentID:          record.AgentID,
			PolicyVersion:    record.PolicyVersion,
			Timestamp:        governanceTimestamp(record.Timestamp),
		})
	}
	return decisions, truncated, nil
}

func orderedCopilotSessionJobIDs(sess *copilot.CopilotSession) []string {
	if sess == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, msg := range sess.Messages {
		for _, id := range msg.JobIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func copilotJobViewFromMeta(id string, meta map[string]string) copilotSessionJobView {
	topic := meta["topic"]
	labels := parseCopilotLabels(meta["labels"])
	status := strings.ToLower(strings.TrimSpace(meta["state"]))
	if status == "" {
		status = "unknown"
	}
	capabilities := []string{}
	if capability := strings.TrimSpace(meta["capability"]); capability != "" {
		capabilities = append(capabilities, capability)
	}
	return copilotSessionJobView{
		ID:           id,
		Type:         topic,
		Topic:        topic,
		Status:       status,
		Pool:         topic,
		Capabilities: capabilities,
		RiskTags:     parseCopilotStringSlice(meta["risk_tags"]),
		Metadata:     labels,
		CreatedAt:    formatCopilotUnixMicros(meta["created_at"]),
		UpdatedAt:    formatCopilotUnixMicros(meta["updated_at"]),
	}
}

func parseCopilotLabels(raw string) map[string]string {
	labels := map[string]string{}
	if strings.TrimSpace(raw) == "" {
		return labels
	}
	if err := json.Unmarshal([]byte(raw), &labels); err != nil {
		return map[string]string{}
	}
	return labels
}

func parseCopilotStringSlice(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	return values
}

func formatCopilotUnixMicros(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	micros, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || micros <= 0 {
		return ""
	}
	return time.UnixMicro(micros).UTC().Format(time.RFC3339Nano)
}

func jobSetFromViews(jobs []copilotSessionJobView) map[string]struct{} {
	out := make(map[string]struct{}, len(jobs))
	for _, job := range jobs {
		out[job.ID] = struct{}{}
	}
	return out
}

func copilotLogPrincipal(principal string) string {
	principal = strings.TrimSpace(principal)
	if len(principal) <= 8 {
		return principal
	}
	return principal[:8]
}

func sortedCopilotJobIDs(jobSet map[string]struct{}) []string {
	ids := make([]string, 0, len(jobSet))
	for id := range jobSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
