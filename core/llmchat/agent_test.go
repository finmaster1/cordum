package llmchat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// scriptedProvider is a Provider whose Complete returns a scripted stream of
// Chunks per call while recording requests and sampling modes. After scripts
// are exhausted it returns an empty terminal stream.
type scriptedProvider struct {
	mu       sync.Mutex
	scripts  [][]Chunk
	requests []CompleteRequest
	modes    []SamplingMode
	delay    time.Duration
	failOpen bool
}

func (p *scriptedProvider) Complete(ctx context.Context, req CompleteRequest, mode SamplingMode) (<-chan Chunk, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.modes = append(p.modes, mode)
	if len(p.requests) > len(p.scripts) {
		p.mu.Unlock()
		if p.failOpen {
			return nil, errors.New("scriptedProvider: no more scripts")
		}
		out := make(chan Chunk, 1)
		out <- Chunk{Done: true, FinishReason: "stop"}
		close(out)
		return out, nil
	}
	chunks := append([]Chunk(nil), p.scripts[len(p.requests)-1]...)
	delay := p.delay
	p.mu.Unlock()

	out := make(chan Chunk, len(chunks)+1)
	go func() {
		defer close(out)
		for _, c := range chunks {
			if delay > 0 {
				select {
				case <-ctx.Done():
					out <- Chunk{Done: true, Err: ctx.Err()}
					return
				case <-time.After(delay):
				}
			}
			select {
			case <-ctx.Done():
				out <- Chunk{Done: true, Err: ctx.Err()}
				return
			case out <- c:
			}
			if c.Done {
				return
			}
		}
		select {
		case <-ctx.Done():
			out <- Chunk{Done: true, Err: ctx.Err()}
		case out <- Chunk{Done: true, FinishReason: "stop"}:
		}
	}()
	return out, nil
}

func (p *scriptedProvider) HealthCheck(_ context.Context) error { return nil }

func (p *scriptedProvider) Modes() []SamplingMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]SamplingMode, len(p.modes))
	copy(out, p.modes)
	return out
}

func (p *scriptedProvider) Requests() []CompleteRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]CompleteRequest, len(p.requests))
	copy(out, p.requests)
	return out
}

// fakeSessions records AppendMessage calls for assertions.
type fakeSessions struct {
	mu       sync.Mutex
	appended []SessionMessage
	err      error
}

func (s *fakeSessions) AppendMessage(_ context.Context, _ string, msg SessionMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.appended = append(s.appended, msg)
	return nil
}

func (s *fakeSessions) Appended() []SessionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionMessage, len(s.appended))
	copy(out, s.appended)
	return out
}

type staticPromptLoader struct {
	text string
	err  error
}

func (s staticPromptLoader) Load(_ context.Context) (string, error) { return s.text, s.err }

func newTestAgent(provider Provider, sessions SessionStorer, budgetOverride *agentBudgets, prompt PromptLoader) *Agent {
	if prompt == nil {
		prompt = staticPromptLoader{text: "test-system"}
	}
	return NewAgent(AgentConfig{
		Provider:     provider,
		PromptLoader: prompt,
		Sessions:     sessions,
		Budgets:      budgetOverride,
	})
}

func collectFrames(t *testing.T, ch <-chan Frame) []Frame {
	t.Helper()
	var got []Frame
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, f)
		case <-deadline.C:
			t.Fatalf("collectFrames timed out, got=%v", got)
			return got
		}
	}
}

func TestTurn_InformationalSingleProviderCallStreamsFinal(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{scripts: [][]Chunk{{
		{Delta: "Hello, "},
		{Delta: "Cordum operator."},
		{Done: true, FinishReason: "stop"},
	}}}
	sessions := &fakeSessions{}
	a := newTestAgent(provider, sessions, nil, nil)

	frames := collectFrames(t, a.Turn(context.Background(), TurnInput{
		Session:     &Session{ID: "s1", UserPrincipal: "u", Tenant: "t"},
		UserMessage: "how do I configure approval gates?",
	}))

	if got := provider.Modes(); len(got) != 1 || got[0] != SamplingModeResponse {
		t.Fatalf("modes = %v, want one SamplingModeResponse", got)
	}
	wantTypes := []FrameType{FrameUser, FrameAssistantDelta, FrameAssistantDelta, FrameFinal}
	if got := frameTypes(frames); !sameFrameTypes(got, wantTypes) {
		t.Fatalf("frame types = %v, want %v (no retired tool/approval frames)", got, wantTypes)
	}
	if last := lastFrame(frames); last.Text != "Hello, Cordum operator." {
		t.Fatalf("final text = %q", last.Text)
	}

	reqs := provider.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	if len(reqs[0].Messages) != 2 || reqs[0].Messages[0].Role != "system" || reqs[0].Messages[0].Content != "test-system" || reqs[0].Messages[1].Role != "user" {
		t.Fatalf("request messages = %+v, want system + user", reqs[0].Messages)
	}

	appended := sessions.Appended()
	if len(appended) != 2 || appended[0].Role != "user" || appended[1].Role != "assistant" {
		t.Fatalf("appended transcript = %+v, want user + assistant", appended)
	}
}

func TestTurn_PromptLoadFailureDoesNotCallProvider(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{failOpen: true}
	a := newTestAgent(provider, &fakeSessions{}, nil, staticPromptLoader{err: errors.New("prompt unavailable")})

	frames := collectFrames(t, a.Turn(context.Background(), TurnInput{Session: &Session{ID: "s2"}, UserMessage: "hi"}))

	if got := len(provider.Modes()); got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
	terminal := lastFrame(frames)
	if terminal.Type != FrameError || terminal.ErrorCode != ErrorCodePromptLoadFailed || !terminal.IsError {
		t.Fatalf("terminal = %+v, want prompt_load_failed error", terminal)
	}
}

func TestTurn_ProviderStreamFailureReturnsStructuredError(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{scripts: [][]Chunk{{
		{Delta: "partial"},
		{Done: true, Err: errors.New("upstream unavailable")},
	}}}
	a := newTestAgent(provider, &fakeSessions{}, nil, nil)

	frames := collectFrames(t, a.Turn(context.Background(), TurnInput{Session: &Session{ID: "s3"}, UserMessage: "hi"}))

	terminal := lastFrame(frames)
	if terminal.Type != FrameError || terminal.ErrorCode != ErrorCodeProviderFailed || !strings.Contains(terminal.ErrorMsg, "upstream unavailable") {
		t.Fatalf("terminal = %+v, want provider_failed", terminal)
	}
}

func TestTurn_WallClockBudgetTrips(t *testing.T) {
	provider := &scriptedProvider{
		scripts: [][]Chunk{{{Delta: "slow"}, {Done: true, FinishReason: "stop"}}},
		delay:   50 * time.Millisecond,
	}
	budgets := &agentBudgets{MaxWallClock: 1 * time.Millisecond, MaxAssistantBytes: defaultMaxAssistantBytes}
	a := newTestAgent(provider, &fakeSessions{}, budgets, nil)

	frames := collectFrames(t, a.Turn(context.Background(), TurnInput{Session: &Session{ID: "s4"}, UserMessage: "slow"}))
	terminal := lastFrame(frames)
	if terminal.Type != FrameError || terminal.ErrorCode != ErrorCodeWallClockBudgetTripped {
		t.Fatalf("terminal = %+v, want wall_clock_budget_tripped", terminal)
	}
}

func TestTurn_AssistantBytesBudgetTrips(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{scripts: [][]Chunk{{{Delta: strings.Repeat("x", 200), Done: true, FinishReason: "stop"}}}}
	budgets := &agentBudgets{MaxWallClock: defaultMaxWallClock, MaxAssistantBytes: 64}
	a := newTestAgent(provider, &fakeSessions{}, budgets, nil)

	frames := collectFrames(t, a.Turn(context.Background(), TurnInput{Session: &Session{ID: "s5"}, UserMessage: "talk"}))
	terminal := lastFrame(frames)
	if terminal.Type != FrameError || terminal.ErrorCode != ErrorCodeAssistantBytesBudget {
		t.Fatalf("terminal = %+v, want assistant_bytes_budget_tripped", terminal)
	}
}

func TestTurn_AppendUserFailureStopsBeforeProvider(t *testing.T) {
	t.Parallel()
	provider := &scriptedProvider{failOpen: true}
	a := newTestAgent(provider, &fakeSessions{err: errors.New("redis down")}, nil, nil)

	frames := collectFrames(t, a.Turn(context.Background(), TurnInput{Session: &Session{ID: "s6"}, UserMessage: "hi"}))

	if got := len(provider.Modes()); got != 0 {
		t.Fatalf("provider calls = %d, want 0", got)
	}
	terminal := lastFrame(frames)
	if terminal.Type != FrameError || terminal.ErrorCode != ErrorCodeProviderFailed || !strings.Contains(terminal.ErrorMsg, "append user msg") {
		t.Fatalf("terminal = %+v, want append failure", terminal)
	}
}

func lastFrame(fr []Frame) Frame {
	if len(fr) == 0 {
		return Frame{}
	}
	return fr[len(fr)-1]
}

func frameTypes(fr []Frame) []FrameType {
	out := make([]FrameType, len(fr))
	for i, f := range fr {
		out[i] = f.Type
	}
	return out
}

func sameFrameTypes(got, want []FrameType) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
