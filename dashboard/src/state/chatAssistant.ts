import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type {
  AttachedToolCall,
  ChatAssistantMessage,
  ChatFrame,
} from "../types/chatAssistant";

/**
 * Session state for the LLM chat assistant widget. Disjoint from the
 * existing run-keyed `useChatStore` (state/chat.ts) — that one models
 * agent run output, this one models a Gmail-style conversational session.
 *
 * Persistence policy:
 *  - `panelOpen` and `sessionId` persist to localStorage (Zustand
 *    `persist` + `createJSONStorage(() => localStorage)`) so the
 *    conversation pointer survives both same-tab page reload AND tab
 *    close + browser restart. This is the Gmail-style resume contract:
 *    a returning user clicks the header chat-button and lands back in
 *    the previous transcript without re-typing.
 *  - Messages are kept in memory only; on reconnect, they are reloaded
 *    from `GET /api/v1/chat/sessions/{id}` keyed by the persisted
 *    sessionId. Server-side state is authoritative.
 *  - The persisted localStorage entry is principal-scoped only via the
 *    server-side resume check (the gateway rejects a sessionId that
 *    does not match the new principal+tenant). To avoid even surfacing
 *    a stale pointer to the next operator on a shared workstation,
 *    `resetChatAssistantStore` is invoked from `useConfigStore.logout()`
 *    on every sign-out, 401-on-license, and any other path that flushes
 *    `useConfigStore`. That call nukes both the in-memory state and the
 *    `cordum-chat-assistant` localStorage key.
 */
export interface ChatAssistantState {
  panelOpen: boolean;
  sessionId: string | null;
  messages: ChatAssistantMessage[];
  pendingApprovalIds: string[];
  unreadCount: number;

  togglePanel: () => void;
  openPanel: () => void;
  closePanel: () => void;
  setSession: (sessionId: string | null) => void;
  setMessages: (messages: ChatAssistantMessage[]) => void;
  applyFrame: (frame: ChatFrame) => void;
  clearSession: () => void;
  markRead: () => void;
}

function isoNow(): string {
  return new Date().toISOString();
}

function findOrAppendAssistant(messages: ChatAssistantMessage[], frameId: string, at: string): {
  list: ChatAssistantMessage[];
  index: number;
} {
  const idx = messages.findIndex((m) => m.id === frameId && m.role === "assistant");
  if (idx >= 0) {
    return { list: messages, index: idx };
  }
  const next: ChatAssistantMessage = {
    id: frameId,
    role: "assistant",
    text: "",
    toolCalls: [],
    at,
  };
  return { list: [...messages, next], index: messages.length };
}

function patchMessage(
  list: ChatAssistantMessage[],
  index: number,
  patch: Partial<ChatAssistantMessage>,
): ChatAssistantMessage[] {
  if (index < 0 || index >= list.length) return list;
  const next = list.slice();
  next[index] = { ...next[index], ...patch };
  return next;
}

function patchToolCall(
  message: ChatAssistantMessage,
  toolCallId: string,
  patch: Partial<AttachedToolCall>,
): ChatAssistantMessage {
  const idx = message.toolCalls.findIndex((tc) => tc.toolCallId === toolCallId);
  if (idx < 0) return message;
  const calls = message.toolCalls.slice();
  calls[idx] = { ...calls[idx], ...patch };
  return { ...message, toolCalls: calls };
}

export const useChatAssistantStore = create<ChatAssistantState>()(
  persist(
    (set) => ({
      panelOpen: false,
      sessionId: null,
      messages: [],
      pendingApprovalIds: [],
      unreadCount: 0,

      togglePanel: () =>
        set((s) => ({
          panelOpen: !s.panelOpen,
          unreadCount: s.panelOpen ? s.unreadCount : 0,
        })),
      openPanel: () => set({ panelOpen: true, unreadCount: 0 }),
      closePanel: () => set({ panelOpen: false }),

      setSession: (sessionId) => set({ sessionId }),
      setMessages: (messages) => set({ messages }),

      applyFrame: (frame) =>
        set((state) => {
          const at = frame.at ?? isoNow();
          switch (frame.type) {
            case "user": {
              const exists = state.messages.some((m) => m.id === frame.id);
              if (exists) return state;
              const userMessage: ChatAssistantMessage = {
                id: frame.id,
                role: "user",
                text: frame.text,
                toolCalls: [],
                at,
              };
              return { messages: [...state.messages, userMessage] };
            }
            case "assistant_delta": {
              const { list, index } = findOrAppendAssistant(state.messages, frame.id, at);
              const current = list[index];
              const messages = patchMessage(list, index, {
                text: current.text + frame.delta,
              });
              const unread = state.panelOpen ? state.unreadCount : state.unreadCount + 1;
              return { messages, unreadCount: unread };
            }
            case "tool_call": {
              const { list, index } = findOrAppendAssistant(state.messages, frame.id, at);
              const current = list[index];
              if (current.toolCalls.some((tc) => tc.toolCallId === frame.toolCallId)) {
                return { messages: list };
              }
              const messages = patchMessage(list, index, {
                toolCalls: [
                  ...current.toolCalls,
                  { toolCallId: frame.toolCallId, tool: frame.tool, args: frame.args },
                ],
              });
              return { messages };
            }
            case "tool_result": {
              const { list, index } = findOrAppendAssistant(state.messages, frame.id, at);
              const current = list[index];
              const patched = patchToolCall(current, frame.toolCallId, {
                result: { ok: frame.ok, resultPreview: frame.resultPreview },
                approval: current.toolCalls.find((tc) => tc.toolCallId === frame.toolCallId)?.approval
                  ? { ...current.toolCalls.find((tc) => tc.toolCallId === frame.toolCallId)!.approval!, status: "resolved" }
                  : undefined,
              });
              const messages = patchMessage(list, index, { toolCalls: patched.toolCalls });
              return { messages };
            }
            case "approval_required": {
              const { list, index } = findOrAppendAssistant(state.messages, frame.id, at);
              const current = list[index];
              const existingIdx = current.toolCalls.findIndex(
                (tc) => tc.toolCallId === frame.toolCallId,
              );
              const approvalEntry: AttachedToolCall["approval"] = {
                approvalId: frame.approvalId,
                status: "pending",
              };
              const calls =
                existingIdx >= 0
                  ? current.toolCalls.map((tc, i) =>
                      i === existingIdx
                        ? { ...tc, approval: approvalEntry, tool: frame.tool, args: frame.args }
                        : tc,
                    )
                  : [
                      ...current.toolCalls,
                      {
                        toolCallId: frame.toolCallId,
                        tool: frame.tool,
                        args: frame.args,
                        approval: approvalEntry,
                      },
                    ];
              const messages = patchMessage(list, index, { toolCalls: calls });
              const pending = state.pendingApprovalIds.includes(frame.approvalId)
                ? state.pendingApprovalIds
                : [...state.pendingApprovalIds, frame.approvalId];
              return { messages, pendingApprovalIds: pending };
            }
            case "final": {
              return state;
            }
            case "error": {
              const { list, index } = findOrAppendAssistant(state.messages, frame.id, at);
              const current = list[index];
              const messages = patchMessage(list, index, {
                text: current.text
                  ? `${current.text}\n\n[error] ${frame.message}`
                  : `[error] ${frame.message}`,
              });
              return { messages };
            }
            default:
              return state;
          }
        }),

      clearSession: () =>
        set({
          sessionId: null,
          messages: [],
          pendingApprovalIds: [],
          unreadCount: 0,
        }),

      markRead: () => set({ unreadCount: 0 }),
    }),
    {
      name: "cordum-chat-assistant",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        panelOpen: state.panelOpen,
        sessionId: state.sessionId,
      }),
    },
  ),
);

export function resetChatAssistantStore(): void {
  useChatAssistantStore.setState({
    panelOpen: false,
    sessionId: null,
    messages: [],
    pendingApprovalIds: [],
    unreadCount: 0,
  });
  if (typeof window !== "undefined" && window.localStorage) {
    window.localStorage.removeItem("cordum-chat-assistant");
  }
}
