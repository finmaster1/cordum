package llmchat

import (
	"context"
	"sync"
	"testing"
)

type fakeApprovalBus struct {
	mu       sync.Mutex
	handlers []func(context.Context, ApprovalEvent) error
}

func newFakeApprovalBus() *fakeApprovalBus { return &fakeApprovalBus{} }

func (b *fakeApprovalBus) SubscribeApprovalEvents(_ context.Context, handler func(context.Context, ApprovalEvent) error) (ApprovalSubscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
	return fakeApprovalSubscription{}, nil
}

func (b *fakeApprovalBus) Publish(ev ApprovalEvent) {
	b.mu.Lock()
	handlers := append([]func(context.Context, ApprovalEvent) error(nil), b.handlers...)
	b.mu.Unlock()
	for _, h := range handlers {
		_ = h(context.Background(), ev)
	}
}

type fakeApprovalSubscription struct{}

func (fakeApprovalSubscription) Unsubscribe() error { return nil }

func TestApprovalResumerWrongAgentDoesNotConsumePending(t *testing.T) {
	runner := &scriptedChatRunner{resumeFrames: [][]Frame{{{Type: FrameToolResult, ToolResult: `{"ok":true}`}, {Type: FrameFinal, Text: "done"}}}}
	resumer := NewApprovalResumer(ApprovalResumerConfig{Runner: runner})
	var (
		mu     sync.Mutex
		frames []Frame
	)
	resumer.Register(ApprovalPending{
		ApprovalID:  "appr-1",
		AgentID:     "agent-a",
		Session:     &Session{ID: "sess-a"},
		BearerToken: "bearer-a",
		Emit: func(frame Frame) bool {
			mu.Lock()
			defer mu.Unlock()
			frames = append(frames, frame)
			return true
		},
	})

	if err := resumer.handleEvent(context.Background(), ApprovalEvent{ApprovalID: "appr-1", AgentID: "agent-b", Status: ApprovalStatusResolved}); err != nil {
		t.Fatalf("wrong-agent handleEvent: %v", err)
	}
	_, resumes := runner.snapshot()
	if len(resumes) != 0 {
		t.Fatalf("resumes after wrong agent=%+v want none", resumes)
	}

	if err := resumer.handleEvent(context.Background(), ApprovalEvent{ApprovalID: "appr-1", AgentID: "agent-a", SessionID: "sess-a", Status: ApprovalStatusResolved}); err != nil {
		t.Fatalf("correct-agent handleEvent: %v", err)
	}
	_, resumes = runner.snapshot()
	if len(resumes) != 1 || resumes[0].BearerToken != "bearer-a" {
		t.Fatalf("resumes=%+v want one correct-agent resume", resumes)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(frames) != 2 || frames[len(frames)-1].Type != FrameFinal {
		t.Fatalf("frames=%+v want tool_result + final after correct event", frames)
	}
}

// QA reopen #2 regression: a publisher that omits agent_id MUST NOT
// consume a pending registration that has one. Empty-event-field is
// treated as a mismatch (fail-closed), not as a wildcard.
func TestApprovalResumerMissingEventIdentityIsRejected(t *testing.T) {
	runner := &scriptedChatRunner{resumeFrames: [][]Frame{{{Type: FrameFinal, Text: "should not run"}}}}

	t.Run("missing agent_id with pending agent_id", func(t *testing.T) {
		resumer := NewApprovalResumer(ApprovalResumerConfig{Runner: runner})
		resumer.Register(ApprovalPending{
			ApprovalID: "appr-missing-agent",
			AgentID:    "agent-a",
			Session:    &Session{ID: "sess-a"},
			Emit:       func(Frame) bool { return true },
		})
		if err := resumer.handleEvent(context.Background(), ApprovalEvent{
			ApprovalID: "appr-missing-agent",
			// AgentID intentionally empty — must NOT match a pending with agent_id="agent-a".
			Status: ApprovalStatusResolved,
		}); err != nil {
			t.Fatalf("handleEvent err: %v", err)
		}
		// The pending registration must still be present (event rejected, not consumed).
		resumer.mu.Lock()
		_, stillPending := resumer.pending["appr-missing-agent"]
		resumer.mu.Unlock()
		if !stillPending {
			t.Fatal("pending registration was consumed by an event with empty agent_id; expected fail-closed")
		}
	})

	t.Run("missing session_id with pending session", func(t *testing.T) {
		resumer := NewApprovalResumer(ApprovalResumerConfig{Runner: runner})
		resumer.Register(ApprovalPending{
			ApprovalID: "appr-missing-session",
			AgentID:    "agent-a",
			Session:    &Session{ID: "sess-a"},
			Emit:       func(Frame) bool { return true },
		})
		if err := resumer.handleEvent(context.Background(), ApprovalEvent{
			ApprovalID: "appr-missing-session",
			AgentID:    "agent-a",
			// SessionID intentionally empty — must NOT match a pending with session_id="sess-a".
			Status: ApprovalStatusResolved,
		}); err != nil {
			t.Fatalf("handleEvent err: %v", err)
		}
		resumer.mu.Lock()
		_, stillPending := resumer.pending["appr-missing-session"]
		resumer.mu.Unlock()
		if !stillPending {
			t.Fatal("pending registration was consumed by an event with empty session_id; expected fail-closed")
		}
	})
}
