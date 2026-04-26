package llmchat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/cordum/cordum/core/mcp"
)

// FrameType discriminates the shape of an Agent.Turn output frame.
// Pinned wire format consumed by phase-5 WS handler. Renaming or
// reordering breaks downstream consumers.
type FrameType string

const (
	// FrameUser is emitted once at the start of a Turn carrying the
	// raw user message that was just appended to the session
	// transcript.
	FrameUser FrameType = "user"

	// FrameAssistantDelta is emitted for every Chunk.Delta the
	// provider streams during either tool-call or summary phase.
	// Multiple FrameAssistantDelta frames concatenate into the full
	// assistant turn.
	FrameAssistantDelta FrameType = "assistant_delta"

	// FrameToolCall is emitted when the LLM produces a tool call AND
	// the call has been dispatched to the MCP client. The frame
	// carries the tool name + arguments so the dashboard can render
	// "called X with Y" before the result lands.
	FrameToolCall FrameType = "tool_call"

	// FrameToolResult is emitted after MCP returns a tool result. The
	// content is the redacted result body that was fed back into the
	// LLM context.
	FrameToolResult FrameType = "tool_result"

	// FrameApprovalRequired is emitted when MCP returns -32099 for a
	// tool call. The Turn pauses (returns) without a Final or Error;
	// phase-5 WS handler resumes via Resume() once the human approves.
	FrameApprovalRequired FrameType = "approval_required"

	// FrameFinal is emitted exactly once on a successful Turn,
	// carrying the consolidated assistant text after the summary
	// phase completes.
	FrameFinal FrameType = "final"

	// FrameError is emitted in place of FrameFinal when the Turn
	// aborts (budget tripped, repeat call, ctx cancelled, transport
	// failure). Carries an ErrorCode for UI surfacing.
	FrameError FrameType = "error"
)

// Frame is one event on the Turn output channel. Exactly one of
// {Text, ToolCall, ToolResult, ApprovalID, ErrorCode} is meaningful
// per Type — phase-5 WS handler uses the Type as a discriminator.
type Frame struct {
	Type       FrameType        `json:"type"`
	SessionID  string           `json:"session_id,omitempty"`
	Text       string           `json:"text,omitempty"`
	ToolCall   *FrameToolDetail `json:"tool_call,omitempty"`
	ToolResult string           `json:"tool_result,omitempty"`
	IsError    bool             `json:"is_error,omitempty"`
	ApprovalID string           `json:"approval_id,omitempty"`
	ErrorCode  string           `json:"error_code,omitempty"`
	ErrorMsg   string           `json:"error_msg,omitempty"`
}

// FrameToolDetail carries the tool name + raw arguments emitted on a
// FrameToolCall. Kept separate from the bare ToolCall type because the
// Frame's view is "what got dispatched"; ToolCall carries provider-side
// metadata (call_id, etc.) that the wire frame does not need.
type FrameToolDetail struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Error codes for FrameError. Pinned strings so phase-5 WS handler can
// branch on them.
const (
	ErrorCodeRepeatToolCall         = "repeat_tool_call"
	ErrorCodeWallClockBudgetTripped = "wall_clock_budget_tripped"
	ErrorCodeAssistantBytesBudget   = "assistant_bytes_budget_tripped"
	ErrorCodeToolCallsBudgetTripped = "tool_calls_budget_tripped"
	ErrorCodeToolCallFailed         = "tool_call_failed"
	ErrorCodeContextCancelled       = "context_cancelled"
	ErrorCodeProviderFailed         = "provider_failed"
	ErrorCodeListToolsFailed        = "list_tools_failed"
	ErrorCodePromptLoadFailed       = "prompt_load_failed"
)

// Budget defaults pin the production guardrails. All env-overridable
// per task rail #3 (never disable in code).
const (
	defaultMaxToolCallsPerTurn = 12
	defaultMaxWallClock        = 60 * time.Second
	defaultMaxAssistantBytes   = 32 * 1024
)

// Env vars for budget overrides (pinned names; documented in the
// service runbook).
const (
	envMaxToolCalls = "LLMCHAT_MAX_TOOL_CALLS_PER_TURN"
	envMaxWallClock = "LLMCHAT_MAX_WALL_CLOCK_PER_TURN"
	envMaxAssistant = "LLMCHAT_MAX_ASSISTANT_BYTES"
)

// agentBudgets carries the resolved per-turn limits.
type agentBudgets struct {
	MaxToolCalls      int
	MaxWallClock      time.Duration
	MaxAssistantBytes int
}

// loadBudgetsFromEnv reads the three env vars; invalid values fall
// back to defaults with a slog.Warn so the operator notices but the
// service stays operational (rail #3 — never disable budgets).
func loadBudgetsFromEnv() agentBudgets {
	b := agentBudgets{
		MaxToolCalls:      defaultMaxToolCallsPerTurn,
		MaxWallClock:      defaultMaxWallClock,
		MaxAssistantBytes: defaultMaxAssistantBytes,
	}
	if raw := os.Getenv(envMaxToolCalls); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			b.MaxToolCalls = v
		} else {
			slog.Warn("llmchat/agent: invalid LLMCHAT_MAX_TOOL_CALLS_PER_TURN; using default", "raw", raw, "default", defaultMaxToolCallsPerTurn)
		}
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
// Defined as an interface so tests can inject an in-memory fake
// without standing up Redis.
type SessionStorer interface {
	AppendMessage(ctx context.Context, id string, msg SessionMessage) error
	SetPendingToolCall(ctx context.Context, id string, ref *ToolCallRef) error
}

// MCPCaller is the slice of *MCPClient the Agent depends on. Same
// rationale as SessionStorer — keeps the test seams thin.
type MCPCaller interface {
	ListTools(ctx context.Context) (*mcp.ToolListResult, error)
	CallTool(ctx context.Context, name string, arguments json.RawMessage, bearerToken string) (*mcp.ToolCallResult, error)
}

// Agent runs the conversation state machine for a single Turn at a
// time. Reuse across Turns is safe; it holds no per-turn state.
type Agent struct {
	provider     Provider
	mcp          MCPCaller
	redactor     Redactor
	promptLoader PromptLoader
	sessions     SessionStorer
	budgets      agentBudgets
	metrics      *Metrics
}

// AgentConfig wires the Agent's collaborators. NewAgent applies
// defaults for the budget knobs.
type AgentConfig struct {
	Provider     Provider
	MCP          MCPCaller
	Redactor     Redactor
	PromptLoader PromptLoader
	Sessions     SessionStorer
	Metrics      *Metrics
	// Budgets, when zero-valued, are filled from env (or the
	// hard-coded defaults if env is unset).
	Budgets *agentBudgets
}

// NewAgent constructs an Agent with the supplied collaborators and
// budget configuration.
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
		mcp:          cfg.MCP,
		redactor:     cfg.Redactor,
		promptLoader: cfg.PromptLoader,
		sessions:     cfg.Sessions,
		budgets:      budgets,
		metrics:      cfg.Metrics,
	}
}

// TurnInput carries the Turn arguments. BearerToken is the per-session
// delegation JWT minted by phase-3's DelegationClient; CallTool
// forwards it to the gateway so the action runs under the user's
// scoped identity.
type TurnInput struct {
	Session     *Session
	UserMessage string
	BearerToken string
}

// Turn runs the chat agent loop and streams Frames on the returned
// channel. The channel is closed when the Turn terminates (success,
// error, or approval-required pause). The body runs in exactly one
// goroutine; the caller may range over the channel until close.
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
	slog.Info("llmchat/agent: turn_start",
		"session_id", in.Session.ID,
		"principal", in.Session.UserPrincipal,
		"tenant", in.Session.Tenant,
	)

	// Append user msg to session + transcript view, emit FrameUser.
	in.Session.Messages = append(in.Session.Messages, SessionMessage{Role: "user", Text: in.UserMessage, At: time.Now().UTC()})
	if a.sessions != nil {
		if err := a.sessions.AppendMessage(ctx, in.Session.ID, SessionMessage{Role: "user", Text: in.UserMessage, At: time.Now().UTC()}); err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "append user msg: " + err.Error()})
			return
		}
	}
	emitFrame(ctx, out, Frame{Type: FrameUser, Text: in.UserMessage})

	// Load system prompt once per turn.
	systemPrompt := ""
	if a.promptLoader != nil {
		var err error
		systemPrompt, err = a.promptLoader.Load(ctx)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodePromptLoadFailed, ErrorMsg: err.Error()})
			return
		}
	}

	// Fetch tool catalog (TTL-cached at the mcpclient layer).
	var tools []Tool
	if a.mcp != nil {
		toolList, err := a.mcp.ListTools(ctx)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeListToolsFailed, ErrorMsg: err.Error()})
			return
		}
		if toolList != nil {
			tools = make([]Tool, 0, len(toolList.Tools))
			for _, t := range toolList.Tools {
				params, err := json.Marshal(t.InputSchema)
				if err != nil {
					emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeListToolsFailed, ErrorMsg: "marshal tool schema: " + err.Error()})
					return
				}
				tools = append(tools, Tool{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  params,
				})
			}
		}
	}

	// Per-turn state.
	tcCount := 0
	bytesSeen := 0
	repeatHashes := map[string]struct{}{}
	finishReason := ""

	// Tool-call phase: run provider in a loop, dispatching tool calls
	// and feeding results back, until the provider stops requesting
	// tools (FinishReason != "tool_calls").
toolLoop:
	for {
		if err := ctx.Err(); err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeContextCancelled, ErrorMsg: err.Error()})
			return
		}
		if elapsed := time.Since(startedAt); elapsed > a.budgets.MaxWallClock {
			slog.Warn("llmchat/agent: budget_tripped", "kind", "wall_clock", "elapsed", elapsed)
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeWallClockBudgetTripped, ErrorMsg: fmt.Sprintf("turn exceeded %s", a.budgets.MaxWallClock)})
			return
		}

		req := buildCompleteRequest(systemPrompt, in.Session.Messages, tools)
		stream, err, observeProvider := a.completeWithMetrics(ctx, req, SamplingModeToolCalls)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: err.Error()})
			return
		}

		var toolCalls []ToolCall
		finishReason = ""
		for chunk := range stream {
			if chunk.Err != nil {
				observeProvider()
				emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: chunk.Err.Error()})
				return
			}
			if elapsed := time.Since(startedAt); elapsed > a.budgets.MaxWallClock {
				observeProvider()
				slog.Warn("llmchat/agent: budget_tripped", "kind", "wall_clock", "elapsed", elapsed)
				emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeWallClockBudgetTripped, ErrorMsg: fmt.Sprintf("turn exceeded %s", a.budgets.MaxWallClock)})
				return
			}
			if chunk.Delta != "" {
				bytesSeen += len(chunk.Delta)
				a.metrics.IncTokenBudgetUsed(float64(len(chunk.Delta)))
				if bytesSeen > a.budgets.MaxAssistantBytes {
					observeProvider()
					slog.Warn("llmchat/agent: budget_tripped", "kind", "assistant_bytes", "seen", bytesSeen)
					emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeAssistantBytesBudget, ErrorMsg: fmt.Sprintf("assistant output exceeded %d bytes", a.budgets.MaxAssistantBytes)})
					return
				}
				// The first provider pass is for deterministic tool selection only.
				// Some OpenAI-compatible backends still emit direct-answer text in
				// this mode when no tool call is needed. Keep that text internal so
				// the user sees exactly one answer from the summary phase below.
			}
			if len(chunk.ToolCalls) > 0 {
				toolCalls = append(toolCalls, chunk.ToolCalls...)
			}
			if chunk.FinishReason != "" {
				finishReason = chunk.FinishReason
			}
		}
		observeProvider()

		// Dispatch any tool calls accumulated this iteration.
		if len(toolCalls) == 0 {
			break toolLoop
		}
		for _, tc := range toolCalls {
			tcCount++
			if tcCount > a.budgets.MaxToolCalls {
				slog.Warn("llmchat/agent: budget_tripped", "kind", "tool_calls", "count", tcCount)
				emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeToolCallsBudgetTripped, ErrorMsg: fmt.Sprintf("turn exceeded %d tool calls", a.budgets.MaxToolCalls)})
				return
			}

			hashKey := repeatCallHash(tc.Name, tc.Arguments)
			if _, dup := repeatHashes[hashKey]; dup {
				slog.Warn("llmchat/agent: repeat_call_aborted", "tool", tc.Name)
				emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeRepeatToolCall, ErrorMsg: fmt.Sprintf("tool %s called twice with identical arguments", tc.Name)})
				return
			}
			repeatHashes[hashKey] = struct{}{}

			slog.Info("llmchat/agent: tool_call_dispatched",
				"session_id", in.Session.ID,
				"tool", tc.Name,
			)
			a.metrics.IncToolCall(tc.Name)
			result, err := a.mcp.CallTool(ctx, tc.Name, tc.Arguments, in.BearerToken)
			if err != nil {
				var ae *ApprovalRequiredError
				if errors.As(err, &ae) {
					slog.Info("llmchat/agent: approval_required",
						"session_id", in.Session.ID,
						"tool", tc.Name,
						"approval_id", ae.ApprovalID,
					)
					ref := &ToolCallRef{ID: tc.ID, Name: tc.Name, Arguments: string(tc.Arguments)}
					if a.sessions != nil {
						if saveErr := a.sessions.SetPendingToolCall(ctx, in.Session.ID, ref); saveErr != nil {
							emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "persist pending tool call: " + saveErr.Error()})
							return
						}
					}
					in.Session.PendingToolCall = ref
					emitFrame(ctx, out, Frame{Type: FrameApprovalRequired, ApprovalID: ae.ApprovalID})
					return
				}
				slog.Warn("llmchat/agent: tool_call_failed", "tool", tc.Name, "error", err)
				emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeToolCallFailed, ErrorMsg: err.Error()})
				return
			}

			// Serialize + redact the tool result, then feed it back
			// into the LLM context as a tool-role message.
			resultBytes, err := json.Marshal(result)
			if err != nil {
				emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "marshal tool result: " + err.Error()})
				return
			}
			redacted := resultBytes
			if a.redactor != nil {
				redacted = a.redactor.RedactToolResult(resultBytes)
			}
			toolMsg := Message{
				Role:       "tool",
				Content:    string(redacted),
				ToolCallID: tc.ID,
				Name:       tc.Name,
			}
			in.Session.Messages = append(in.Session.Messages, SessionMessage{
				Role: "tool",
				Text: string(redacted),
				ToolCalls: []ToolCallRef{{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: string(tc.Arguments),
				}},
				At: time.Now().UTC(),
			})
			// Append into the per-turn provider history (used for
			// the next iteration's CompleteRequest).
			req.Messages = append(req.Messages, toolMsg)
			if a.sessions != nil {
				if saveErr := a.sessions.AppendMessage(ctx, in.Session.ID, in.Session.Messages[len(in.Session.Messages)-1]); saveErr != nil {
					emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "persist tool result: " + saveErr.Error()})
					return
				}
			}

			emitFrame(ctx, out, Frame{Type: FrameToolCall, ToolCall: &FrameToolDetail{Name: tc.Name, Arguments: tc.Arguments}})
			emitFrame(ctx, out, Frame{Type: FrameToolResult, ToolResult: string(redacted)})
		}

		// Some backends emit FinishReason="stop" alongside tool_calls
		// in the same stream — we treat any tool_calls payload as
		// "keep iterating" regardless of the finish reason on this
		// chunk; the next iteration's stream will close cleanly with
		// no tool_calls when the LLM is done.
		if finishReason == "stop" && len(toolCalls) == 0 {
			break toolLoop
		}
	}

	// Summary phase: fires ONCE per turn (rail #6). Re-build the
	// request from the now-extended message history (tools results
	// already appended above) and run the provider with the
	// summary-mode sampling knobs.
	if err := ctx.Err(); err != nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeContextCancelled, ErrorMsg: err.Error()})
		return
	}
	slog.Info("llmchat/agent: summary_phase_started",
		"session_id", in.Session.ID,
		"tool_calls", tcCount,
	)

	summaryReq := buildCompleteRequest(systemPrompt, in.Session.Messages, tools)
	summaryStream, err, observeProvider := a.completeWithMetrics(ctx, summaryReq, SamplingModeSummary)
	if err != nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: err.Error()})
		return
	}

	var consolidated string
	for chunk := range summaryStream {
		if chunk.Err != nil {
			observeProvider()
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: chunk.Err.Error()})
			return
		}
		if chunk.Delta != "" {
			bytesSeen += len(chunk.Delta)
			a.metrics.IncTokenBudgetUsed(float64(len(chunk.Delta)))
			if bytesSeen > a.budgets.MaxAssistantBytes {
				observeProvider()
				slog.Warn("llmchat/agent: budget_tripped", "kind", "assistant_bytes", "seen", bytesSeen)
				emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeAssistantBytesBudget, ErrorMsg: fmt.Sprintf("assistant output exceeded %d bytes", a.budgets.MaxAssistantBytes)})
				return
			}
			consolidated += chunk.Delta
			emitFrame(ctx, out, Frame{Type: FrameAssistantDelta, Text: chunk.Delta})
		}
	}
	observeProvider()

	in.Session.Messages = append(in.Session.Messages, SessionMessage{Role: "assistant", Text: consolidated, At: time.Now().UTC()})
	if a.sessions != nil {
		if saveErr := a.sessions.AppendMessage(ctx, in.Session.ID, in.Session.Messages[len(in.Session.Messages)-1]); saveErr != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "persist assistant turn: " + saveErr.Error()})
			return
		}
	}

	slog.Info("llmchat/agent: turn_end",
		"session_id", in.Session.ID,
		"tool_calls", tcCount,
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

// buildCompleteRequest assembles the provider request envelope from
// the session transcript + tool catalog + system prompt. The system
// prompt is prepended as a system-role Message because the actual
// CompleteRequest schema does not have a separate System field.
func buildCompleteRequest(systemPrompt string, sessionMessages []SessionMessage, tools []Tool) CompleteRequest {
	msgs := make([]Message, 0, len(sessionMessages)+1)
	if systemPrompt != "" {
		msgs = append(msgs, Message{Role: "system", Content: systemPrompt})
	}
	for _, sm := range sessionMessages {
		m := Message{Role: sm.Role, Content: sm.Text}
		if len(sm.ToolCalls) > 0 {
			m.ToolCalls = make([]ToolCall, 0, len(sm.ToolCalls))
			for _, tc := range sm.ToolCalls {
				m.ToolCalls = append(m.ToolCalls, ToolCall{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: json.RawMessage(tc.Arguments),
				})
			}
		}
		msgs = append(msgs, m)
	}
	return CompleteRequest{Messages: msgs, Tools: tools}
}

// repeatCallHash hashes (tool_name + canonicalised JSON args) so the
// repeat-call detector can compare two calls for identity. The
// canonicalisation step (key-sorted JSON re-marshal) ensures
// `{"a":1,"b":2}` and `{"b":2,"a":1}` produce the same hash.
func repeatCallHash(name string, args json.RawMessage) string {
	canon := canonicaliseJSON(args)
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write(canon)
	return hex.EncodeToString(h.Sum(nil))
}

// canonicaliseJSON returns a stable JSON representation of args. If
// the input is not valid JSON, the raw bytes are returned (so
// non-JSON args still hash deterministically).
func canonicaliseJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	canonical := canonicaliseValue(v)
	out, err := json.Marshal(canonical)
	if err != nil {
		return raw
	}
	return out
}

// canonicaliseValue walks a decoded JSON tree and returns a value
// whose maps have keys ordered. Slices are kept in input order
// because order is meaningful (a list of jobs differs from the
// reverse list).
func canonicaliseValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := make([][2]any, 0, len(keys))
		for _, k := range keys {
			ordered = append(ordered, [2]any{k, canonicaliseValue(t[k])})
		}
		return orderedMap(ordered)
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = canonicaliseValue(x)
		}
		return out
	default:
		return v
	}
}

// orderedMap is a map representation that marshals to a deterministic
// key order. We can't use a stdlib type because Go's json package
// re-orders map keys alphabetically when marshaling map types — but
// that's exactly what we want, so we wrap as a stable type that
// MarshalJSON does the same thing explicitly.
type orderedMap [][2]any

// MarshalJSON emits the entries in slice order.
func (m orderedMap) MarshalJSON() ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	out := make([]byte, 0, 64)
	out = append(out, '{')
	for i, kv := range m {
		if i > 0 {
			out = append(out, ',')
		}
		k, _ := kv[0].(string)
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		out = append(out, kb...)
		out = append(out, ':')
		vb, err := json.Marshal(kv[1])
		if err != nil {
			return nil, err
		}
		out = append(out, vb...)
	}
	out = append(out, '}')
	return out, nil
}

// emitFrame sends a frame, honouring ctx cancellation so a slow
// reader cannot deadlock the run goroutine.
func emitFrame(ctx context.Context, out chan<- Frame, f Frame) {
	select {
	case <-ctx.Done():
	case out <- f:
	}
}
