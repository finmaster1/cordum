import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { ChatAssistantMessage, ChatFrame } from "../types/chatAssistant";

/**
 * Session state for the LLM chat assistant widget. Disjoint from the existing
 * run-keyed `useChatStore` (state/chat.ts) — this one models a Gmail-style
 * informational conversation.
 */
export interface ChatAssistantState {
  panelOpen: boolean;
  sessionId: string | null;
  messages: ChatAssistantMessage[];
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

function findOrAppendAssistant(
  messages: ChatAssistantMessage[],
  frameId: string,
  at: string,
): { list: ChatAssistantMessage[]; index: number } {
  const idx = messages.findIndex((m) => m.id === frameId && m.role === "assistant");
  if (idx >= 0) return { list: messages, index: idx };
  const next: ChatAssistantMessage = { id: frameId, role: "assistant", text: "", at };
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

export const useChatAssistantStore = create<ChatAssistantState>()(
  persist(
    (set) => ({
      panelOpen: false,
      sessionId: null,
      messages: [],
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
                at,
              };
              return { messages: [...state.messages, userMessage] };
            }
            case "assistant_delta": {
              const { list, index } = findOrAppendAssistant(state.messages, frame.id, at);
              const current = list[index];
              const messages = patchMessage(list, index, { text: current.text + frame.delta });
              const unread = state.panelOpen ? state.unreadCount : state.unreadCount + 1;
              return { messages, unreadCount: unread };
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

      clearSession: () => set({ sessionId: null, messages: [], unreadCount: 0 }),

      markRead: () => set({ unreadCount: 0 }),
    }),
    {
      name: "cordum-chat-assistant",
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({ panelOpen: state.panelOpen, sessionId: state.sessionId }),
    },
  ),
);

export function resetChatAssistantStore(): void {
  useChatAssistantStore.setState({ panelOpen: false, sessionId: null, messages: [], unreadCount: 0 });
  if (typeof window !== "undefined" && window.localStorage) {
    window.localStorage.removeItem("cordum-chat-assistant");
  }
}
