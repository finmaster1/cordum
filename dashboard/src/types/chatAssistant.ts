/**
 * Wire-format types for the Cordum LLM chat assistant.
 *
 * Informational-only chat frames mirror the server-side schema: user echoes,
 * assistant deltas, final, and structured errors. Action and approval frames
 * are intentionally absent from the chat widget contract.
 */

export type ChatFrame = UserFrame | AssistantDeltaFrame | FinalFrame | ErrorFrame;

export interface UserFrame {
  type: "user";
  id: string;
  text: string;
  at?: string;
}

export interface AssistantDeltaFrame {
  type: "assistant_delta";
  id: string;
  delta: string;
  at?: string;
}

export interface FinalFrame {
  type: "final";
  id: string;
  at?: string;
}

export interface ErrorFrame {
  type: "error";
  id: string;
  message: string;
  code?: string;
  at?: string;
}

/** One assistant turn or user message in the rendered transcript. */
export interface ChatAssistantMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
  at: string;
}

export interface ChatAssistantSessionSummary {
  sessionId: string;
  principal: string;
  tenant: string;
  createdAt: string;
  lastActiveAt: string;
  messageCount: number;
}

export interface ChatAssistantSessionDetail extends ChatAssistantSessionSummary {
  messages: ChatAssistantMessage[];
}

/**
 * /api/v1/chat/healthz response shape. The widget mirrors the server's
 * 200 vs 503 split into a tagged union so the consuming UI never has to
 * inspect HTTP status codes itself.
 */
export type AvailabilityStatus =
  | { available: true }
  | { available: false; reason: AvailabilityReason };

export type AvailabilityReason =
  | "vllm_down"
  | "redis_down"
  | "unauthorized"
  | "unentitled"
  | "unknown";

export type ChatConnectionStatus = "idle" | "connecting" | "open" | "reconnecting" | "closed";
