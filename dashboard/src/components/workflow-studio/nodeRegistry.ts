import type { LucideIcon } from "lucide-react";
import {
  Briefcase,
  MessageSquare,
  Package,
  Wrench,
  UserCheck,
  Clock,
  GitBranch,
  Bell,
  GitFork,
  Layers,
  Globe,
  Code,
  GitMerge,
  Repeat,
  Workflow,
  AlertTriangle,
  Database,
  HelpCircle,
} from "lucide-react";
import type { RunStatus } from "@/api/types";

// ---------------------------------------------------------------------------
// Step type metadata
// ---------------------------------------------------------------------------

export interface StepTypeMeta {
  /** Display label */
  label: string;
  /** Short description for palette / tooltip */
  description: string;
  /** Lucide icon component */
  icon: LucideIcon;
  /** Accent colour class (Tailwind) used for the icon background */
  accent: string;
  /** Text colour class for the icon itself */
  iconColor: string;
  /** Category for sidebar grouping */
  category: "agent" | "flow" | "data" | "other";
  /** Whether the node hides the top input handle (e.g. error-trigger) */
  hideInput?: boolean;
}

const REGISTRY: Record<string, StepTypeMeta> = {
  job: {
    label: "Job",
    description: "Execute a worker job",
    icon: Briefcase,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "agent",
  },
  "agent-task": {
    label: "Agent Task",
    description: "Run an AI agent task",
    icon: MessageSquare,
    accent: "bg-[var(--color-info)]/10",
    iconColor: "text-[var(--color-info)]",
    category: "agent",
  },
  "pack-action": {
    label: "Pack Action",
    description: "Invoke a pack action",
    icon: Package,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "agent",
  },
  "tool-call": {
    label: "Tool Call",
    description: "Call an external tool",
    icon: Wrench,
    accent: "bg-[var(--color-warning)]/10",
    iconColor: "text-[var(--color-warning)]",
    category: "agent",
  },
  approval: {
    label: "Approval",
    description: "Human approval gate",
    icon: UserCheck,
    accent: "bg-[var(--color-warning)]/10",
    iconColor: "text-[var(--color-warning)]",
    category: "flow",
  },
  delay: {
    label: "Delay",
    description: "Wait for duration or time",
    icon: Clock,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "flow",
  },
  condition: {
    label: "Condition",
    description: "If/else branching",
    icon: GitBranch,
    accent: "bg-[var(--color-info)]/10",
    iconColor: "text-[var(--color-info)]",
    category: "flow",
  },
  notify: {
    label: "Notify",
    description: "Send a notification",
    icon: Bell,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "flow",
  },
  "fan-out": {
    label: "Fan-out",
    description: "Scatter to multiple targets",
    icon: GitFork,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "flow",
  },
  parallel: {
    label: "Parallel",
    description: "Run branches concurrently",
    icon: Layers,
    accent: "bg-[var(--color-info)]/10",
    iconColor: "text-[var(--color-info)]",
    category: "flow",
  },
  http: {
    label: "HTTP",
    description: "Make an HTTP request",
    icon: Globe,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "data",
  },
  transform: {
    label: "Transform",
    description: "Transform data with code",
    icon: Code,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "data",
  },
  switch: {
    label: "Switch",
    description: "Multi-way branching",
    icon: GitMerge,
    accent: "bg-[var(--color-info)]/10",
    iconColor: "text-[var(--color-info)]",
    category: "flow",
  },
  loop: {
    label: "Loop",
    description: "Iterate over a collection",
    icon: Repeat,
    accent: "bg-[var(--color-warning)]/10",
    iconColor: "text-[var(--color-warning)]",
    category: "flow",
  },
  "sub-workflow": {
    label: "Sub-flow",
    description: "Run a nested workflow",
    icon: Workflow,
    accent: "bg-[var(--color-info)]/10",
    iconColor: "text-[var(--color-info)]",
    category: "flow",
  },
  storage: {
    label: "Storage",
    description: "Read/write persistent data",
    icon: Database,
    accent: "bg-primary/10",
    iconColor: "text-primary",
    category: "data",
  },
  "error-trigger": {
    label: "Error Trigger",
    description: "Handle step errors",
    icon: AlertTriangle,
    accent: "bg-destructive/10",
    iconColor: "text-destructive",
    category: "flow",
    hideInput: true,
  },
};

const FALLBACK_META: StepTypeMeta = {
  label: "Unknown",
  description: "Unknown step type",
  icon: HelpCircle,
  accent: "bg-muted/10",
  iconColor: "text-muted-foreground",
  category: "other",
};

/**
 * Resolve metadata for any step type string.
 * Falls back gracefully for unrecognised types.
 */
export function getStepMeta(stepType: string): StepTypeMeta {
  return REGISTRY[stepType] ?? FALLBACK_META;
}

/**
 * All registered step types, grouped by category for the sidebar palette.
 */
export function getGroupedStepTypes(): { category: string; label: string; types: (StepTypeMeta & { type: string })[] }[] {
  const groups: Record<string, (StepTypeMeta & { type: string })[]> = {};
  for (const [type, meta] of Object.entries(REGISTRY)) {
    const cat = meta.category;
    if (!groups[cat]) groups[cat] = [];
    groups[cat].push({ ...meta, type });
  }

  const categoryLabels: Record<string, string> = {
    agent: "Agent & Actions",
    flow: "Flow Control",
    data: "Data & Integration",
    other: "Other",
  };

  return Object.entries(groups).map(([cat, types]) => ({
    category: cat,
    label: categoryLabels[cat] ?? cat,
    types,
  }));
}

/**
 * Palette types — the subset of types users can drag onto the canvas.
 * Excludes `job` (legacy alias for agent-task) and `storage`.
 */
export const PALETTE_TYPES = Object.keys(REGISTRY).filter(
  (t) => t !== "job" && t !== "storage",
);

// ---------------------------------------------------------------------------
// Run status helpers (shared between node rendering and edge styling)
// ---------------------------------------------------------------------------

export interface StatusVisual {
  bg: string;
  border: string;
  pulse: boolean;
  dimmed: boolean;
  strikethrough: boolean;
  label: string;
}

const STATUS_VISUALS: Record<string, StatusVisual> = {
  succeeded: {
    bg: "bg-[var(--color-success)]/5",
    border: "border-[var(--color-success)]/40",
    pulse: false,
    dimmed: false,
    strikethrough: false,
    label: "Succeeded",
  },
  running: {
    bg: "bg-[var(--color-info)]/5",
    border: "border-[var(--color-info)]/40",
    pulse: true,
    dimmed: false,
    strikethrough: false,
    label: "Running",
  },
  failed: {
    bg: "bg-destructive/5",
    border: "border-destructive/40",
    pulse: false,
    dimmed: false,
    strikethrough: false,
    label: "Failed",
  },
  pending: {
    bg: "bg-muted/30",
    border: "border-border",
    pulse: false,
    dimmed: true,
    strikethrough: false,
    label: "Pending",
  },
  waiting: {
    bg: "bg-[var(--color-warning)]/5",
    border: "border-[var(--color-warning)]/40",
    pulse: true,
    dimmed: false,
    strikethrough: false,
    label: "Waiting",
  },
  cancelled: {
    bg: "bg-muted/50",
    border: "border-muted",
    pulse: false,
    dimmed: false,
    strikethrough: true,
    label: "Cancelled",
  },
  timed_out: {
    bg: "bg-destructive/5",
    border: "border-destructive/30",
    pulse: false,
    dimmed: false,
    strikethrough: false,
    label: "Timed Out",
  },
};

const NEUTRAL_STATUS: StatusVisual = {
  bg: "bg-card",
  border: "border-border",
  pulse: false,
  dimmed: false,
  strikethrough: false,
  label: "",
};

export function getStatusVisual(status?: RunStatus | string): StatusVisual {
  if (!status) return NEUTRAL_STATUS;
  return STATUS_VISUALS[status] ?? NEUTRAL_STATUS;
}

// ---------------------------------------------------------------------------
// Safety decision badge config (reused in node + detail panel)
// ---------------------------------------------------------------------------

export interface SafetyBadgeConfig {
  label: string;
  className: string;
  glyph: string;
}

const SAFETY_BADGES: Record<string, SafetyBadgeConfig> = {
  allow: { label: "Allowed", className: "bg-[var(--color-success)] text-primary-foreground", glyph: "\u2713" },
  deny: { label: "Denied", className: "bg-destructive text-primary-foreground", glyph: "\u2717" },
  require_approval: { label: "Approval required", className: "bg-[var(--color-warning)] text-primary-foreground", glyph: "\u270B" },
  throttle: { label: "Throttled", className: "bg-[var(--color-info)] text-primary-foreground", glyph: "\u23F3" },
};

export function getSafetyBadge(decisionType?: string): SafetyBadgeConfig | null {
  if (!decisionType) return null;
  return SAFETY_BADGES[decisionType] ?? null;
}

// ---------------------------------------------------------------------------
// Text helpers
// ---------------------------------------------------------------------------

export function truncate(str: string, max: number): string {
  return str.length > max ? str.slice(0, max) + "\u2026" : str;
}

// ---------------------------------------------------------------------------
// Job-type check (shared predicate)
// ---------------------------------------------------------------------------

const JOB_TYPES = new Set(["job", "agent-task", "pack-action", "tool-call"]);

export function isJobType(stepType: string): boolean {
  return JOB_TYPES.has(stepType);
}
