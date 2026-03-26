import { useCallback, useState, type DragEvent } from "react";
import {
  ChevronLeft,
  ChevronRight,
  Clock,
  Search,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { formatDuration, formatRelativeTime } from "@/lib/utils";
import { StatusBadge, type BadgeVariant } from "@/components/ui/StatusBadge";
import type { Workflow, WorkflowRun } from "@/api/types";
import type { StudioMode } from "./types";
import { getGroupedStepTypes, getStepMeta } from "./nodeRegistry";

// ---------------------------------------------------------------------------
// Drag handler factory (shared for all palette items)
// ---------------------------------------------------------------------------

function handleDragStart(event: DragEvent, stepType: string) {
  event.dataTransfer.setData("application/workflow-studio", stepType);
  event.dataTransfer.effectAllowed = "move";
}

// ---------------------------------------------------------------------------
// Run status → badge variant
// ---------------------------------------------------------------------------

function runBadgeVariant(status?: string): BadgeVariant {
  switch (status) {
    case "succeeded": return "healthy";
    case "running": return "info";
    case "failed": return "danger";
    case "waiting": return "warning";
    default: return "muted";
  }
}

// ---------------------------------------------------------------------------
// Palette item (edit mode)
// ---------------------------------------------------------------------------

function PaletteItem({ type, label, iconColor }: { type: string; label: string; iconColor: string }) {
  const meta = getStepMeta(type);
  const Icon = meta.icon;

  return (
    <div
      draggable
      onDragStart={(e) => handleDragStart(e, type)}
      className="flex cursor-grab items-center gap-2.5 rounded-xl border border-border bg-card/60 px-3 py-2 text-xs font-medium text-ink shadow-sm transition-all hover:border-accent hover:shadow-soft active:cursor-grabbing"
    >
      <div className={cn("flex h-7 w-7 shrink-0 items-center justify-center rounded-lg", meta.accent)}>
        <Icon className={cn("h-3.5 w-3.5", iconColor)} />
      </div>
      <div className="min-w-0">
        <span className="block text-xs font-medium text-ink truncate">{label}</span>
        <span className="block text-[10px] text-muted-foreground truncate">{meta.description}</span>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Run list item (view mode)
// ---------------------------------------------------------------------------

function RunListItem({
  run,
  isSelected,
  onSelect,
}: {
  run: WorkflowRun;
  isSelected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "w-full text-left px-3 py-2 rounded-xl transition-colors",
        isSelected
          ? "bg-accent/10 border border-accent/30"
          : "hover:bg-surface-2 border border-transparent",
      )}
    >
      <div className="flex items-center justify-between">
        <span className="text-xs font-medium text-ink truncate">
          {run.id.slice(0, 8)}
        </span>
        <StatusBadge variant={runBadgeVariant(run.status)}>
          {run.status}
        </StatusBadge>
      </div>
      <div className="flex items-center gap-2 mt-1 text-[10px] text-muted-foreground">
        {run.startedAt && (
          <span>{formatRelativeTime(run.startedAt)}</span>
        )}
        {run.duration != null && (
          <>
            <span className="text-border">&middot;</span>
            <span>{formatDuration(run.duration)}</span>
          </>
        )}
      </div>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface StudioSidebarProps {
  mode: StudioMode;
  workflow: Workflow | null;
  runs: WorkflowRun[];
  selectedRunId: string | null;
  onSelectRun: (runId: string | null) => void;
}

// ---------------------------------------------------------------------------
// StudioSidebar
// ---------------------------------------------------------------------------

export function StudioSidebar({
  mode,
  workflow,
  runs,
  selectedRunId,
  onSelectRun,
}: StudioSidebarProps) {
  const [collapsed, setCollapsed] = useState(false);
  const [search, setSearch] = useState("");
  const isEdit = mode === "edit";

  const toggleCollapsed = useCallback(() => setCollapsed((v) => !v), []);

  // Filter palette items by search
  const groups = getGroupedStepTypes();
  const filteredGroups = search.trim()
    ? groups.map((g) => ({
        ...g,
        types: g.types.filter(
          (t) =>
            t.label.toLowerCase().includes(search.toLowerCase()) ||
            t.type.toLowerCase().includes(search.toLowerCase()),
        ),
      })).filter((g) => g.types.length > 0)
    : groups;

  if (collapsed) {
    return (
      <div className="w-10 border-r border-border bg-surface-0 flex flex-col items-center pt-3 shrink-0">
        <button
          type="button"
          onClick={toggleCollapsed}
          className="p-1.5 rounded-full hover:bg-surface-2 transition-colors"
          title="Expand sidebar"
        >
          <ChevronRight className="w-4 h-4 text-muted-foreground" />
        </button>
      </div>
    );
  }

  return (
    <aside className="w-60 border-r border-border bg-surface-0 flex flex-col shrink-0 overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2.5 border-b border-border shrink-0">
        <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
          {isEdit ? "Nodes" : "Workflow"}
        </span>
        <button
          type="button"
          onClick={toggleCollapsed}
          className="p-1 rounded hover:bg-surface-2 transition-colors"
          title="Collapse sidebar"
        >
          <ChevronLeft className="w-3.5 h-3.5 text-muted-foreground" />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-3 space-y-4">
        {isEdit ? (
          <>
            {/* Search */}
            <div className="relative">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
              <input
                type="text"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search nodes..."
                className="w-full h-8 pl-8 pr-3 text-xs bg-surface-2/50 border border-border rounded-xl text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-accent"
              />
            </div>

            {/* Node palette groups */}
            {filteredGroups.map((group) => (
              <section key={group.category}>
                <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground/60 mb-2">
                  {group.label}
                </h4>
                <div className="space-y-1.5">
                  {group.types.map((t) => (
                    <PaletteItem
                      key={t.type}
                      type={t.type}
                      label={t.label}
                      iconColor={t.iconColor}
                    />
                  ))}
                </div>
              </section>
            ))}

            {filteredGroups.length === 0 && (
              <p className="text-xs text-muted-foreground text-center py-4">
                No matching nodes
              </p>
            )}
          </>
        ) : (
          <>
            {/* Workflow info */}
            {workflow && (
              <section className="space-y-2">
                <div>
                  <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">Name</span>
                  <p className="text-xs text-ink font-medium">{workflow.name}</p>
                </div>
                {workflow.description && (
                  <div>
                    <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">Description</span>
                    <p className="text-xs text-muted-foreground">{workflow.description}</p>
                  </div>
                )}
                <div className="flex gap-3">
                  <div>
                    <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">Steps</span>
                    <p className="text-xs text-ink font-medium">{workflow.steps?.length ?? 0}</p>
                  </div>
                  {workflow.version && (
                    <div>
                      <span className="text-[10px] font-mono text-muted-foreground uppercase tracking-wider">Version</span>
                      <p className="text-xs text-ink font-medium">{workflow.version}</p>
                    </div>
                  )}
                </div>
              </section>
            )}

            {/* Run selector */}
            <section>
              <div className="flex items-center justify-between mb-2">
                <h4 className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
                  Runs
                </h4>
                {selectedRunId && (
                  <button
                    type="button"
                    onClick={() => onSelectRun(null)}
                    className="text-[10px] text-accent hover:underline"
                  >
                    Clear
                  </button>
                )}
              </div>

              {/* Blueprint mode hint */}
              {!selectedRunId && runs.length > 0 && (
                <p className="text-[10px] text-muted-foreground mb-2">
                  Select a run to overlay status on the diagram
                </p>
              )}

              <div className="space-y-1">
                {runs.map((r) => (
                  <RunListItem
                    key={r.id}
                    run={r}
                    isSelected={selectedRunId === r.id}
                    onSelect={() => onSelectRun(selectedRunId === r.id ? null : r.id)}
                  />
                ))}
                {runs.length === 0 && (
                  <div className="flex flex-col items-center py-6 text-muted-foreground">
                    <Clock className="w-8 h-8 mb-2 opacity-30" />
                    <p className="text-xs">No runs yet</p>
                  </div>
                )}
              </div>
            </section>
          </>
        )}
      </div>
    </aside>
  );
}
