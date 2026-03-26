import { memo, useState, useCallback } from "react";
import { Handle, Position, type NodeProps } from "reactflow";
import { CheckCircle, Loader2, XCircle, Clock, Slash, UserCheck } from "lucide-react";
import { cn } from "@/lib/utils";
import { formatDuration } from "@/lib/utils";
import type { UnifiedNodeData } from "../types";
import {
  getStepMeta,
  getStatusVisual,
  getSafetyBadge,
  isJobType,
  truncate,
} from "../nodeRegistry";

// ---------------------------------------------------------------------------
// Status icon resolver (shared between tooltip and node body)
// ---------------------------------------------------------------------------

function StatusIcon({ status }: { status?: string }) {
  switch (status) {
    case "succeeded":
      return <CheckCircle className="h-3.5 w-3.5 text-[var(--color-success)]" />;
    case "running":
      return <Loader2 className="h-3.5 w-3.5 text-[var(--color-info)] animate-spin" />;
    case "failed":
      return <XCircle className="h-3.5 w-3.5 text-destructive" />;
    case "waiting":
      return <UserCheck className="h-3.5 w-3.5 text-[var(--color-warning)]" />;
    case "cancelled":
      return <Slash className="h-3.5 w-3.5 text-muted-foreground" />;
    case "timed_out":
      return <Clock className="h-3.5 w-3.5 text-destructive" />;
    default:
      return null;
  }
}

// ---------------------------------------------------------------------------
// Hover tooltip
// ---------------------------------------------------------------------------

function NodeTooltip({ data }: { data: UnifiedNodeData }) {
  const meta = getStepMeta(data.stepType);
  const statusVisual = getStatusVisual(data.runStatus);
  const Icon = meta.icon;

  return (
    <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-50 pointer-events-none">
      <div className="min-w-[200px] max-w-[280px] space-y-1.5 whitespace-nowrap rounded-2xl border border-border bg-surface-1 px-3 py-2.5 text-[11px] shadow-soft">
        <div className="flex items-center gap-1.5 font-semibold text-ink">
          <Icon className={cn("h-3.5 w-3.5", meta.iconColor)} />
          {truncate(data.label, 40)}
        </div>
        <div className="flex items-center gap-1.5 text-muted-foreground">
          <span className="capitalize">{data.stepType.replace(/-/g, " ")}</span>
          {data.runStatus && (
            <>
              <span className="text-border">&middot;</span>
              <span>{statusVisual.label}</span>
            </>
          )}
        </div>
        {data.topic && (
          <div className="text-muted-foreground font-mono truncate max-w-[260px]">
            topic: {data.topic}
          </div>
        )}
        {data.condition && (
          <div className="text-muted-foreground font-mono truncate max-w-[260px]">
            if: {truncate(data.condition, 50)}
          </div>
        )}
        {data.duration != null && (
          <div className="text-muted-foreground">Duration: {formatDuration(data.duration)}</div>
        )}
        {data.error && (
          <div className="max-w-[260px] whitespace-normal text-destructive">
            {truncate(data.error, 80)}
          </div>
        )}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Subtitle line — shows the most relevant config detail
// ---------------------------------------------------------------------------

function getSubtitle(data: UnifiedNodeData): string | null {
  if (data.topic) return data.topic;
  if (data.condition) return `if: ${data.condition}`;
  if (data.delay_sec) return `wait ${data.delay_sec}s`;
  if (data.delay_until) return `until: ${data.delay_until}`;
  if (data.for_each) return `each: ${data.for_each}`;

  const config = data.config as Record<string, unknown> | undefined;
  if (config?.url) return String(config.url);
  if (config?.message) return truncate(String(config.message), 40);

  const input = data.input as Record<string, unknown> | undefined;
  if (input?.prompt) return truncate(String(input.prompt), 40);

  return null;
}

// ---------------------------------------------------------------------------
// UnifiedNode
// ---------------------------------------------------------------------------

function UnifiedNodeInner({ data, selected }: NodeProps<UnifiedNodeData>) {
  const [hovered, setHovered] = useState(false);
  const meta = getStepMeta(data.stepType);
  const statusVisual = getStatusVisual(data.runStatus);
  const safetyBadge = isJobType(data.stepType) ? getSafetyBadge(data.safetyDecision?.type) : null;
  const subtitle = getSubtitle(data);
  const Icon = meta.icon;
  const isEdit = data.mode === "edit";
  const hasRunOverlay = !!data.runStatus;

  const handleMouseEnter = useCallback(() => setHovered(true), []);
  const handleMouseLeave = useCallback(() => setHovered(false), []);

  // Determine border and background based on run status or selection
  const borderClass = hasRunOverlay
    ? statusVisual.border
    : selected
      ? "border-accent ring-2 ring-accent/30"
      : "border-border hover:border-accent/40";

  const bgClass = hasRunOverlay ? statusVisual.bg : "bg-card";

  return (
    <div
      className={cn(
        "relative min-w-[180px] max-w-[220px] rounded-xl border-2 transition-all duration-200",
        "shadow-sm hover:shadow-md",
        bgClass,
        borderClass,
        statusVisual.pulse && "animate-pulse",
        statusVisual.dimmed && "opacity-60",
      )}
      onMouseEnter={handleMouseEnter}
      onMouseLeave={handleMouseLeave}
    >
      {/* Tooltip on hover */}
      {hovered && <NodeTooltip data={data} />}

      {/* Top input handle */}
      {!meta.hideInput && (
        <Handle
          type="target"
          position={Position.Top}
          className={cn(
            "!w-3 !h-3 !border-2 !border-card !rounded-full",
            hasRunOverlay ? "!bg-accent" : "!bg-muted-foreground/50",
            isEdit && "hover:!bg-accent",
          )}
        />
      )}

      {/* Safety decision corner badge */}
      {safetyBadge && (
        <span
          className={cn(
            "absolute -right-1.5 -top-1.5 flex h-5 w-5 items-center justify-center rounded-full text-[9px] font-bold shadow-sm z-10",
            safetyBadge.className,
          )}
          title={safetyBadge.label}
        >
          {safetyBadge.glyph}
        </span>
      )}

      {/* Main content */}
      <div className="px-3 py-2.5">
        {/* Icon + Label row */}
        <div className="flex items-center gap-2.5">
          <div
            className={cn(
              "flex h-8 w-8 shrink-0 items-center justify-center rounded-lg",
              meta.accent,
            )}
          >
            <Icon className={cn("h-4 w-4", meta.iconColor)} />
          </div>
          <div className="flex-1 min-w-0">
            <span
              className={cn(
                "block text-xs font-semibold text-ink truncate",
                statusVisual.strikethrough && "line-through",
              )}
              title={data.label}
            >
              {truncate(data.label, 24)}
            </span>
            <span className="block text-[10px] text-muted-foreground capitalize">
              {meta.label}
            </span>
          </div>
          {/* Status icon (run mode only) */}
          <StatusIcon status={data.runStatus} />
        </div>

        {/* Subtitle */}
        {subtitle && (
          <p
            className="mt-1.5 truncate text-[10px] text-muted-foreground font-mono"
            title={subtitle}
          >
            {truncate(subtitle, 30)}
          </p>
        )}

        {/* Footer: duration + error indicator (run mode) */}
        {(data.duration != null || data.error) && (
          <div className="mt-1.5 flex items-center justify-between text-[10px]">
            {data.duration != null ? (
              <span className="text-muted-foreground">{formatDuration(data.duration)}</span>
            ) : (
              <span />
            )}
            {data.error && (
              <span
                className="ml-1 h-2 w-2 shrink-0 rounded-full bg-destructive"
                title={truncate(data.error, 120)}
              />
            )}
          </div>
        )}
      </div>

      {/* Output handles */}
      {data.stepType === "condition" ? (
        <>
          <Handle
            type="source"
            id="true"
            position={Position.Right}
            className={cn(
              "!w-3 !h-3 !border-2 !border-card !rounded-full",
              data.conditionResult === false ? "!bg-muted !opacity-30" : "!bg-[var(--color-success)]",
            )}
          />
          <span className="absolute right-0 translate-x-full pl-1.5 top-1/2 -translate-y-1/2 text-[10px] text-muted-foreground pointer-events-none whitespace-nowrap font-mono">
            true
          </span>
          <Handle
            type="source"
            id="false"
            position={Position.Left}
            className={cn(
              "!w-3 !h-3 !border-2 !border-card !rounded-full",
              data.conditionResult === true ? "!bg-muted !opacity-30" : "!bg-destructive",
            )}
          />
          <span className="absolute left-0 -translate-x-full pr-1.5 top-1/2 -translate-y-1/2 text-[10px] text-muted-foreground pointer-events-none whitespace-nowrap font-mono">
            false
          </span>
        </>
      ) : (
        <Handle
          type="source"
          position={Position.Bottom}
          className={cn(
            "!w-3 !h-3 !border-2 !border-card !rounded-full",
            hasRunOverlay ? "!bg-accent" : "!bg-muted-foreground/50",
            isEdit && "hover:!bg-accent",
          )}
        />
      )}
    </div>
  );
}

export const UnifiedNode = memo(UnifiedNodeInner);
