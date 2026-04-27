import { describe, it, expect, beforeEach } from "vitest";
import { useConfigStore } from "../config";
import {
  resetChatAssistantStore,
  useChatAssistantStore,
} from "../chatAssistant";

const STORAGE_KEY = "cordum-chat-assistant";

function flushPersist(): Promise<void> {
  return Promise.resolve();
}

describe("ConfigStore.logout() resets ChatAssistantStore", () => {
  beforeEach(() => {
    resetChatAssistantStore();
    if (typeof window !== "undefined" && window.localStorage) {
      window.localStorage.removeItem(STORAGE_KEY);
    }
    useConfigStore.setState({ isLoggingOut: false });
  });

  it("clears in-memory chat-assistant state on logout", async () => {
    useChatAssistantStore.setState({
      panelOpen: true,
      sessionId: "sess-leak",
      messages: [
        {
          id: "u-1",
          role: "user",
          text: "hi",
          toolCalls: [],
          at: "2026-01-01T00:00:00Z",
        },
      ],
      pendingApprovalIds: ["appr-leak-1"],
      unreadCount: 3,
    });
    await flushPersist();

    const beforeRaw = window.localStorage.getItem(STORAGE_KEY);
    expect(beforeRaw).not.toBeNull();
    expect(beforeRaw).toContain("sess-leak");

    useConfigStore.getState().logout();

    const after = useChatAssistantStore.getState();
    expect(after.sessionId).toBeNull();
    expect(after.messages).toHaveLength(0);
    expect(after.panelOpen).toBe(false);
    expect(after.pendingApprovalIds).toHaveLength(0);
    expect(after.unreadCount).toBe(0);
  });

  it("removes the persisted localStorage entry on logout", async () => {
    useChatAssistantStore.setState({
      panelOpen: true,
      sessionId: "sess-cross-tenant",
    });
    await flushPersist();
    expect(window.localStorage.getItem(STORAGE_KEY)).not.toBeNull();

    useConfigStore.getState().logout();

    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
  });

  it("is idempotent — second logout does not resurrect chat state", async () => {
    useChatAssistantStore.setState({
      panelOpen: true,
      sessionId: "sess-idem",
      unreadCount: 5,
    });
    await flushPersist();

    useConfigStore.getState().logout();
    const afterFirst = useChatAssistantStore.getState();
    expect(afterFirst.sessionId).toBeNull();
    expect(afterFirst.unreadCount).toBe(0);

    useConfigStore.getState().logout();
    const afterSecond = useChatAssistantStore.getState();
    expect(afterSecond.sessionId).toBeNull();
    expect(afterSecond.messages).toHaveLength(0);
    expect(afterSecond.panelOpen).toBe(false);
    expect(afterSecond.pendingApprovalIds).toHaveLength(0);
    expect(afterSecond.unreadCount).toBe(0);
    expect(window.localStorage.getItem(STORAGE_KEY)).toBeNull();
  });
});
