import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { resetChatAssistantStore, useChatAssistantStore } from "./chatAssistant";

describe("useChatAssistantStore", () => {
  beforeEach(() => {
    resetChatAssistantStore();
  });
  afterEach(() => {
    resetChatAssistantStore();
  });

  it("starts closed with empty conversation", () => {
    const s = useChatAssistantStore.getState();
    expect(s.panelOpen).toBe(false);
    expect(s.sessionId).toBeNull();
    expect(s.messages).toEqual([]);
    expect(s.pendingApprovalIds).toEqual([]);
    expect(s.unreadCount).toBe(0);
  });

  it("togglePanel flips open and clears unread count when opening", () => {
    useChatAssistantStore.getState().applyFrame({
      type: "assistant_delta",
      id: "msg-1",
      delta: "hi",
      at: "2026-04-26T10:00:00Z",
    });
    expect(useChatAssistantStore.getState().unreadCount).toBe(1);
    useChatAssistantStore.getState().togglePanel();
    const after = useChatAssistantStore.getState();
    expect(after.panelOpen).toBe(true);
    expect(after.unreadCount).toBe(0);
  });

  it("appends a user frame as a user message", () => {
    useChatAssistantStore.getState().applyFrame({
      type: "user",
      id: "u-1",
      text: "hello",
      at: "2026-04-26T10:00:00Z",
    });
    const msgs = useChatAssistantStore.getState().messages;
    expect(msgs).toHaveLength(1);
    expect(msgs[0]).toMatchObject({ id: "u-1", role: "user", text: "hello" });
  });

  it("concatenates assistant_delta frames into the same assistant message", () => {
    const apply = useChatAssistantStore.getState().applyFrame;
    apply({ type: "assistant_delta", id: "a-1", delta: "Hel" });
    apply({ type: "assistant_delta", id: "a-1", delta: "lo," });
    apply({ type: "assistant_delta", id: "a-1", delta: " world" });
    const msgs = useChatAssistantStore.getState().messages;
    expect(msgs).toHaveLength(1);
    expect(msgs[0]).toMatchObject({ id: "a-1", role: "assistant", text: "Hello, world" });
  });

  it("attaches tool_call to the parent assistant message and is idempotent", () => {
    const apply = useChatAssistantStore.getState().applyFrame;
    apply({ type: "assistant_delta", id: "a-1", delta: "thinking" });
    apply({
      type: "tool_call",
      id: "a-1",
      toolCallId: "tc-1",
      tool: "cordum_list_jobs",
      args: { limit: 10 },
    });
    apply({
      type: "tool_call",
      id: "a-1",
      toolCallId: "tc-1",
      tool: "cordum_list_jobs",
      args: { limit: 10 },
    });
    const msgs = useChatAssistantStore.getState().messages;
    expect(msgs[0].toolCalls).toHaveLength(1);
    expect(msgs[0].toolCalls[0].tool).toBe("cordum_list_jobs");
  });

  it("merges tool_result into the matching tool_call", () => {
    const apply = useChatAssistantStore.getState().applyFrame;
    apply({
      type: "tool_call",
      id: "a-1",
      toolCallId: "tc-1",
      tool: "cordum_list_jobs",
      args: {},
    });
    apply({
      type: "tool_result",
      id: "a-1",
      toolCallId: "tc-1",
      ok: true,
      resultPreview: "5 jobs",
    });
    const tc = useChatAssistantStore.getState().messages[0].toolCalls[0];
    expect(tc.result).toEqual({ ok: true, resultPreview: "5 jobs" });
  });

  it("approval_required adds a pending approval and tool_result resolves it", () => {
    const apply = useChatAssistantStore.getState().applyFrame;
    apply({
      type: "tool_call",
      id: "a-1",
      toolCallId: "tc-1",
      tool: "cordum_approve_job",
      args: { jobId: "job-7" },
    });
    apply({
      type: "approval_required",
      id: "a-1",
      toolCallId: "tc-1",
      approvalId: "appr-9",
      tool: "cordum_approve_job",
      args: { jobId: "job-7" },
    });
    expect(useChatAssistantStore.getState().pendingApprovalIds).toEqual(["appr-9"]);
    apply({
      type: "tool_result",
      id: "a-1",
      toolCallId: "tc-1",
      ok: true,
      resultPreview: "approved",
    });
    const tc = useChatAssistantStore.getState().messages[0].toolCalls[0];
    expect(tc.approval).toMatchObject({ approvalId: "appr-9", status: "resolved" });
  });

  it("error frame appends a bracketed marker without overwriting prior text", () => {
    const apply = useChatAssistantStore.getState().applyFrame;
    apply({ type: "assistant_delta", id: "a-1", delta: "Hello" });
    apply({ type: "error", id: "a-1", message: "stream timed out" });
    expect(useChatAssistantStore.getState().messages[0].text).toBe(
      "Hello\n\n[error] stream timed out",
    );
  });

  it("clearSession wipes session, messages, approvals, and unread", () => {
    const s = useChatAssistantStore.getState();
    s.setSession("sess-1");
    s.applyFrame({
      type: "approval_required",
      id: "a-1",
      toolCallId: "tc-1",
      approvalId: "appr-1",
      tool: "x",
      args: {},
    });
    s.clearSession();
    const after = useChatAssistantStore.getState();
    expect(after.sessionId).toBeNull();
    expect(after.messages).toEqual([]);
    expect(after.pendingApprovalIds).toEqual([]);
    expect(after.unreadCount).toBe(0);
  });
});
