import { CheckCircle2, AlertTriangle, Clock } from "lucide-react";
import { CodeBlock } from "../ui/CodeBlock";
import { formatRelative } from "../../lib/format";
import type { ActivityItem } from "../../types/activity";

type Props = { activity: ActivityItem };

export function ToolResultBlock({ activity }: Props) {
  const statusCode = activity.payload?.status_code;
  const isError = typeof statusCode === "number" ? statusCode >= 400 : false;
  const Icon = isError ? AlertTriangle : CheckCircle2;

  return (
    <div className="rounded-2xl border border-border bg-card/80 p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Icon className={`h-4 w-4 ${isError ? "text-danger" : "text-success"}`} />
          <div className="text-sm font-semibold text-ink">Tool result</div>
        </div>
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          {activity.payload?.latency_ms ? (
            <span className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              {activity.payload.latency_ms}ms
            </span>
          ) : null}
          <span>{formatRelative(activity.timestamp)}</span>
        </div>
      </div>
      {activity.payload?.tool_output ? (
        <div className="mt-3">
          <CodeBlock language="json" maxHeight={200}>{JSON.stringify(activity.payload.tool_output, null, 2)}</CodeBlock>
        </div>
      ) : (
        <div className="mt-2 text-xs text-muted-foreground">No output recorded.</div>
      )}
    </div>
  );
}
