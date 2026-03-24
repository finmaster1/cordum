package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cordum/cordum/core/controlplane/scheduler"
	"github.com/cordum/cordum/core/infra/bus"
	"github.com/cordum/cordum/core/infra/store"
	"github.com/cordum/cordum/core/model"
	capsdk "github.com/cordum/cordum/core/protocol/capsdk"
	pb "github.com/cordum/cordum/core/protocol/pb/v1"
	wf "github.com/cordum/cordum/core/workflow"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	if s.workflowEng == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "workflow engine unavailable")
		return
	}
	if err := s.requireRole(r, "admin"); err != nil {
		writeForbidden(w, r, err)
		return
	}
	runID := r.PathValue("run_id")
	if runID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "missing run_id")
		return
	}
	if s.workflowStore != nil {
		if run, err := s.workflowStore.GetRun(r.Context(), runID); err == nil && run != nil {
			if err := s.requireTenantAccess(r, run.OrgID); err != nil {
				writeErrorJSON(w, http.StatusForbidden, "tenant access denied")
				return
			}
		}
	}

	// Serialize workflow run mutations with the same lock used by the workflow-engine reconciler.
	if s.jobStore != nil {
		lockKey := "cordum:wf:run:lock:" + runID
		token, err := s.jobStore.TryAcquireLock(r.Context(), lockKey, 30*time.Second)
		if err != nil || token == "" {
			writeErrorJSON(w, http.StatusConflict, "workflow run is busy, retry")
			return
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.jobStore.ReleaseLock(ctx, lockKey, token); err != nil {
				slog.Warn("approval lock release failed, will expire via TTL",
					"lock_key", lockKey, "error", err)
			}
		}()
	}

	if err := s.workflowEng.CancelRun(r.Context(), runID); err != nil {
		writeInternalError(w, r, "cancel run", err)
		return
	}
	cancelRunWfName := ""
	if s.workflowStore != nil {
		if run, err := s.workflowStore.GetRun(r.Context(), runID); err == nil && run != nil {
			if wfDef, err := s.workflowStore.GetWorkflow(r.Context(), run.WorkflowID); err == nil && wfDef != nil {
				cancelRunWfName = wfDef.Name
			}
		}
	}
	s.appendAuditEntryNamed(r.Context(), "cancel", "run", runID, cancelRunWfName, policyActorID(r), policyRole(r), "cancel run "+runID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	if s.jobStore == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "job store unavailable")
		return
	}
	if err := s.requireRole(r, "admin"); err != nil {
		writeForbidden(w, r, err)
		return
	}
	limit, cursor := parsePagination(r, 100)
	// Overfetch slightly to account for items that will be filtered out
	// by tenant access checks, so the client receives close to `limit` items.
	fetchLimit := limit + 10
	// Pending approvals (APPROVAL_REQUIRED state).
	jobs, err := s.jobStore.ListJobsByState(r.Context(), model.JobStateApproval, cursor, fetchLimit)
	if err != nil {
		writeInternalError(w, r, "list approvals", err)
		return
	}
	// Also include recently resolved approvals from post-approval states.
	// These are jobs that passed through the approval flow and have an
	// ApprovalRecord, now in PENDING (approved), DENIED, SUCCEEDED, or FAILED.
	includeResolved := r.URL.Query().Get("include_resolved") != "false"
	if includeResolved {
		resolvedLimit := fetchLimit - int64(len(jobs))
		if resolvedLimit < 0 {
			resolvedLimit = 0
		}
		seenIDs := make(map[string]bool, len(jobs))
		for _, j := range jobs {
			seenIDs[j.ID] = true
		}
		for _, state := range []model.JobState{model.JobStatePending, model.JobStateDenied, model.JobStateSucceeded, model.JobStateFailed} {
			if resolvedLimit <= 0 {
				break
			}
			resolved, err := s.jobStore.ListJobsByState(r.Context(), state, cursor, resolvedLimit)
			if err != nil {
				slog.Warn("list approvals: resolved jobs query failed",
					"state", string(state), "error", err)
				continue
			}
			for _, rj := range resolved {
				if seenIDs[rj.ID] {
					continue
				}
				// Only include jobs that have an approval record (went through approval flow).
				approval, aprErr := s.jobStore.GetApprovalRecord(r.Context(), rj.ID)
				if aprErr != nil {
					slog.Warn("list approvals: approval record lookup failed for resolved job",
						"job_id", rj.ID, "state", string(state), "error", aprErr)
					continue
				}
				if approval.ApprovedBy == "" {
					continue
				}
				jobs = append(jobs, rj)
				seenIDs[rj.ID] = true
				resolvedLimit--
			}
		}
	}
	items := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		if err := s.requireTenantAccess(r, job.Tenant); err != nil {
			slog.Debug("list approvals: skipping item for tenant mismatch",
				"job_id", job.ID, "tenant", job.Tenant)
			continue
		}
		record, sdErr := s.jobStore.GetSafetyDecision(r.Context(), job.ID)
		if sdErr != nil {
			slog.Warn("list approvals: safety decision unavailable, skipping item",
				"job_id", job.ID, "error", sdErr)
			continue
		}
		item := map[string]any{
			"job":               job,
			"decision":          record.Decision,
			"policy_snapshot":   record.PolicySnapshot,
			"policy_rule_id":    record.RuleID,
			"policy_reason":     record.Reason,
			"constraints":       record.Constraints,
			"job_hash":          record.JobHash,
			"approval_required": record.ApprovalRequired,
			"approval_ref":      record.ApprovalRef,
		}
		// Merge approval resolution fields when an approval record exists.
		if approval, err := s.jobStore.GetApprovalRecord(r.Context(), job.ID); err == nil && approval.ApprovedBy != "" {
			item["resolved_by"] = approval.ApprovedBy
			item["resolved_comment"] = approval.Note
			item["resolution"] = approval.Reason
			if approval.ApprovedAt > 0 {
				item["resolved_at"] = approval.ApprovedAt
			}
		}
		// Enrich with workflow labels from the original job request so the
		// dashboard can distinguish gate approvals from policy approvals.
		// Also skip approvals whose workflow run has already terminated.
		if req, err := s.jobStore.GetJobRequest(r.Context(), job.ID); err == nil && req != nil {
			if req.Labels != nil {
				// Filter out stale approvals: if the run is terminal, skip this item.
				if runID := strings.TrimSpace(req.Labels["run_id"]); runID != "" && s.workflowStore != nil {
					if run, runErr := s.workflowStore.GetRun(r.Context(), runID); runErr == nil && run != nil {
						if wf.IsTerminalRunStatus(run.Status) {
							continue
						}
					}
				}
				if v := req.Labels["workflow_id"]; v != "" {
					item["workflow_id"] = v
				}
				if v := req.Labels["run_id"]; v != "" {
					item["workflow_run_id"] = v
				}
				if v := req.Labels["step_id"]; v != "" {
					item["step_name"] = v
				}
				if v := req.Labels["gate_type"]; v != "" {
					item["gate_type"] = v
				}
			}
			// Dereference context_ptr to include the actual job input payload
			// (e.g. transfer amount, customer, reason) so approvers see what
			// they are approving.
			if ptr := strings.TrimSpace(req.GetContextPtr()); ptr != "" {
				item["context_ptr"] = ptr
				if s.memStore != nil {
					if key, err := store.KeyFromPointer(ptr); err == nil {
						if raw, err := s.memStore.GetContext(r.Context(), key); err == nil && len(raw) > 0 {
							var payload map[string]any
							if err := json.Unmarshal(raw, &payload); err == nil {
								item["job_input"] = payload
							}
						}
					}
				}
			}
		}
		items = append(items, item)
	}
	// Cap items to the requested limit (we may have overfetched).
	if int64(len(items)) > limit {
		items = items[:limit]
	}
	// Set pagination cursor based on actual results: if we fetched
	// at least `limit` items worth of data, there may be more pages.
	var nextCursor *int64
	if int64(len(jobs)) >= limit {
		nc := jobs[len(jobs)-1].UpdatedAt - 1
		nextCursor = &nc
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]any{
		"items":       items,
		"next_cursor": nextCursor,
	})
}

// approvalLockPrefix must match the scheduler's jobLockPrefix so the
// gateway and scheduler coordinate on the same distributed lock.
const approvalLockPrefix = "cordum:scheduler:job:"

// approvalLockTTL is the distributed lock TTL for approval operations.
const approvalLockTTL = 10 * time.Second

// handlerResult carries the HTTP status and body from inside a lock closure
// back to the handler's response writer.
type handlerResult struct {
	status int
	body   any
}

// withApprovalLock acquires a per-job distributed lock, executes fn, and
// releases the lock on return. Returns store.ErrLockBusy-style error if
// the lock cannot be acquired within a short deadline.
func (s *server) withApprovalLock(ctx context.Context, jobID string, fn func(ctx context.Context) error) error {
	key := approvalLockPrefix + jobID
	lockStart := time.Now()
	deadline := lockStart.Add(2 * time.Second)
	var token string
	var err error
	for {
		lockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		token, err = s.jobStore.TryAcquireLock(lockCtx, key, approvalLockTTL)
		cancel()
		if err != nil {
			slog.Error("approval lock acquire failed", "job_id", jobID, "error", err)
			return fmt.Errorf("lock acquire: %w", err)
		}
		if token != "" {
			break
		}
		if time.Now().After(deadline) {
			slog.Warn("approval lock busy", "job_id", jobID, "waited_ms", time.Since(lockStart).Milliseconds())
			return fmt.Errorf("approval lock busy")
		}
		time.Sleep(25 * time.Millisecond)
	}
	slog.Debug("approval lock acquired", "job_id", jobID, "wait_ms", time.Since(lockStart).Milliseconds())
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if rErr := s.jobStore.ReleaseLock(releaseCtx, key, token); rErr != nil {
			slog.Warn("approval lock release failed", "job_id", jobID, "error", rErr)
		}
	}()
	return fn(ctx)
}

func (s *server) handleApproveJob(w http.ResponseWriter, r *http.Request) {
	if s.jobStore == nil || s.bus == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "job store or bus unavailable")
		return
	}
	if err := s.requireRole(r, "admin"); err != nil {
		writeForbidden(w, r, err)
		return
	}
	var body struct {
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		if !errors.Is(err, io.EOF) {
			writeJSONDecodeError(w, err, "invalid body")
			return
		}
	}
	jobID := r.PathValue("job_id")
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "missing job_id")
		return
	}

	// Acquire distributed lock so concurrent approve/reject requests are
	// serialised. Uses the same key prefix as the scheduler engine.
	var result handlerResult
	lockErr := s.withApprovalLock(r.Context(), jobID, func(ctx context.Context) error {
		state, err := s.jobStore.GetState(ctx, jobID)
		if err != nil {
			result = handlerResult{http.StatusNotFound, "job not found"}
			return nil
		}

		// Idempotency: if the job was already approved (state moved past
		// APPROVAL), return the existing approval record instead of 409.
		if state != model.JobStateApproval {
			if state == model.JobStatePending || state == model.JobStateSucceeded ||
				state == model.JobStateScheduled || state == model.JobStateDispatched ||
				state == model.JobStateRunning {
				req, reqErr := s.jobStore.GetJobRequest(ctx, jobID)
				if reqErr != nil {
					slog.Warn("approve: idempotency check skipped, job request lookup failed",
						"job_id", jobID, "error", reqErr)
				}
				if req != nil && req.Labels != nil && req.Labels["approval_granted"] == "true" {
					rec, recErr := s.jobStore.GetApprovalRecord(ctx, jobID)
					if recErr != nil {
						slog.Warn("approve: idempotent approval record lookup failed",
							"job_id", jobID, "error", recErr)
					}
					traceID, traceErr := s.jobStore.GetTraceID(ctx, jobID)
					if traceErr != nil {
						slog.Warn("approve: idempotent trace ID lookup failed",
							"job_id", jobID, "error", traceErr)
					}
					slog.Info("approval idempotent — already approved",
						"job_id", jobID, "trace_id", traceID,
						"state", string(state), "approved_by", rec.ApprovedBy,
						"actor", policyActorID(r))
					result = handlerResult{http.StatusOK, map[string]any{
						"job_id":      jobID,
						"trace_id":    traceID,
						"status":      "already_approved",
						"approved_by": rec.ApprovedBy,
						"approved_at": rec.ApprovedAt,
					}}
					return nil
				}
			}
			s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), "job not awaiting approval (state="+string(state)+")")
			result = handlerResult{http.StatusConflict, "job not awaiting approval"}
			return nil
		}

		if tenant, tenantErr := s.jobStore.GetTenant(ctx, jobID); tenantErr != nil {
			slog.Warn("approve: tenant lookup failed", "job_id", jobID, "error", tenantErr)
		} else if tenant != "" {
			if err := s.requireTenantAccess(r, tenant); err != nil {
				result = handlerResult{http.StatusForbidden, "tenant access denied"}
				return nil
			}
		}
		req, err := s.jobStore.GetJobRequest(ctx, jobID)
		if err != nil {
			result = handlerResult{http.StatusNotFound, "job request not found"}
			return nil
		}

		// Check if workflow run is terminal (under lock to prevent TOCTOU).
		if req.Labels != nil {
			if runID := strings.TrimSpace(req.Labels["run_id"]); runID != "" && s.workflowStore != nil {
				if run, runErr := s.workflowStore.GetRun(ctx, runID); runErr == nil && run != nil {
					if wf.IsTerminalRunStatus(run.Status) {
						msg := fmt.Sprintf("workflow run %s — approval no longer valid", run.Status)
						s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), msg)
						result = handlerResult{http.StatusConflict, msg}
						return nil
					}
				}
			}
		}

		safetyRecord, err := s.jobStore.GetSafetyDecision(ctx, jobID)
		if err != nil {
			result = handlerResult{http.StatusServiceUnavailable, "safety decision unavailable"}
			return nil
		}
		if !safetyRecord.ApprovalRequired && safetyRecord.Decision != model.SafetyRequireApproval {
			s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), "job not awaiting approval (safety record)")
			result = handlerResult{http.StatusConflict, "job not awaiting approval"}
			return nil
		}
		if safetyRecord.JobHash == "" {
			s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), "approval job hash unavailable")
			result = handlerResult{http.StatusConflict, "approval job hash unavailable"}
			return nil
		}
		isWorkflowGate := strings.TrimSpace(req.GetTopic()) == capsdk.SubjectApprovalGate
		if !isWorkflowGate && req.Labels != nil && strings.EqualFold(strings.TrimSpace(req.Labels["gate_type"]), "workflow_approval") {
			isWorkflowGate = true
		}
		policySnapshot := strings.TrimSpace(safetyRecord.PolicySnapshot)
		if isWorkflowGate {
			if policySnapshot == "" {
				policySnapshot = "workflow-gate"
			}
		} else {
			if policySnapshot == "" {
				s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), "approval policy snapshot unavailable")
				result = handlerResult{http.StatusConflict, "approval policy snapshot unavailable"}
				return nil
			}
			if s.safetyClient == nil {
				result = handlerResult{http.StatusServiceUnavailable, "safety kernel unavailable"}
				return nil
			}
			snapResp, err := s.safetyClient.ListSnapshots(ctx, &pb.ListSnapshotsRequest{})
			if err != nil {
				result = handlerResult{http.StatusBadGateway, "list safety snapshots failed"}
				return nil
			}
			currentSnapshot := ""
			if snapResp != nil && len(snapResp.Snapshots) > 0 {
				currentSnapshot = strings.TrimSpace(snapResp.Snapshots[0])
			}
			if currentSnapshot == "" || snapshotBase(currentSnapshot) != snapshotBase(policySnapshot) {
				s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), "policy snapshot changed")
				result = handlerResult{http.StatusConflict, "policy snapshot changed; re-evaluate before approving"}
				return nil
			}
		}
		hash, err := scheduler.HashJobRequest(req)
		if err != nil {
			result = handlerResult{http.StatusInternalServerError, "failed to hash job request"}
			return nil
		}
		if hash != safetyRecord.JobHash {
			s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), "job request changed")
			result = handlerResult{http.StatusConflict, "job request changed; approval rejected"}
			return nil
		}
		if req.Labels == nil {
			req.Labels = map[string]string{}
		}
		req.Labels["approval_granted"] = "true"
		reason := strings.TrimSpace(body.Reason)
		note := strings.TrimSpace(body.Note)
		if reason != "" {
			req.Labels["approval_reason"] = reason
		}
		if note != "" {
			req.Labels["approval_note"] = note
		}
		// Stable idempotency key per job so NATS dedup works on retries.
		req.Labels[bus.LabelBusMsgID] = "approval:" + jobID
		if err := s.jobStore.SetJobRequest(ctx, req); err != nil {
			if strings.Contains(err.Error(), "transaction failed") {
				result = handlerResult{http.StatusConflict, "concurrent approval conflict; retry"}
				return nil
			}
			result = handlerResult{http.StatusInternalServerError, "failed to persist approval request"}
			return nil
		}
		approvedBy := strings.TrimSpace(policyActorID(r))
		if approvedBy == "" {
			approvedBy = "system/unknown"
		}
		approvalRole := strings.TrimSpace(policyRole(r))
		if err := s.jobStore.SetApprovalRecord(ctx, jobID, store.ApprovalRecord{
			ApprovedBy:     approvedBy,
			ApprovedRole:   approvalRole,
			ApprovedAt:     time.Now().UnixMicro(),
			Reason:         reason,
			Note:           note,
			PolicySnapshot: policySnapshot,
			JobHash:        safetyRecord.JobHash,
		}); err != nil {
			result = handlerResult{http.StatusInternalServerError, "failed to persist approval record"}
			return nil
		}

		// Re-check workflow run terminal status under lock right before
		// the state transition to prevent approving dead workflow runs.
		if req.Labels != nil {
			if runID := strings.TrimSpace(req.Labels["run_id"]); runID != "" && s.workflowStore != nil {
				if run, runErr := s.workflowStore.GetRun(ctx, runID); runErr == nil && run != nil {
					if wf.IsTerminalRunStatus(run.Status) {
						msg := fmt.Sprintf("workflow run %s — approval no longer valid", run.Status)
						s.appendAuditEntryNamed(ctx, "approve_failed", "job", jobID, "", policyActorID(r), policyRole(r), msg)
						result = handlerResult{http.StatusConflict, msg}
						return nil
					}
				}
			}
		}

		if err := s.jobStore.SetState(ctx, jobID, model.JobStatePending); err != nil {
			if strings.Contains(err.Error(), "transaction failed") {
				result = handlerResult{http.StatusConflict, "concurrent approval conflict; retry"}
				return nil
			}
			result = handlerResult{http.StatusInternalServerError, "set job state failed"}
			return nil
		}
		traceID, traceErr := s.jobStore.GetTraceID(ctx, jobID)
		if traceErr != nil {
			slog.Warn("approve: trace ID lookup failed, using empty",
				"job_id", jobID, "error", traceErr)
		}
		packet := &pb.BusPacket{
			TraceId:         traceID,
			SenderId:        "api-gateway",
			CreatedAt:       timestamppb.Now(),
			ProtocolVersion: capsdk.DefaultProtocolVersion,
			Payload: &pb.BusPacket_JobRequest{
				JobRequest: req,
			},
		}
		if err := s.bus.Publish(capsdk.SubjectSubmit, packet); err != nil {
			result = handlerResult{http.StatusBadGateway, "publish approval failed"}
			return nil
		}
		slog.Info("job approved",
			"job_id", jobID, "trace_id", traceID,
			"topic", req.GetTopic(), "actor", policyActorID(r),
			"role", policyRole(r))
		s.appendAuditEntryNamed(ctx, "approve", "job", jobID, req.GetTopic(), policyActorID(r), policyRole(r), "approve job "+jobID)
		result = handlerResult{http.StatusOK, map[string]string{"job_id": jobID, "trace_id": traceID}}
		return nil
	})
	if lockErr != nil {
		if strings.Contains(lockErr.Error(), "lock busy") {
			writeErrorJSON(w, http.StatusConflict, "approval in progress; retry")
			return
		}
		writeInternalError(w, r, "approval lock", lockErr)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if result.status >= 400 {
		if msg, ok := result.body.(string); ok {
			writeErrorJSON(w, result.status, msg)
		} else {
			writeJSON(w, result.body)
		}
		return
	}
	writeJSON(w, result.body)
}

func (s *server) handleRejectJob(w http.ResponseWriter, r *http.Request) {
	if s.jobStore == nil || s.bus == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "job store or bus unavailable")
		return
	}
	if err := s.requireRole(r, "admin"); err != nil {
		writeForbidden(w, r, err)
		return
	}
	var body struct {
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if err := decodeJSONBody(w, r, &body); err != nil {
		if !errors.Is(err, io.EOF) {
			writeJSONDecodeError(w, err, "invalid body")
			return
		}
	}
	jobID := r.PathValue("job_id")
	if jobID == "" {
		writeErrorJSON(w, http.StatusBadRequest, "missing job_id")
		return
	}

	var result handlerResult
	lockErr := s.withApprovalLock(r.Context(), jobID, func(ctx context.Context) error {
		state, err := s.jobStore.GetState(ctx, jobID)
		if err != nil {
			result = handlerResult{http.StatusNotFound, "job not found"}
			return nil
		}
		if state != model.JobStateApproval {
			// Idempotency: if already denied, return success.
			if state == model.JobStateDenied {
				rec, recErr := s.jobStore.GetApprovalRecord(ctx, jobID)
				if recErr != nil {
					slog.Warn("reject: idempotent approval record lookup failed",
						"job_id", jobID, "error", recErr)
				}
				result = handlerResult{http.StatusOK, map[string]any{
					"job_id":      jobID,
					"status":      "already_rejected",
					"rejected_by": rec.ApprovedBy,
					"rejected_at": rec.ApprovedAt,
				}}
				return nil
			}
			s.appendAuditEntryNamed(ctx, "reject_failed", "job", jobID, "", policyActorID(r), policyRole(r), "job not awaiting approval (state="+string(state)+")")
			result = handlerResult{http.StatusConflict, "job not awaiting approval"}
			return nil
		}
		if tenant, tenantErr := s.jobStore.GetTenant(ctx, jobID); tenantErr != nil {
			slog.Warn("reject: tenant lookup failed", "job_id", jobID, "error", tenantErr)
		} else if tenant != "" {
			if err := s.requireTenantAccess(r, tenant); err != nil {
				result = handlerResult{http.StatusForbidden, "tenant access denied"}
				return nil
			}
		}
		safetyRecord, safetyErr := s.jobStore.GetSafetyDecision(ctx, jobID)
		if safetyErr != nil {
			slog.Warn("reject: safety decision unavailable, proceeding with empty record",
				"job_id", jobID, "error", safetyErr)
		}
		reason := strings.TrimSpace(body.Reason)
		note := strings.TrimSpace(body.Note)
		approvedBy := strings.TrimSpace(policyActorID(r))
		if approvedBy == "" {
			approvedBy = "system/unknown"
		}
		approvalRole := strings.TrimSpace(policyRole(r))
		if err := s.jobStore.SetApprovalRecord(ctx, jobID, store.ApprovalRecord{
			ApprovedBy:     approvedBy,
			ApprovedRole:   approvalRole,
			ApprovedAt:     time.Now().UnixMicro(),
			Reason:         reason,
			Note:           note,
			PolicySnapshot: safetyRecord.PolicySnapshot,
			JobHash:        safetyRecord.JobHash,
		}); err != nil {
			result = handlerResult{http.StatusInternalServerError, "failed to persist approval record"}
			return nil
		}
		if err := s.jobStore.SetState(ctx, jobID, model.JobStateDenied); err != nil {
			result = handlerResult{http.StatusInternalServerError, "set job state failed"}
			return nil
		}
		traceID, traceErr := s.jobStore.GetTraceID(ctx, jobID)
		if traceErr != nil {
			slog.Warn("reject: trace ID lookup failed, using empty",
				"job_id", jobID, "error", traceErr)
		}
		errorMessage := "approval rejected"
		if reason != "" {
			errorMessage = reason
		}
		packet := &pb.BusPacket{
			TraceId:         traceID,
			SenderId:        "api-gateway",
			CreatedAt:       timestamppb.Now(),
			ProtocolVersion: capsdk.DefaultProtocolVersion,
			Payload: &pb.BusPacket_JobResult{
				JobResult: &pb.JobResult{
					JobId:         jobID,
					Status:        pb.JobStatus_JOB_STATUS_DENIED,
					ErrorCode:     "approval_rejected",
					ErrorCodeEnum: pb.ErrorCode_ERROR_CODE_SAFETY_DENIED,
					ErrorMessage:  errorMessage,
				},
			},
		}
		if err := s.bus.Publish(capsdk.SubjectDLQ, packet); err != nil {
			slog.Error("publish dlq on approval reject failed", "job_id", jobID, "error", err)
		}
		rejectTopic, topicErr := s.jobStore.GetTopic(ctx, jobID)
		if topicErr != nil {
			slog.Warn("reject: topic lookup failed", "job_id", jobID, "error", topicErr)
		}
		if rejectTopic == capsdk.SubjectWorkflowApprovalGate {
			if err := s.bus.Publish(capsdk.SubjectResult, packet); err != nil {
				slog.Error("publish result on workflow gate reject failed", "job_id", jobID, "error", err)
			}
		}
		slog.Info("job rejected",
			"job_id", jobID, "topic", rejectTopic,
			"actor", policyActorID(r), "role", policyRole(r),
			"reason", reason)
		s.appendAuditEntryNamed(ctx, "reject", "job", jobID, rejectTopic, policyActorID(r), policyRole(r), "reject job "+jobID)
		result = handlerResult{http.StatusOK, map[string]string{"job_id": jobID}}
		return nil
	})
	if lockErr != nil {
		if strings.Contains(lockErr.Error(), "lock busy") {
			writeErrorJSON(w, http.StatusConflict, "rejection in progress; retry")
			return
		}
		writeInternalError(w, r, "rejection lock", lockErr)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if result.status >= 400 {
		if msg, ok := result.body.(string); ok {
			writeErrorJSON(w, result.status, msg)
		} else {
			writeJSON(w, result.body)
		}
		return
	}
	writeJSON(w, result.body)
}

// snapshotBase returns the base policy hash from a combined snapshot string.
// Combined snapshots have the form "base|cfg:hash"; this extracts just "base"
// so that config-overlay changes don't invalidate existing approvals.
func snapshotBase(snap string) string {
	if i := strings.Index(snap, "|"); i >= 0 {
		return snap[:i]
	}
	return snap
}

