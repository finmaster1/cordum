package llmchat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// ApprovalResumeInput is the phase-5 bridge from an asynchronous approval
// event back into the phase-4 agent loop. Approved events replay the pending
// tool call; rejected events inject a synthetic tool result so the LLM, not the
// transport layer, narrates the denial.
type ApprovalResumeInput struct {
	Session      *Session
	Approved     bool
	BearerToken  string
	DenialReason string
}

// approvalResumeRunner is implemented by *Agent and test fakes that can
// continue a paused turn after an approval event resolves.
type approvalResumeRunner interface {
	ResumeApproval(ctx context.Context, in ApprovalResumeInput) <-chan Frame
}

// ResumeApproval continues a Turn that previously paused on
// FrameApprovalRequired. It emits tool_result followed by summary-phase
// assistant_delta/final frames. This method intentionally does not emit a new
// user frame: the user's message was already persisted by Turn().
func (a *Agent) ResumeApproval(ctx context.Context, in ApprovalResumeInput) <-chan Frame {
	out := make(chan Frame, 8)
	go a.runApprovalResume(ctx, in, out)
	return out
}

func (a *Agent) runApprovalResume(ctx context.Context, in ApprovalResumeInput, out chan<- Frame) {
	defer close(out)
	if in.Session == nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "session is nil"})
		return
	}
	ref := in.Session.PendingToolCall
	if ref == nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeToolCallFailed, ErrorMsg: "pending tool call not found"})
		return
	}

	startedAt := time.Now()
	resultText := deniedByReviewerMessage
	isError := !in.Approved
	if in.Approved {
		if a.mcp == nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeToolCallFailed, ErrorMsg: "mcp client not configured"})
			return
		}
		a.metrics.IncToolCall(ref.Name)
		result, err := a.mcp.CallTool(ctx, ref.Name, json.RawMessage(ref.Arguments), in.BearerToken)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeToolCallFailed, ErrorMsg: err.Error()})
			return
		}
		raw, err := json.Marshal(result)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "marshal tool result: " + err.Error()})
			return
		}
		if a.redactor != nil {
			raw = a.redactor.RedactToolResult(raw)
		}
		resultText = string(raw)
	} else if in.DenialReason != "" {
		// Keep the externally-visible text pinned while still recording the
		// reviewer-provided reason inside the LLM context for narration.
		resultText = deniedByReviewerMessage
	}

	toolMsg := SessionMessage{
		Role: "tool",
		Text: resultText,
		ToolCalls: []ToolCallRef{{
			ID:        ref.ID,
			Name:      ref.Name,
			Arguments: ref.Arguments,
		}},
		At: time.Now().UTC(),
	}
	in.Session.Messages = append(in.Session.Messages, toolMsg)
	in.Session.PendingToolCall = nil
	if a.sessions != nil {
		if err := a.sessions.AppendMessage(ctx, in.Session.ID, toolMsg); err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "persist approval tool result: " + err.Error()})
			return
		}
		if err := a.sessions.SetPendingToolCall(ctx, in.Session.ID, nil); err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "clear pending tool call: " + err.Error()})
			return
		}
	}
	emitFrame(ctx, out, Frame{Type: FrameToolResult, ToolResult: resultText, IsError: isError})

	systemPrompt, tools, ok := a.resumePromptAndTools(ctx, out)
	if !ok {
		return
	}
	slog.Info("llmchat/agent: summary_phase_started",
		"session_id", in.Session.ID,
		"tool_calls", 1,
		"resume", true,
	)
	a.runSummaryOnly(ctx, in.Session, systemPrompt, tools, startedAt, out)
}

func (a *Agent) resumePromptAndTools(ctx context.Context, out chan<- Frame) (string, []Tool, bool) {
	systemPrompt := ""
	if a.promptLoader != nil {
		var err error
		systemPrompt, err = a.promptLoader.Load(ctx)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodePromptLoadFailed, ErrorMsg: err.Error()})
			return "", nil, false
		}
	}
	var tools []Tool
	if a.mcp != nil {
		toolList, err := a.mcp.ListTools(ctx)
		if err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeListToolsFailed, ErrorMsg: err.Error()})
			return "", nil, false
		}
		if toolList != nil {
			tools = make([]Tool, 0, len(toolList.Tools))
			for _, t := range toolList.Tools {
				params, err := json.Marshal(t.InputSchema)
				if err != nil {
					emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeListToolsFailed, ErrorMsg: "marshal tool schema: " + err.Error()})
					return "", nil, false
				}
				tools = append(tools, Tool{Name: t.Name, Description: t.Description, Parameters: params})
			}
		}
	}
	return systemPrompt, tools, true
}

func (a *Agent) runSummaryOnly(ctx context.Context, sess *Session, systemPrompt string, tools []Tool, startedAt time.Time, out chan<- Frame) {
	if a.provider == nil {
		emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "provider not configured"})
		return
	}
	stream, err, observeProvider := a.completeWithMetrics(ctx, buildCompleteRequest(systemPrompt, sess.Messages, tools), SamplingModeSummary)
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
		if chunk.Delta == "" {
			continue
		}
		bytesSeen += len(chunk.Delta)
		a.metrics.IncTokenBudgetUsed(float64(len(chunk.Delta)))
		if bytesSeen > a.budgets.MaxAssistantBytes {
			observeProvider()
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeAssistantBytesBudget, ErrorMsg: fmt.Sprintf("assistant output exceeded %d bytes", a.budgets.MaxAssistantBytes)})
			return
		}
		consolidated += chunk.Delta
		emitFrame(ctx, out, Frame{Type: FrameAssistantDelta, Text: chunk.Delta})
	}
	observeProvider()
	msg := SessionMessage{Role: "assistant", Text: consolidated, At: time.Now().UTC()}
	sess.Messages = append(sess.Messages, msg)
	if a.sessions != nil {
		if err := a.sessions.AppendMessage(ctx, sess.ID, msg); err != nil {
			emitFrame(ctx, out, Frame{Type: FrameError, ErrorCode: ErrorCodeProviderFailed, ErrorMsg: "persist assistant turn: " + err.Error()})
			return
		}
	}
	slog.Info("llmchat/agent: turn_end",
		"session_id", sess.ID,
		"resume", true,
		"bytes", bytesSeen,
		"duration_ms", time.Since(startedAt).Milliseconds(),
	)
	emitFrame(ctx, out, Frame{Type: FrameFinal, Text: consolidated})
}

var _ approvalResumeRunner = (*Agent)(nil)
