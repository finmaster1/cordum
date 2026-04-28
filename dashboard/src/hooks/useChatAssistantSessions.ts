import { useQuery } from "@tanstack/react-query";
import { get } from "@/api/client";
import type { ChatAssistantSessionDetail, ChatAssistantSessionSummary } from "@/types/chatAssistant";

interface BackendSessionsResponse {
  items?: BackendSessionSummary[];
  next_cursor?: string | null;
}

interface BackendSessionSummary {
  session_id?: string;
  sessionId?: string;
  principal?: string;
  user_principal?: string;
  tenant?: string;
  created_at?: string;
  createdAt?: string;
  last_active_at?: string;
  lastActiveAt?: string;
  message_count?: number;
  messageCount?: number;
}

interface BackendSessionDetail extends BackendSessionSummary {
  messages?: BackendMessage[];
}

interface BackendMessage {
  id?: string;
  role?: "user" | "assistant";
  text?: string;
  at?: string;
}

function normalizeSummary(raw: BackendSessionSummary): ChatAssistantSessionSummary {
  return {
    sessionId: raw.session_id ?? raw.sessionId ?? "",
    principal: raw.principal ?? raw.user_principal ?? "",
    tenant: raw.tenant ?? "",
    createdAt: raw.created_at ?? raw.createdAt ?? "",
    lastActiveAt: raw.last_active_at ?? raw.lastActiveAt ?? "",
    messageCount: raw.message_count ?? raw.messageCount ?? 0,
  };
}

function normalizeDetail(raw: BackendSessionDetail): ChatAssistantSessionDetail {
  return {
    ...normalizeSummary(raw),
    messages: ((raw.messages ?? []) as BackendMessage[]).map((m) => ({
      id: m.id ?? "",
      role: (m.role ?? "assistant") as "user" | "assistant",
      text: m.text ?? "",
      at: m.at ?? "",
    })),
  };
}

export interface UseChatAssistantSessionsParams {
  cursor?: string;
  limit?: number;
  enabled?: boolean;
}

export interface ChatAssistantSessionsPage {
  items: ChatAssistantSessionSummary[];
  nextCursor: string | null;
}

export function useChatAssistantSessions({
  cursor,
  limit = 50,
  enabled = true,
}: UseChatAssistantSessionsParams = {}) {
  return useQuery<ChatAssistantSessionsPage>({
    queryKey: ["chat-assistant", "sessions", { cursor: cursor ?? null, limit }],
    enabled,
    queryFn: async () => {
      const params = new URLSearchParams();
      if (cursor) params.set("cursor", cursor);
      if (limit) params.set("limit", String(limit));
      const path = `/chat/sessions${params.size ? `?${params.toString()}` : ""}`;
      const res = await get<BackendSessionsResponse>(path);
      return {
        items: (res.items ?? []).map(normalizeSummary),
        nextCursor: res.next_cursor ?? null,
      };
    },
    staleTime: 5_000,
    refetchInterval: 30_000,
  });
}

export function useChatAssistantSessionDetail(sessionId: string | undefined) {
  return useQuery<ChatAssistantSessionDetail>({
    queryKey: ["chat-assistant", "session", sessionId ?? null],
    enabled: !!sessionId,
    queryFn: async () => {
      const res = await get<BackendSessionDetail>(`/chat/sessions/${encodeURIComponent(sessionId!)}`);
      return normalizeDetail(res);
    },
    staleTime: 5_000,
  });
}
