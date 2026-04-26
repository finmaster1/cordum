import { useState } from "react";
import { Check, X, Loader2, ShieldCheck } from "lucide-react";
import { post } from "@/api/client";
import { cn } from "@/lib/utils";
import type { AttachedToolCall } from "@/types/chatAssistant";

interface ApprovalInlinePromptProps {
  toolCall: AttachedToolCall;
}

type PendingState = "idle" | "approving" | "rejecting";

// previewArgs renders tool call arguments as a compact JSON string the
// operator can scan before approving — overreliance affordance: the user
// sees exactly what they are authorising, not a paraphrase from the LLM.
function previewArgs(args: Record<string, unknown>): string {
  try {
    const json = JSON.stringify(args, null, 2);
    return json.length > 600 ? `${json.slice(0, 597)}…` : json;
  } catch {
    return "{…}";
  }
}

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
      // resolved/rejected. Optimistically reset local state so the UI
      // does not show two spinners if the WS frame races us.
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
      <div className="flex items-center gap-1.5 font-semibold text-status-warning">
        <ShieldCheck className="h-3.5 w-3.5" aria-hidden="true" />
        <span>Cordum approval gate paused this call</span>
      </div>
      <p className="mt-1 text-[11px] leading-snug text-muted-foreground">
        The chat assistant is not allowed to run this mutating tool
        without an explicit human decision. Review the arguments below;
        only Approve when you are confident they are correct.
      </p>
      <div className="mt-2 font-mono text-[11px] text-foreground">{toolCall.tool}</div>
      <pre
        aria-label="tool call arguments"
        className="mt-1 max-h-48 overflow-auto rounded-lg bg-surface-0 p-2 font-mono text-[11px] text-muted-foreground"
      >
        {previewArgs(toolCall.args)}
      </pre>
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
          aria-label={`Approve ${toolCall.tool} — verify arguments first`}
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
          Verify and approve
        </button>
        <button
          type="button"
          onClick={() => decide("reject")}
          disabled={state !== "idle"}
          aria-label={`Reject ${toolCall.tool}`}
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
      <p className="mt-1.5 text-[10px] uppercase tracking-widest text-muted-foreground/60">
        Audit chain records every decision
      </p>
    </div>
  );
}
