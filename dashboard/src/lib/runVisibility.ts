import type { RunStatus } from "../api/types";

export type RunVisibilityState =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "blocked";

const TERMINAL_RUN_STATUSES = new Set<RunStatus>([
  "succeeded",
  "failed",
  "denied",
  "timed_out",
  "cancelled",
]);

export function normalizeRunStatusValue(raw?: string): RunStatus | undefined {
  const lower = (raw || "").trim().toLowerCase();
  if (!lower) return undefined;

  switch (lower) {
    case "pending":
    case "queued":
      return "pending";
    case "running":
      return "running";
    case "waiting":
      return "waiting";
    case "succeeded":
    case "completed":
    case "success":
      return "succeeded";
    case "failed":
    case "error":
    case "errored":
      return "failed";
    case "denied":
    case "blocked":
      return "denied";
    case "timed_out":
    case "timeout":
    case "timedout":
      return "timed_out";
    case "cancelled":
    case "canceled":
      return "cancelled";
    default:
      return undefined;
  }
}

export function toRunVisibilityState(status?: string): RunVisibilityState | undefined {
  const normalized = normalizeRunStatusValue(status);
  if (!normalized) return undefined;

  switch (normalized) {
    case "pending":
      return "queued";
    case "running":
      return "running";
    case "waiting":
    case "denied":
      return "blocked";
    case "succeeded":
      return "completed";
    case "failed":
    case "timed_out":
    case "cancelled":
      return "failed";
    default:
      return undefined;
  }
}

export function isRunVisibilityActive(status?: string): boolean {
  const visibility = toRunVisibilityState(status);
  return visibility === "queued" || visibility === "running";
}

export function isRunVisibilityTerminal(status?: string): boolean {
  const visibility = toRunVisibilityState(status);
  return visibility === "completed" || visibility === "failed" || visibility === "blocked";
}

export function isTerminalRunStatus(status?: string): boolean {
  const normalized = normalizeRunStatusValue(status);
  return normalized ? TERMINAL_RUN_STATUSES.has(normalized) : false;
}
