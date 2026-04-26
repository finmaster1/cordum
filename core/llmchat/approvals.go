package llmchat

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
)

const (
	ApprovalSubjectWildcard = "sys.approvals.>"
	ApprovalStatusResolved  = "resolved"
	ApprovalStatusRejected  = "rejected"

	deniedByReviewerMessage = "denied by human reviewer"
)

// ApprovalEvent is the transport-neutral event shape consumed by llm-chat.
// The NATS adapter decodes this from SystemAlert.Details; tests publish it
// directly via a fake bus.
type ApprovalEvent struct {
	ApprovalID string `json:"approval_id"`
	SessionID  string `json:"session_id,omitempty"`
	AgentID    string `json:"agent_id,omitempty"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

type ApprovalSubscription interface {
	Unsubscribe() error
}

type ApprovalEventBus interface {
	SubscribeApprovalEvents(ctx context.Context, handler func(context.Context, ApprovalEvent) error) (ApprovalSubscription, error)
}

type ApprovalResumerConfig struct {
	Bus    ApprovalEventBus
	Runner approvalResumeRunner
}

// ApprovalPending binds one approval_id to an active WS connection.
type ApprovalPending struct {
	ApprovalID  string
	AgentID     string
	Session     *Session
	BearerToken string
	Emit        func(Frame) bool
	Runner      approvalResumeRunner
}

// ApprovalResumer fans approval resolution events into the registered session
// stream and drops duplicate resolution events after first delivery.
type ApprovalResumer struct {
	runner approvalResumeRunner

	mu      sync.Mutex
	pending map[string]ApprovalPending
	done    map[string]struct{}
	sub     ApprovalSubscription
}

func NewApprovalResumer(cfg ApprovalResumerConfig) *ApprovalResumer {
	r := &ApprovalResumer{runner: cfg.Runner, pending: map[string]ApprovalPending{}, done: map[string]struct{}{}}
	if cfg.Bus != nil {
		sub, err := cfg.Bus.SubscribeApprovalEvents(context.Background(), r.handleEvent)
		if err != nil {
			slog.Warn("llmchat: approval event subscribe failed", "subject", ApprovalSubjectWildcard, "error", err)
		} else {
			r.sub = sub
		}
	}
	return r
}

func (r *ApprovalResumer) Close() error {
	if r == nil || r.sub == nil {
		return nil
	}
	return r.sub.Unsubscribe()
}

func (r *ApprovalResumer) Register(p ApprovalPending) {
	if r == nil || strings.TrimSpace(p.ApprovalID) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, alreadyDone := r.done[p.ApprovalID]; alreadyDone {
		return
	}
	r.pending[p.ApprovalID] = p
}

func (r *ApprovalResumer) handleEvent(ctx context.Context, ev ApprovalEvent) error {
	approvalID := strings.TrimSpace(ev.ApprovalID)
	if approvalID == "" {
		return nil
	}
	r.mu.Lock()
	if _, alreadyDone := r.done[approvalID]; alreadyDone {
		r.mu.Unlock()
		return nil
	}
	pending, ok := r.pending[approvalID]
	if !ok {
		r.mu.Unlock()
		return nil
	}
	// Ownership check is fail-CLOSED: when the pending registration carries
	// agent_id / session_id, the inbound event MUST also carry the matching
	// value. Empty / missing on the event is treated as a mismatch — a
	// spoofed publisher can't drop the field to bypass the check.
	if pending.AgentID != "" && ev.AgentID != pending.AgentID {
		r.mu.Unlock()
		slog.Warn("llmchat/approvals: dropping event with non-matching agent_id",
			"approval_id", approvalID,
			"pending_agent_id", pending.AgentID,
			"event_agent_id_present", ev.AgentID != "")
		return nil
	}
	if pending.Session != nil && pending.Session.ID != "" && ev.SessionID != pending.Session.ID {
		r.mu.Unlock()
		slog.Warn("llmchat/approvals: dropping event with non-matching session_id",
			"approval_id", approvalID,
			"pending_session_id", pending.Session.ID,
			"event_session_id_present", ev.SessionID != "")
		return nil
	}
	delete(r.pending, approvalID)
	r.done[approvalID] = struct{}{}
	r.mu.Unlock()
	runner := pending.Runner
	if runner == nil {
		runner = r.runner
	}
	if runner == nil || pending.Emit == nil || pending.Session == nil {
		return nil
	}
	approved := strings.EqualFold(ev.Status, ApprovalStatusResolved) || strings.EqualFold(ev.Status, "approved")
	resume := ApprovalResumeInput{Session: pending.Session, Approved: approved, BearerToken: pending.BearerToken, DenialReason: ev.Reason}
	if strings.EqualFold(ev.Status, ApprovalStatusRejected) || strings.EqualFold(ev.Status, "rejected") {
		resume.Approved = false
	}
	for frame := range runner.ResumeApproval(ctx, resume) {
		if frame.SessionID == "" {
			frame.SessionID = pending.Session.ID
		}
		if !pending.Emit(frame) {
			break
		}
	}
	return nil
}

// ParseApprovalEventJSON is a small helper for bus adapters and tests.
func ParseApprovalEventJSON(raw []byte) (ApprovalEvent, error) {
	var ev ApprovalEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return ApprovalEvent{}, err
	}
	return ev, nil
}
