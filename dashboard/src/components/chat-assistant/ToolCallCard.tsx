import { useState } from "react";
import { ChevronDown, ChevronRight, CheckCircle2, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import type { AttachedToolCall } from "@/types/chatAssistant";

interface ToolCallCardProps {
  toolCall: AttachedToolCall;
}

function previewArgs(args: Record<string, unknown>): string {
  try {
    const json = JSON.stringify(args);
    return json.length > 80 ? `${json.slice(0, 77)}…` : json;
  } catch {
    return "{…}";
  }
}

export function ToolCallCard({ toolCall }: ToolCallCardProps) {
  const [expanded, setExpanded] = useState(false);
  const result = toolCall.result;
  const ok = result?.ok;
  const StatusIcon = result ? (ok ? CheckCircle2 : XCircle) : null;
  const statusClass = result
    ? ok
      ? "text-status-healthy"
      : "text-status-error"
    : "text-muted-foreground";

  return (
    <div
      role="article"
      aria-label={`tool call ${toolCall.tool}`}
      className="mt-2 rounded-xl border border-border bg-surface-1/60"
    >
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((s) => !s)}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs"
      >
        {expanded ? (
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="font-mono font-semibold text-foreground">{toolCall.tool}</span>
        <span className="flex-1 truncate font-mono text-muted-foreground/70">
          {previewArgs(toolCall.args)}
        </span>
        {StatusIcon && (
          <StatusIcon className={cn("h-3.5 w-3.5 shrink-0", statusClass)} />
        )}
      </button>
      {expanded && (
        <div className="border-t border-border/50 px-3 py-2 space-y-2 text-xs">
          <div>
            <div className="mb-1 font-mono uppercase tracking-wide text-muted-foreground/60">args</div>
            <pre className="max-h-40 overflow-auto rounded-lg bg-surface-0 p-2 font-mono text-xs text-foreground">
              {JSON.stringify(toolCall.args, null, 2)}
            </pre>
          </div>
          {result && (
            <div>
              <div className="mb-1 flex items-center gap-1 font-mono uppercase tracking-wide text-muted-foreground/60">
                <span>result</span>
                <span className={cn("font-semibold", statusClass)}>
                  {ok ? "ok" : "error"}
                </span>
              </div>
              <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-surface-0 p-2 font-mono text-xs text-foreground">
                {result.resultPreview}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
