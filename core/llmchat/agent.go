package llmchat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// FrameType discriminates the shape of an Agent.Turn output frame.
// Pinned wire format consumed by HTTP, SSE, and websocket chat handlers.
type FrameType string

const (
	// FrameUser is emitted once at the start of a Turn carrying the
	// raw user message that was just appended to the session transcript.
	FrameUser FrameType = "user"

	// FrameAssistantDelta is emitted for every provider text delta.
	// Multiple FrameAssistantDelta frames concatenate into the full
	// assistant turn.
	FrameAssistantDelta FrameType = "assistant_delta"

	// FrameFinal is emitted exactly once on a successful Turn,
	// carrying the consolidated assistant text.
	FrameFinal FrameType = "final"

	// FrameError is emitted in place of FrameFinal when the Turn aborts
	// (ctx cancelled, provider failure, or bounded-output guard tripped).
	FrameError FrameType = "error"
)

// Frame is one event on the Turn output channel. Exactly one of Text or
// ErrorCode/ErrorMsg is meaningful per Type.
type Frame struct {
	Type      FrameType `json:"type"`
	SessionID string    `json:"session_id,omitempty"`
	Text      string    `json:"text,omitempty"`
	IsError   bool      `json:"is_error,omitempty"`
	ErrorCode string    `json:"error_code,omitempty"`
	ErrorMsg  string    `json:"error_msg,omitempty"`
}

// Error codes for FrameError. Pinned strings so chat clients can branch
// without parsing prose.
const (
	ErrorCodeWallClockBudgetTripped = "wall_clock_budget_tripped"
	ErrorCodeAssistantBytesBudget   = "assistant_bytes_budget_tripped"
	ErrorCodeContextCancelled       = "context_cancelled"
	ErrorCodeProviderFailed         = "provider_failed"
	ErrorCodePromptLoadFailed       = "prompt_load_failed"
)

// Budget defaults pin production guardrails. Output and wall-clock limits
// remain relevant in informational-only mode even though tool budgets do not.
const (
	defaultMaxWallClock      = 60 * time.Second
	defaultMaxAssistantBytes = 32 * 1024
)

// Env vars for budget overrides (pinned names; documented in the service
// runbook). The old per-turn action-count knob was retired with the
// informational-only flow.
const (
	envMaxWallClock = "LLMCHAT_MAX_WALL_CLOCK_PER_TURN"
	envMaxAssistant = "LLMCHAT_MAX_ASSISTANT_BYTES"
)

// agentBudgets carries the resolved per-turn limits.
type agentBudgets struct {
	MaxWallClock      time.Duration
	MaxAssistantBytes int
}

// loadBudgetsFromEnv reads the output guards; invalid values fall back to
// defaults with a slog.Warn so the operator notices but the service stays up.
func loadBudgetsFromEnv() agentBudgets {
	b := agentBudgets{
		MaxWallClock:      defaultMaxWallClock,
		MaxAssistantBytes: defaultMaxAssistantBytes,
	}
	if raw := os.Getenv(envMaxWallClock); raw != "" {
		if v, err := time.ParseDuration(raw); err == nil && v > 0 {
			b.MaxWallClock = v
		} else {
			slog.Warn("llmchat/agent: invalid LLMCHAT_MAX_WALL_CLOCK_PER_TURN; using default", "raw", raw, "default", defaultMaxWallClock)
		}
	}
	if raw := os.Getenv(envMaxAssistant); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			b.MaxAssistantBytes = v
		} else {
			slog.Warn("llmchat/agent: invalid LLMCHAT_MAX_ASSISTANT_BYTES; using default", "raw", raw, "default", defaultMaxAssistantBytes)
		}
	}
	return b
}

// SessionStorer is the slice of *SessionStore the Agent depends on.
// Defined as an interface so tests can inject an in-memory fake.
type SessionStorer interface {
	AppendMessage(ctx context.Context, id string, msg SessionMessage) error
}

// Agent runs the informational-only conversation state machine for a single
// Turn at a time. Reuse across Turns is safe; it holds no per-turn state.
type Agent struct {
	provider     Provider
	promptLoader PromptLoader
	sessions     SessionStorer
	budgets      agentBudgets
	metrics      *Metrics
}

// AgentConfig wires the Agent's collaborators. NewAgent applies defaults for
// the budget knobs.
type AgentConfig struct {
	Provider     Provider
	PromptLoader PromptLoader
	Sessions     SessionStorer
	Metrics      *Metrics
	// Budgets, when zero-valued, are filled from env (or the hard-coded
	// defaults if env is unset).
	Budgets *agentBudgets
}

// NewAgent constructs an Agent with the supplied collaborators and budget
// configuration.
func NewAgent(cfg AgentConfig) *Agent {
	var budgets agentBudgets
	if cfg.Budgets != nil {
		budgets = *cfg.Budgets
	} else {
		budgets = loadBudgetsFromEnv()
	}
	if cfg.Metrics == nil {
		cfg.Metrics = NewNopMetrics()
	}
	return &Agent{
		provider:     cfg.Provider,
		promptLoader: cfg.PromptLoader,
		sessions:     cfg.Sessions,
		budgets:      budgets,
		metrics:      cfg.Metrics,
	}
}

// TurnInput carries one user message and the session transcript it belongs to.
type TurnInput struct {
	Session     *Session
	UserMessage string
}

// Turn runs the informational chat turn and streams Frames on the returned
// channel. The channel is closed when the Turn terminates.
func (a *Agent) Turn(ctx context.Context, in TurnInput) <-chan Frame {
	out := make(chan Frame, 8)
	go a.runTurn(ctx, in, out)
	return out
}

func (a *Agent) runTurn(ctx context.Context, in TurnInput, out chan<- Frame) {
	defer close(out)

	if in.Session == nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "session is nil"})
		return
	}

	startedAt := time.Now()
	logger := sessionLogger(ctx, in.Session)
	logger.Info("llmchat/agent: turn_start")

	userMsg := SessionMessage{Role: "user", Text: in.UserMessage, At: time.Now().UTC()}
	in.Session.Messages = append(in.Session.Messages, userMsg)
	if a.sessions != nil {
		if err := a.sessions.AppendMessage(ctx, in.Session.ID, userMsg); err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "append user msg: " + err.Error()})
			return
		}
	}
	emitFrame(ctx, out, Frame{Type: FrameUser, Text: in.UserMessage})

	systemPrompt := ""
	if a.promptLoader != nil {
		var err error
		systemPrompt, err = a.promptLoader.Load(ctx)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodePromptLoadFailed, ErrorMsg: err.Error()})
			return
		}
	}

	if err := ctx.Err(); err != nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeContextCancelled, ErrorMsg: err.Error()})
		return
	}

	stream, err, observeProvider := a.completeWithMetrics(ctx, buildCompleteRequest(systemPrompt, in.Session.Messages), SamplingModeResponse)
	if err != nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: err.Error()})
		return
	}

	var consolidated string
	bytesSeen := 0
	for chunk := range stream {
		if chunk.Err != nil {
			observeProvider()
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: chunk.Err.Error()})
			return
		}
		if elapsed := time.Since(startedAt); elapsed > a.budgets.MaxWallClock {
			observeProvider()
			logger.Warn("llmchat/agent: budget_tripped", "kind", "wall_clock", "elapsed", elapsed)
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeWallClockBudgetTripped, ErrorMsg: fmt.Sprintf("turn exceeded %s", a.budgets.MaxWallClock)})
			return
		}
		if chunk.Delta == "" {
			continue
		}
		bytesSeen += len(chunk.Delta)
		a.metrics.IncTokenBudgetUsed(float64(len(chunk.Delta)))
		if bytesSeen > a.budgets.MaxAssistantBytes {
			observeProvider()
			logger.Warn("llmchat/agent: budget_tripped", "kind", "assistant_bytes", "seen", bytesSeen)
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeAssistantBytesBudget, ErrorMsg: fmt.Sprintf("assistant output exceeded %d bytes", a.budgets.MaxAssistantBytes)})
			return
		}
		consolidated += chunk.Delta
		emitFrame(ctx, out, Frame{Type: FrameAssistantDelta, Text: chunk.Delta})
	}
	observeProvider()

	assistantMsg := SessionMessage{Role: "assistant", Text: consolidated, At: time.Now().UTC()}
	in.Session.Messages = append(in.Session.Messages, assistantMsg)
	if a.sessions != nil {
		if saveErr := a.sessions.AppendMessage(ctx, in.Session.ID, assistantMsg); saveErr != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "persist assistant turn: " + saveErr.Error()})
			return
		}
	}

	logger.Info("llmchat/agent: turn_end",
		"bytes", bytesSeen,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	emitFrame(ctx, out, Frame{Type: FrameFinal, Text: consolidated})
}

func (a *Agent) completeWithMetrics(ctx context.Context, req CompleteRequest, mode SamplingMode) (<-chan Chunk, error, func()) {
	startedAt := time.Now()
	observed := false
	observe := func() {
		if observed {
			return
		}
		observed = true
		if a != nil && a.metrics != nil {
			a.metrics.ObserveVLLMLatency(time.Since(startedAt))
		}
	}
	stream, err := a.provider.Complete(ctx, req, mode)
	if err != nil {
		observe()
		return nil, err, func() {}
	}
	return stream, nil, observe
}

// buildCompleteRequest assembles the provider request envelope from the
// session transcript + system prompt. The system prompt is prepended as a
// system-role Message because the provider schema does not have a separate
// System field.
func buildCompleteRequest(systemPrompt string, sessionMessages []SessionMessage) CompleteRequest {
	msgs := make([]Message, 0, len(sessionMessages)+1)
	if systemPrompt != "" {
		msgs = append(msgs, Message{Role: "system", Content: systemPrompt})
	}
	for _, sm := range sessionMessages {
		msgs = append(msgs, Message{Role: sm.Role, Content: sm.Text})
	}
	return CompleteRequest{Messages: msgs}
}

func emitFrame(ctx context.Context, out chan<- Frame, frame Frame) {
	if frame.Type == FrameError {
		frame.IsError = true
	}
	select {
	case <-ctx.Done():
	case out <- frame:
	}
}
