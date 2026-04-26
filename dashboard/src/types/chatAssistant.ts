/**
 * Wire-format types for the Cordum LLM chat assistant.
 *
 * The shapes here mirror the server-side frame schema pinned by phase-5
 * (cordum/docs/llmchat/overview.md) — the dashboard must not drift, since
 * the WebSocket payloads are the public contract between the two.
 */

export type ChatFrame =
  | UserFrame
  | AssistantDeltaFrame
  | ToolCallFrame
  | ToolResultFrame
  | ApprovalRequiredFrame
  | FinalFrame
  | ErrorFrame;

export interface UserFrame {
  type: "user";
  id: string;
  text: string;
  at: string;
}

export interface AssistantDeltaFrame {
  type: "assistant_delta";
  id: string;
  delta: string;
  at?: string;
}

export interface ToolCallFrame {
  type: "tool_call";
  id: string;
  toolCallId: string;
  tool: string;
  args: Record<string, unknown>;
  at?: string;
}

export interface ToolResultFrame {
  type: "tool_result";
  id: string;
  toolCallId: string;
  ok: boolean;
  resultPreview: string;
  at?: string;
}

export interface ApprovalRequiredFrame {
  type: "approval_required";
  id: string;
  toolCallId: string;
  approvalId: string;
  tool: string;
  args: Record<string, unknown>;
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

/**
 * One assistant turn or user message in the rendered transcript. ToolCalls
 * are attached to the assistant message that produced them so the UI can
 * render them inline below the assistant bubble.
 */
export interface ChatAssistantMessage {
  id: string;
  role: "user" | "assistant";
  text: string;
  toolCalls: AttachedToolCall[];
  at: string;
}

export interface AttachedToolCall {
  toolCallId: string;
  tool: string;
  args: Record<string, unknown>;
  result?: { ok: boolean; resultPreview: string };
  approval?: { approvalId: string; status: "pending" | "resolved" | "rejected" };
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

export type ChatConnectionStatus =
  | "idle"
  | "connecting"
  | "open"
  | "reconnecting"
  | "closed";
