/*
 * DESIGN: "Control Surface" — Job Detail
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useParams, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { motion } from "framer-motion";
import { get } from "@/api/client";
import { mapJobDetail, type BackendJobDetail } from "@/api/transform";
import type { Job, OutputFinding } from "@/api/types";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import {
  ArrowLeft, Copy, Play, XCircle, Clock, Shield,
  FileText, AlertTriangle, Eye,
} from "lucide-react";
import { cn, formatRelativeTime, formatDuration } from "@/lib/utils";
import { useElapsedTimer } from "@/hooks/useElapsedTimer";
import { PageHeader } from "@/components/layout/PageHeader";
import { useState, useMemo } from "react";
import { toast } from "sonner";
import { useEventStore } from "@/state/events";
import { useCancelJob, useRetryJob } from "@/hooks/useJobs";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ErrorBanner } from "@/components/ui/ErrorBanner";
import { JobActions } from "@/components/jobs/JobActions";
import { CollapsibleSection } from "@/components/ui/CollapsibleSection";
import { CodeBlock } from "@/components/ui/CodeBlock";

function jobStatusVariant(status: string) {
  switch (status) {
    case "running": return "healthy" as const;
    case "succeeded": case "completed": return "cordum" as const;
    case "failed": case "timeout": case "timed_out": return "danger" as const;
    case "denied": case "output_quarantined": return "governance" as const;
    case "approval_required": return "warning" as const;
    case "pending": case "scheduled": return "warning" as const;
    case "dispatched": return "info" as const;
    case "cancelled": return "muted" as const;
    default: return "muted" as const;
  }
}

const JOB_STATES = ["pending", "scheduled", "dispatched", "running", "succeeded"];

const TERMINAL_ERROR_STATES = new Set(["failed", "timeout", "timed_out"]);
const TERMINAL_GOVERNANCE_STATES = new Set(["denied", "output_quarantined", "approval_required"]);

function StateMachine({ currentState }: { currentState: string }) {
  const normalized = currentState === "completed" ? "succeeded" : currentState;
  const currentIdx = JOB_STATES.indexOf(normalized);
  const isFailed = TERMINAL_ERROR_STATES.has(normalized);
  const isGovernance = TERMINAL_GOVERNANCE_STATES.has(normalized);
  const isTerminalSpecial = isFailed || isGovernance;

  return (
    <div className="flex items-center gap-1">
      {JOB_STATES.map((state, i) => {
        const isPast = i < currentIdx;
        const isCurrent = state === normalized;
        const isActive = isPast || isCurrent;

        return (
          <div key={state} className="flex items-center gap-1">
            <div
              className={cn(
                "flex items-center justify-center w-7 h-7 rounded-full text-xs font-mono uppercase transition-all",
                isCurrent && !isTerminalSpecial && "bg-cordum text-[#0f1518] ring-2 ring-cordum/30",
                isPast && "bg-cordum/20 text-cordum",
                !isActive && !isCurrent && "bg-surface-2 text-muted-foreground",
              )}
            >
              {isPast ? "✓" : (i + 1)}
            </div>
            {i < JOB_STATES.length - 1 && (
              <div className={cn("w-6 h-[2px] rounded", isPast ? "bg-cordum/40" : "bg-border")} />
            )}
          </div>
        );
      })}
      {isFailed && (
        <>
          <div className="w-6 h-[2px] rounded bg-destructive/40" />
          <div className="flex items-center justify-center w-7 h-7 rounded-full bg-destructive text-destructive-foreground text-xs ring-2 ring-destructive/30">
            ✕
          </div>
        </>
      )}
      {isGovernance && (
        <>
          <div className="w-6 h-[2px] rounded bg-[color:rgba(139,92,186,0.4)]" />
          <div className="flex items-center justify-center w-7 h-7 rounded-full bg-[color:rgba(139,92,186,0.18)] text-[color:rgba(139,92,186,1)] text-xs ring-2 ring-[color:rgba(139,92,186,0.3)]">
            ⊘
          </div>
        </>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// BlobViewer — shows Redis pointer + expandable "Read" button for data
// ---------------------------------------------------------------------------

const MAX_RESULT_DISPLAY = 100 * 1024; // 100KB

function formatBlobData(data: unknown): string | null {
  if (data == null) return null;
  if (typeof data === "string") {
    // Auto-parse JSON strings for pretty-printing
    try {
      const parsed = JSON.parse(data);
      if (typeof parsed === "object" && parsed !== null) {
        return JSON.stringify(parsed, null, 2);
      }
    } catch {
      // Not JSON — display as-is
    }
    return data;
  }
  return JSON.stringify(data, null, 2);
}

function BlobViewer({ label, pointer, data, emptyText }: {
  label: string;
  pointer?: string;
  data?: unknown;
  emptyText: string;
}) {
  const [expanded, setExpanded] = useState(false);
  const [showFull, setShowFull] = useState(false);

  if (!pointer && data == null) {
    return (
      <div className="surface-inset p-4 font-mono text-xs">
        <p className="text-muted-foreground italic">{emptyText}</p>
      </div>
    );
  }

  const formatted = formatBlobData(data);
  const isTruncated = formatted != null && formatted.length > MAX_RESULT_DISPLAY && !showFull;
  const displayText = isTruncated ? formatted.slice(0, MAX_RESULT_DISPLAY) : formatted;

  return (
    <div className="space-y-3">
      {pointer && (
        <div className="surface-inset p-4 font-mono text-xs flex items-center justify-between gap-3">
          <div className="min-w-0">
            <span className="text-muted-foreground">{label} pointer: </span>
            <span className="text-foreground break-all">{pointer}</span>
          </div>
          {formatted && (
            <Button
              variant="outline"
              size="sm"
              className="shrink-0"
              onClick={() => setExpanded(!expanded)}
            >
              <Eye className="w-3 h-3 mr-1" />
              {expanded ? "Hide" : "Read"}
            </Button>
          )}
        </div>
      )}
      {(expanded || !pointer) && displayText && (
        <div>
          <CodeBlock language="json" copyable maxHeight={500}>{displayText}</CodeBlock>
          {isTruncated && (
            <button
              type="button"
              onClick={() => setShowFull(true)}
              className="mt-2 text-cordum hover:underline text-xs"
            >
              Show full result ({Math.round((formatted?.length ?? 0) / 1024)}KB)
            </button>
          )}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Terminal — shows live WebSocket events + result data for a job
// ---------------------------------------------------------------------------

function JobTerminal({ job }: { job: Job }) {
  const events = useEventStore((s) => s.events);
  const jobEvents = useMemo(
    () =>
      events
        .filter((e) => {
          const p = e.payload ?? {};
          const eid = (p.jobId ?? p.job_id) as string | undefined;
          return eid === job.id;
        })
        .reverse(), // oldest first
    [events, job.id],
  );

  const hasResult = job.result != null;
  const hasEvents = jobEvents.length > 0;

  if (!hasResult && !hasEvents) {
    return (
      <p className="text-muted-foreground italic">
        {job.status === "running" || job.status === "pending" || job.status === "dispatched"
          ? "Waiting for output\u2026"
          : "No output recorded for this job."}
      </p>
    );
  }

  return (
    <div className="space-y-2">
      {hasEvents && jobEvents.map((e) => (
        <div key={e.id} className="flex gap-3">
          <span className="text-muted-foreground shrink-0 w-[80px]">
            {new Date(e.timestamp).toLocaleTimeString()}
          </span>
          <span className="text-cordum shrink-0">[{e.type}]</span>
          <span className="text-foreground break-all">
            {(e.payload?.message as string) ?? (e.payload?.status as string) ?? JSON.stringify(e.payload)}
          </span>
        </div>
      ))}
      {hasResult && (
        <>
          {hasEvents && <div className="border-t border-border my-3" />}
          <div className="text-muted-foreground mb-1">--- Result ---</div>
          <CodeBlock title="Result" language="json">{typeof job.result === "string" ? job.result : JSON.stringify(job.result, null, 2)}</CodeBlock>
        </>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Timeline — reconstructs chronological event list from job metadata
// ---------------------------------------------------------------------------

interface TimelineEntry {
  time: string;
  label: string;
  detail?: string;
  variant: "cordum" | "warning" | "danger" | "muted" | "governance";
}

function JobTimeline({ job }: { job: Job }) {
  const entries = useMemo(() => {
    const items: TimelineEntry[] = [];

    // Created
    if (job.createdAt) {
      items.push({ time: job.createdAt, label: "Job submitted", detail: `Topic: ${job.topic}`, variant: "muted" });
    }

    // Safety decision
    if (job.safetyDecision?.type) {
      const t = job.createdAt; // safety eval happens at submit time
      const variant = job.safetyDecision.type === "allow" ? "cordum"
        : job.safetyDecision.type === "deny" ? "governance"
        : "warning";
      items.push({
        time: t,
        label: `Safety: ${job.safetyDecision.type}`,
        detail: job.safetyDecision.reason || job.safetyDecision.matchedRule,
        variant,
      });
    }

    // Approval
    if (job.approvalAt) {
      const t = new Date(job.approvalAt).toISOString();
      items.push({
        time: t,
        label: `Approved by ${job.approvalBy ?? "unknown"}`,
        detail: job.approvalReason || job.approvalNote || undefined,
        variant: "cordum",
      });
    }

    // Output safety
    if (job.output_safety?.decision) {
      const variant = job.output_safety.decision === "ALLOW" ? "cordum"
        : job.output_safety.decision === "QUARANTINE" ? "danger"
        : "warning";
      items.push({
        time: job.updatedAt,
        label: `Output policy: ${job.output_safety.decision}`,
        detail: job.output_safety.reason,
        variant,
      });
    }

    // Error
    if (job.errorMessage || job.status === "failed") {
      items.push({ time: job.updatedAt, label: "Error", detail: job.errorMessage || `Job failed (no error message provided). Status code: ${job.errorCode || "unknown"}`, variant: "danger" });
    }

    // Final state
    if (job.status === "succeeded" || job.status === "failed" || job.status === "cancelled") {
      items.push({
        time: job.updatedAt,
        label: `Job ${job.status}`,
        detail: job.status === "succeeded" ? `Attempts: ${job.attempts ?? 1}` : undefined,
        variant: job.status === "succeeded" ? "cordum" : "danger",
      });
    }

    return items;
  }, [job]);

  if (entries.length === 0) {
    return <p className="text-sm text-muted-foreground italic">No timeline events available.</p>;
  }

  return (
    <div className="relative pl-6 space-y-4">
      <div className="absolute left-[9px] top-1 bottom-1 w-px bg-border" />
      {entries.map((entry, i) => (
        <div key={`${entry.label}-${i}`} className="relative flex items-start gap-3">
          <div
            className={cn(
              "absolute left-[-15px] top-1.5 w-[10px] h-[10px] rounded-full border-2",
              entry.variant === "cordum" && "border-cordum bg-cordum/20",
              entry.variant === "warning" && "border-[var(--color-warning)] bg-[var(--color-warning)]/20",
              entry.variant === "danger" && "border-destructive bg-destructive/20",
              entry.variant === "muted" && "border-border bg-surface-2",
            )}
          />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-xs font-semibold text-foreground">{entry.label}</span>
              <span
                className="text-xs text-muted-foreground font-mono"
                title={formatRelativeTime(entry.time)}
              >
                {new Date(entry.time).toLocaleString()}
              </span>
            </div>
            {entry.detail && (
              <p className="text-xs text-muted-foreground mt-0.5 break-all">{entry.detail}</p>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

export default function JobDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  // Cancel/retry handled by JobActions component
  const isJobActive = (s?: string) => !!s && ["running", "dispatched", "pending", "scheduled"].includes(s);

  const { data: job, isLoading, isError, error, refetch } = useQuery({
    queryKey: ["job", id],
    queryFn: async () => {
      const res = await get<BackendJobDetail>(`/jobs/${id}`);
      return mapJobDetail(res);
    },
    enabled: !!id,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      if (status && ["succeeded", "failed", "cancelled", "denied", "timeout", "output_quarantined"].includes(status)) return false;
      return 5_000;
    },
  });

  const { formatted: elapsedFormatted, elapsed } = useElapsedTimer(
    job?.createdAt,
    isJobActive(job?.status),
  );

  const copyId = () => {
    if (id) {
      navigator.clipboard.writeText(id);
      toast.success("Job ID copied");
    }
  };

  if (isError) {
    return <ErrorBanner message={error instanceof Error ? error.message : "Failed to load job details"} onRetry={() => void refetch()} />;
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-3">
          <Skeleton className="h-8 w-8" />
          <Skeleton className="h-6 w-48" />
        </div>
        <div className="grid grid-cols-2 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-32" />
          ))}
        </div>
      </div>
    );
  }

  if (!job) {
    return (
      <div className="flex flex-col items-center justify-center py-20">
        <AlertTriangle className="w-10 h-10 text-[var(--color-warning)] mb-3" />
        <h2 className="text-lg font-semibold font-display text-foreground">Job not found</h2>
        <p className="text-sm text-muted-foreground mt-1">The job may have been purged or the ID is invalid.</p>
        <Button variant="outline" size="sm" className="mt-4" onClick={() => navigate("/jobs")}>
          <ArrowLeft className="w-3 h-3 mr-1" />
          Back to Jobs
        </Button>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        label="Operate / Jobs"
        title={`Job ${id?.slice(0, 12)}…`}
        subtitle={id || ""}
        actions={
          <div className="flex gap-2 items-center">
            <StatusBadge variant={jobStatusVariant(job.status)} dot pulse={job.status === "running"}>
              {job.status === "output_quarantined" ? "quarantined" : job.status}
            </StatusBadge>
            <button type="button" onClick={copyId} className="p-2 rounded-full hover:bg-surface-2 text-muted-foreground hover:text-foreground transition-colors" title="Copy Job ID">
              <Copy className="w-3.5 h-3.5" />
            </button>
            <JobActions job={job} />
          </div>
        }
      />

      <div className="flex items-center gap-3">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate("/jobs")}
        >
          <ArrowLeft className="w-4 h-4 mr-1" />
          Back to Jobs
        </Button>
      </div>

      {/* Safety Bypass Warning */}
      {job.labels?.safety_bypassed === "true" && (
        <div className={cn("flex items-center gap-3 px-4 py-3 rounded-xl border", "bg-[var(--color-warning)]/10 border-[var(--color-warning)]/30 text-[var(--color-warning)]")}>
          <Shield className="w-5 h-5 flex-shrink-0" />
          <div>
            <p className="text-sm font-semibold">Safety Bypassed</p>
            <p className="text-xs opacity-80">
              This job was allowed via fail-open because the Safety Kernel was unavailable.
              {job.labels.safety_bypass_reason && (
                <> Reason: {job.labels.safety_bypass_reason}</>
              )}
            </p>
          </div>
        </div>
      )}

      {/* State Machine */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="instrument-card flex items-center justify-center"
      >
        <StateMachine currentState={job.status} />
      </motion.div>

      {/* ── A. Two-column summary ── */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3, delay: 0 }}
        className="space-y-4"
      >
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {/* Job Identity — includes workflow context */}
          <div className="instrument-card">
            <div className="flex items-center gap-2 mb-4">
              <FileText className="w-4 h-4 text-cordum" />
              <h2 className="font-display font-semibold text-sm text-foreground">Job Identity</h2>
            </div>
            <dl className="grid grid-cols-[110px_1fr] gap-x-6 gap-y-3 items-baseline">
              {([
                ["Topic", job.topic],
                ["Tenant", job.tenant],
                ["Team", job.team],
                ["Actor", job.actorId ? `${job.actorId} (${job.actorType})` : undefined],
                ["Capability", job.capability],
                ["Attempts", job.attempts ? String(job.attempts) : undefined],
                ["Trace ID", job.traceId],
                ["Workflow", job.workflowId],
                ["Run", job.workflowRunId],
              ] as [string, string | undefined][])
                .filter(([, value]) => !!value)
                .map(([label, value]) => (
                  <div key={label} className="contents">
                    <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">{label}</dt>
                    <dd className="text-sm text-foreground font-mono truncate">
                      {label === "Workflow" ? (
                        <button
                          type="button"
                          className="text-cordum hover:underline"
                          onClick={() => navigate(`/workflows/${job.workflowId}/studio`)}
                        >
                          {value}
                        </button>
                      ) : label === "Run" && job.workflowId && job.workflowRunId ? (
                        <button
                          type="button"
                          className="text-cordum hover:underline"
                          onClick={() => navigate(`/workflows/${job.workflowId}/runs/${job.workflowRunId}`)}
                        >
                          {value}
                        </button>
                      ) : value}
                    </dd>
                  </div>
                ))}
              {isJobActive(job.status) && (
                <div className="contents">
                  <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Elapsed</dt>
                  <dd className="text-sm font-mono text-foreground flex items-center gap-1.5">
                    <span className="w-1.5 h-1.5 rounded-full bg-cordum animate-pulse" />
                    {elapsedFormatted}
                  </dd>
                </div>
              )}
              {isJobActive(job.status) && job.createdAt && (
                <div className="contents">
                  <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Elapsed</dt>
                  <dd className="text-sm font-mono text-foreground flex items-center gap-1.5">
                    <Clock className="w-3.5 h-3.5 text-[var(--color-info)]" />
                    <span>{elapsedFormatted}</span>
                    <span className="w-1.5 h-1.5 rounded-full bg-[var(--color-info)] animate-pulse" />
                  </dd>
                </div>
              )}
              {!isJobActive(job.status) && job.createdAt && job.updatedAt && (
                <div className="contents">
                  <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Duration</dt>
                  <dd className="text-sm font-mono text-foreground">
                    {formatDuration(new Date(job.updatedAt).getTime() - new Date(job.createdAt).getTime())}
                  </dd>
                </div>
              )}
            </dl>
          </div>

          {/* Safety Decision */}
          <div className={cn(
            "instrument-card",
            job.safetyDecision?.type === "deny" && "status-danger",
          )}>
            <div className="flex items-center gap-2 mb-3">
              <Shield className="w-4 h-4 text-cordum" />
              <h2 className="font-display font-semibold text-sm text-foreground">Safety Decision</h2>
            </div>
            {job.safetyDecision?.type && (
              <div className="mb-4">
                <StatusBadge
                  variant={
                    job.safetyDecision.type === "allow" ? "healthy" :
                    job.safetyDecision.type === "deny" ? "governance" :
                    "warning"
                  }
                >
                  {job.safetyDecision.type}
                </StatusBadge>
              </div>
            )}
            <dl className="grid grid-cols-[110px_1fr] gap-x-6 gap-y-3 items-baseline">
              {([
                ["Reason", job.safetyDecision?.reason],
                ["Rule ID", job.safetyDecision?.matchedRule],
                ["Risk Tags", (job.riskTags ?? []).join(", ") || undefined],
              ] as [string, string | undefined][])
                .filter(([, value]) => !!value)
                .map(([label, value]) => (
                  <div key={label} className="contents">
                    <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">{label}</dt>
                    <dd className="text-sm text-foreground font-mono truncate">{value}</dd>
                  </div>
                ))}
            </dl>
          </div>
        </div>

        {/* Labels — inline, no card wrapper */}
        {job.labels && Object.keys(job.labels).length > 0 && (
          <div className="flex flex-wrap gap-2">
            {Object.entries(job.labels).map(([k, v]) => (
              <span key={k} className="text-xs font-mono px-2 py-1 rounded-full bg-surface-2 border border-border text-foreground">
                <span className="text-muted-foreground">{k}:</span> {v}
              </span>
            ))}
          </div>
        )}

        {/* Error — collapsible, right under tags */}
        {(job.errorMessage || job.status === "failed") && (
          <CollapsibleSection
            title="Error"
            defaultOpen={job.status === "failed"}
            badge={<StatusBadge variant="danger">error</StatusBadge>}
          >
            <div className="rounded-2xl bg-destructive/5 border border-destructive/15 p-4">
              <p className="text-sm font-mono text-destructive whitespace-pre-wrap">{job.errorMessage || `Job failed (no error message provided). Status code: ${job.errorCode || "unknown"}`}</p>
              {job.errorCode && (
                <p className="text-xs text-muted-foreground mt-2 font-mono">
                  Code: {job.errorCode} {job.errorCodeEnum ? `(${job.errorCodeEnum})` : ""}
                </p>
              )}
            </div>
          </CollapsibleSection>
        )}
      </motion.div>

      {/* ── B. Safety Story — always visible ── */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3, delay: 0.05 }}
      >
        <div className="flex items-center gap-2 mb-3">
          <Shield className="w-4 h-4 text-cordum" />
          <h2 className="font-display font-semibold text-sm text-foreground">Safety Story</h2>
        </div>
        <div className="space-y-4">
          {/* Step 1: Input Evaluation */}
          <div className="instrument-card p-0 overflow-hidden">
            <div className="px-5 py-3 border-b border-border bg-surface-0 flex items-center gap-2">
              <div className="w-5 h-5 rounded-full bg-cordum/15 flex items-center justify-center text-xs font-mono font-bold text-cordum">1</div>
              <span className="text-xs font-mono font-medium text-foreground">Input Policy Evaluation</span>
              <StatusBadge variant={job.safetyDecision?.type === "deny" ? "governance" : job.safetyDecision?.type === "require_approval" ? "warning" : "healthy"}>
                {job.safetyDecision?.type ?? "no evaluation"}
              </StatusBadge>
            </div>
            <div className="p-5 space-y-3">
              <dl className="grid grid-cols-[120px_1fr] gap-x-6 gap-y-3 items-baseline">
                {([
                  ["Decision", job.safetyDecision?.type],
                  ["Reason", job.safetyDecision?.reason],
                  ["Matched Rule", job.safetyDecision?.matchedRule],
                  ["Eval Time", job.safetyDecision?.evalTimeMs ? `${job.safetyDecision.evalTimeMs}ms` : undefined],
                ] as [string, string | undefined][])
                  .filter(([, value]) => !!value)
                  .map(([label, value]) => (
                    <div key={label} className="contents">
                      <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">{label}</dt>
                      <dd className="text-sm font-mono text-foreground">{value}</dd>
                    </div>
                  ))}
              </dl>
              {/* Evaluation path */}
              {job.safetyDecision?.evalPath && job.safetyDecision.evalPath.length > 0 && (
                <div className="mt-3">
                  <p className="text-xs font-mono text-muted-foreground uppercase tracking-wider mb-2">Evaluation Path</p>
                  <div className="flex items-center gap-1 flex-wrap">
                    {job.safetyDecision.evalPath.map((step, stepIdx) => (
                      <span key={step} className="inline-flex items-center">
                        <span className="px-2 py-0.5 rounded bg-surface-1 border border-border text-xs font-mono text-foreground">{step}</span>
                        {stepIdx < (job.safetyDecision?.evalPath?.length ?? 0) - 1 && <span className="text-muted-foreground mx-1">&rarr;</span>}
                      </span>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* Step 2: Output Evaluation */}
          <div className={cn("instrument-card p-0 overflow-hidden", job.output_safety?.decision === "QUARANTINE" ? "status-danger" : "")}>
            <div className="px-5 py-3 border-b border-border bg-surface-0 flex items-center gap-2">
              <div className="w-5 h-5 rounded-full bg-[var(--color-info)]/15 flex items-center justify-center text-xs font-mono font-bold text-[var(--color-info)]">2</div>
              <span className="text-xs font-mono font-medium text-foreground">Output Policy Evaluation</span>
              {job.output_safety ? (
                <StatusBadge variant={job.output_safety.decision === "ALLOW" ? "healthy" : job.output_safety.decision === "REDACT" ? "warning" : "danger"}>
                  {job.output_safety.decision}
                </StatusBadge>
              ) : (
                <StatusBadge variant="muted">not evaluated</StatusBadge>
              )}
            </div>
            <div className="p-5 space-y-3">
              {job.output_safety ? (
                <>
                  <dl className="grid grid-cols-[120px_1fr] gap-x-6 gap-y-3 items-baseline">
                    <div className="contents">
                      <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Decision</dt>
                      <dd><StatusBadge variant={job.output_safety.decision === "ALLOW" ? "healthy" : "danger"}>{job.output_safety.decision}</StatusBadge></dd>
                    </div>
                    {job.output_safety.reason && (
                      <div className="contents">
                        <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Reason</dt>
                        <dd className="text-sm font-mono text-foreground">{job.output_safety.reason}</dd>
                      </div>
                    )}
                    {job.output_safety.rule_id && (
                      <div className="contents">
                        <dt className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Rule</dt>
                        <dd className="text-sm font-mono text-foreground">{job.output_safety.rule_id}</dd>
                      </div>
                    )}
                  </dl>
                  {Array.isArray(job.output_safety.findings) && job.output_safety.findings.length > 0 && (
                    <div className="mt-3 space-y-2">
                      <p className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Findings</p>
                      {job.output_safety.findings.map((f: OutputFinding) => (
                        <div key={`${f.type}-${f.scanner ?? ""}-${f.detail.slice(0, 40)}`} className="surface-inset p-3">
                          <div className="flex items-center gap-2 mb-1">
                            <StatusBadge variant={f.severity === "critical" ? "danger" : f.severity === "high" ? "warning" : "muted"}>{f.severity}</StatusBadge>
                            <span className="text-xs font-mono text-foreground">{f.type}</span>
                            {f.scanner && <span className="text-xs text-muted-foreground">via {f.scanner}</span>}
                          </div>
                          <p className="text-xs text-muted-foreground">{f.detail}</p>
                          {f.matched_pattern && <p className="text-xs font-mono text-destructive mt-1">Pattern: {f.matched_pattern}</p>}
                        </div>
                      ))}
                    </div>
                  )}
                  {/* Redaction preview */}
                  {job.output_safety.decision === "REDACT" && job.output_safety.redacted_ptr && (
                    <div className="mt-3">
                      <p className="text-xs font-mono text-muted-foreground uppercase tracking-wider mb-2">Redacted Output</p>
                      <BlobViewer
                        label="Redacted"
                        pointer={job.output_safety.redacted_ptr}
                        data={job.output_safety.redacted}
                        emptyText="Redacted content not yet resolved"
                      />
                    </div>
                  )}
                </>
              ) : (
                <p className="text-xs text-muted-foreground">No output policy evaluation was performed for this job.</p>
              )}
            </div>
          </div>
        </div>
      </motion.div>

      {/* ── C. Timeline — always visible ── */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3, delay: 0.1 }}
        className="instrument-card"
      >
        <div className="flex items-center gap-2 mb-4">
          <Clock className="w-4 h-4 text-cordum" />
          <h2 className="font-display font-semibold text-sm text-foreground">Event Timeline</h2>
        </div>
        <JobTimeline job={job} />
      </motion.div>

      {/* ── E. Collapsed drawers ── */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3, delay: 0.2 }}
        className="space-y-4"
      >
        <CollapsibleSection title="Context payload" defaultOpen={false}>
          <div className="instrument-card">
            <BlobViewer label="Context" pointer={job.contextPtr} data={job.context} emptyText="No context data available" />
          </div>
        </CollapsibleSection>

        <CollapsibleSection title="Raw output" defaultOpen={false}>
          <div className="space-y-4">
            <div className="instrument-card">
              <BlobViewer
                label="Result"
                pointer={job.resultPtr}
                data={job.result}
                emptyText={
                  ["running", "pending", "dispatched"].includes(job.status)
                    ? "Job is still running\u2026"
                    : "No result data available"
                }
              />
            </div>
            <div className="instrument-card">
              <div className="surface-inset p-4 font-mono text-xs text-foreground min-h-[200px] max-h-[500px] overflow-auto">
                <JobTerminal job={job} />
              </div>
            </div>
          </div>
        </CollapsibleSection>
      </motion.div>

      {/* Cancel/retry/remediate dialogs are handled by JobActions */}
    </div>
  );
}
