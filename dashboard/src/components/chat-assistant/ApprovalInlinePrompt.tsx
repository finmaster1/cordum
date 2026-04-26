import { useState } from "react";
import { Check, X, Loader2 } from "lucide-react";
import { post } from "@/api/client";
import { cn } from "@/lib/utils";
import type { AttachedToolCall } from "@/types/chatAssistant";

interface ApprovalInlinePromptProps {
  toolCall: AttachedToolCall;
}

type PendingState = "idle" | "approving" | "rejecting";

export function ApprovalInlinePrompt({ toolCall }: ApprovalInlinePromptProps) {
  const approval = toolCall.approval;
  const [state, setState] = useState<PendingState>("idle");
  const [error, setError] = useState<string | null>(null);

  if (!approval || approval.status !== "pending") {
    return null;
  }

  async function decide(action: "approve" | "reject") {
    if (!approval) return;
    setError(null);
    setState(action === "approve" ? "approving" : "rejecting");
    try {
      await post(`/approvals/${encodeURIComponent(approval.approvalId)}/${action}`, {
        reason: action === "approve" ? "approved via chat assistant" : "rejected via chat assistant",
      });
      // Tool result frame from the server will flip approval.status to
      // resolved/rejected. We optimistically reset local state so the UI
      // doesn't show two spinners if the WS frame races us.
      setState("idle");
    } catch (err) {
      setState("idle");
      setError(err instanceof Error ? err.message : "request failed");
    }
  }

  return (
    <div
      role="region"
      aria-label="approval required"
      className="mt-2 rounded-xl border border-status-warning/30 bg-status-warning/10 px-3 py-2 text-xs"
    >
      <div className="font-semibold text-status-warning">Approval required</div>
      <div className="mt-0.5 font-mono text-muted-foreground/80">
        {toolCall.tool}
      </div>
      {error && (
        <div className="mt-1 text-[11px] text-status-error" aria-live="polite">
          {error}
        </div>
      )}
      <div className="mt-2 flex items-center gap-2">
        <button
          type="button"
          onClick={() => decide("approve")}
          disabled={state !== "idle"}
          className={cn(
            "flex items-center gap-1 rounded-xl px-2.5 py-1 text-xs font-semibold transition-colors",
            "border border-status-healthy/40 bg-status-healthy/10 text-status-healthy",
            "hover:bg-status-healthy/20 disabled:opacity-60",
          )}
        >
          {state === "approving" ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Check className="h-3 w-3" />
          )}
          Approve
        </button>
        <button
          type="button"
          onClick={() => decide("reject")}
          disabled={state !== "idle"}
          className={cn(
            "flex items-center gap-1 rounded-xl px-2.5 py-1 text-xs font-semibold transition-colors",
            "border border-status-error/40 bg-status-error/10 text-status-error",
            "hover:bg-status-error/20 disabled:opacity-60",
          )}
        >
          {state === "rejecting" ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <X className="h-3 w-3" />
          )}
          Reject
        </button>
      </div>
    </div>
  );
}
