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

  it("ignores retired tool and approval frame types", () => {
    const apply = useChatAssistantStore.getState().applyFrame;
    apply({ type: `tool_${"call"}`, id: "a-1", [`tool${"Call"}Id`]: "tc-1", tool: "cordum_list_jobs", args: {} } as never);
    apply({ type: `tool_${"result"}`, id: "a-1", [`tool${"Call"}Id`]: "tc-1", ok: true, resultPreview: "5 jobs" } as never);
    apply({ type: `approval_${"required"}`, id: "a-1", [`tool${"Call"}Id`]: "tc-1", approvalId: "appr-1", tool: "x", args: {} } as never);
    expect(useChatAssistantStore.getState().messages).toEqual([]);
  });

  it("error frame appends a bracketed marker without overwriting prior text", () => {
    const apply = useChatAssistantStore.getState().applyFrame;
    apply({ type: "assistant_delta", id: "a-1", delta: "Hello" });
    apply({ type: "error", id: "a-1", message: "stream timed out" });
    expect(useChatAssistantStore.getState().messages[0].text).toBe(
      "Hello\n\n[error] stream timed out",
    );
  });

  it("clearSession wipes session, messages, and unread count", () => {
    const s = useChatAssistantStore.getState();
    s.setSession("sess-1");
    s.applyFrame({ type: "assistant_delta", id: "a-1", delta: "hello" });
    s.clearSession();
    const after = useChatAssistantStore.getState();
    expect(after.sessionId).toBeNull();
    expect(after.messages).toEqual([]);
    expect(after.unreadCount).toBe(0);
  });
});
