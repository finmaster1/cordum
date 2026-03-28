import { Link } from "react-router-dom";
import { ExternalLink } from "lucide-react";
import { cn } from "../../lib/utils";
import type { ApprovalWorkflowContext } from "../../api/types";

function StepDot({
  state,
  label,
}: {
  state: "completed" | "current" | "pending";
  label?: string;
}) {
  return (
    <div className="flex flex-col items-center gap-1">
      <div
        className={cn(
          "h-2.5 w-2.5 rounded-full",
          state === "completed" && "bg-[var(--color-success)]",
          state === "current" && "animate-pulse bg-[var(--color-warning)]",
          state === "pending" && "bg-muted",
        )}
      />
      {label && (
        <span className="max-w-[72px] truncate text-center text-xs text-muted-foreground">
          {label}
        </span>
      )}
    </div>
  );
}

function StepLine({ completed }: { completed: boolean }) {
  return (
    <div
      className={cn(
        "h-0.5 min-w-3 flex-1",
        completed ? "bg-[var(--color-success)]" : "bg-muted",
      )}
    />
  );
}

interface WorkflowContextProps {
  workflowContext?: ApprovalWorkflowContext;
  nextEffect?: string;
  rejectEffect?: string;
}

export function WorkflowContext({
  workflowContext,
  nextEffect,
  rejectEffect,
}: WorkflowContextProps) {
  if (!workflowContext) {
    return (
      <p className="text-sm text-muted-foreground">
        This approval is not attached to a workflow run. Review the decision
        summary and audit details below.
      </p>
    );
  }

  const {
    workflowId,
    workflowName,
    runId,
    stepId,
    stepIndex,
    stepName,
    totalSteps,
  } = workflowContext;
  const hasStepInfo = stepIndex != null && totalSteps != null;
  const workflowLabel = workflowName?.trim() || workflowId;
  const stepLabel = stepName || stepId || "Current approval step";
  const approveMessage =
    nextEffect ||
    (hasStepInfo && stepIndex + 1 < totalSteps
      ? `Approve to continue to step ${stepIndex + 2} of ${totalSteps}.`
      : "Approve to continue the workflow.");
  const rejectMessage =
    rejectEffect || "Reject to stop this workflow path and preserve the audit trail.";

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="space-y-1">
          <p className="text-xs font-mono uppercase tracking-wide text-muted-foreground">
            Workflow
          </p>
          {workflowId ? (
            <Link
              to={`/workflows/${workflowId}/studio`}
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              {workflowLabel}
              <ExternalLink className="h-3 w-3" />
            </Link>
          ) : (
            <p className="text-sm font-medium text-foreground">{workflowLabel}</p>
          )}
        </div>

        <div className="space-y-1">
          <p className="text-xs font-mono uppercase tracking-wide text-muted-foreground">
            Run
          </p>
          {workflowId && runId ? (
            <Link
              to={`/workflows/${workflowId}/studio?run=${runId}`}
              className="font-mono text-sm text-accent hover:underline"
            >
              {runId}
            </Link>
          ) : (
            <p className="font-mono text-sm text-foreground">{runId || "—"}</p>
          )}
        </div>
      </div>

      <div className="rounded-2xl border border-border bg-surface-1/70 p-3">
        <p className="text-xs font-mono uppercase tracking-wide text-muted-foreground">
          Approval step
        </p>
        <p className="mt-1 text-sm font-medium text-foreground">{stepLabel}</p>
        {hasStepInfo && (
          <p className="mt-1 text-xs text-muted-foreground">
            Step {stepIndex + 1} of {totalSteps}
          </p>
        )}
      </div>

      {hasStepInfo && (
        <div className="flex items-center gap-0.5 py-1">
          {Array.from({ length: totalSteps }, (_, i) => {
            const state =
              i < stepIndex ? "completed" : i === stepIndex ? "current" : "pending";
            return (
              <div key={i} className="contents">
                {i > 0 && <StepLine completed={i <= stepIndex} />}
                <StepDot
                  state={state}
                  label={i === stepIndex ? stepLabel : undefined}
                />
              </div>
            );
          })}
        </div>
      )}

      <div className="space-y-2 text-xs text-muted-foreground">
        <p>{approveMessage}</p>
        <p>{rejectMessage}</p>
      </div>
    </div>
  );
}
