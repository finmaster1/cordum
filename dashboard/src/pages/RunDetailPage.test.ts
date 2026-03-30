import { describe, it, expect } from "vitest";
import { isRunVisibilityActive, isRunVisibilityTerminal, toRunVisibilityState } from "../lib/runVisibility";

/**
 * Tests for RunDetailPage logic: skipped step detection, live vs historical indicator.
 */

function isRunning(status: string): boolean {
  return isRunVisibilityActive(status);
}

function isTerminal(status: string): boolean {
  return isRunVisibilityTerminal(status);
}

function stepClasses(status: string): string[] {
  const classes: string[] = [];
  if (status === "pending") classes.push("text-muted-foreground");
  if (status === "skipped") classes.push("text-muted-foreground", "line-through", "opacity-50");
  if (status !== "pending" && status !== "skipped") classes.push("text-foreground");
  return classes;
}

describe("Skipped step rendering", () => {
  it("applies line-through and opacity for skipped steps", () => {
    const classes = stepClasses("skipped");
    expect(classes).toContain("line-through");
    expect(classes).toContain("opacity-50");
    expect(classes).toContain("text-muted-foreground");
  });

  it("does not apply line-through for pending steps", () => {
    const classes = stepClasses("pending");
    expect(classes).not.toContain("line-through");
    expect(classes).toContain("text-muted-foreground");
  });

  it("does not apply line-through for succeeded steps", () => {
    const classes = stepClasses("succeeded");
    expect(classes).not.toContain("line-through");
    expect(classes).toContain("text-foreground");
  });
});

describe("Live vs historical indicator", () => {
  it("running is live", () => {
    expect(isRunning("running")).toBe(true);
    expect(isTerminal("running")).toBe(false);
  });

  it("pending/queued are live", () => {
    expect(isRunning("pending")).toBe(true);
    expect(isRunning("queued")).toBe(true);
    expect(toRunVisibilityState("pending")).toBe("queued");
  });

  it("succeeded/completed are terminal (historical)", () => {
    expect(isRunning("succeeded")).toBe(false);
    expect(isTerminal("succeeded")).toBe(true);
    expect(isTerminal("completed")).toBe(true);
    expect(toRunVisibilityState("succeeded")).toBe("completed");
  });

  it("failed is terminal", () => {
    expect(isTerminal("failed")).toBe(true);
  });

  it("cancelled is terminal", () => {
    expect(isTerminal("cancelled")).toBe(true);
  });

  it("timed_out is terminal", () => {
    expect(isTerminal("timed_out")).toBe(true);
  });

  it("denied/blocked are terminal governance outcomes", () => {
    expect(isTerminal("denied")).toBe(true);
    expect(isTerminal("blocked")).toBe(true);
    expect(isRunning("denied")).toBe(false);
    expect(toRunVisibilityState("denied")).toBe("blocked");
  });

  it("waiting maps to blocked (not live)", () => {
    expect(isRunning("waiting")).toBe(false);
    expect(isTerminal("waiting")).toBe(true);
    expect(toRunVisibilityState("waiting")).toBe("blocked");
  });
});

// ---------------------------------------------------------------------------
// mapStepStatus — waiting must NOT collapse to running
// ---------------------------------------------------------------------------

function mapStepStatus(status?: string): string {
  switch (status) {
    case "completed": return "succeeded";
    case "succeeded": return "succeeded";
    case "queued": return "pending";
    case "running": return "running";
    case "waiting": return "waiting";
    case "quarantined":
    case "output_quarantined": return "quarantined";
    case "denied":
    case "blocked":
    case "failed":
    case "timed_out": return "failed";
    case "cancelled": return "skipped";
    default: return "pending";
  }
}

describe("mapStepStatus — approval-waiting preservation", () => {
  it("preserves waiting as distinct from running", () => {
    expect(mapStepStatus("waiting")).toBe("waiting");
    expect(mapStepStatus("running")).toBe("running");
    expect(mapStepStatus("waiting")).not.toBe("running");
  });

  it("maps succeeded, failed, cancelled correctly", () => {
    expect(mapStepStatus("succeeded")).toBe("succeeded");
    expect(mapStepStatus("failed")).toBe("failed");
    expect(mapStepStatus("denied")).toBe("failed");
    expect(mapStepStatus("blocked")).toBe("failed");
    expect(mapStepStatus("cancelled")).toBe("skipped");
    expect(mapStepStatus("timed_out")).toBe("failed");
  });

  it("maps queued/completed aliases", () => {
    expect(mapStepStatus("queued")).toBe("pending");
    expect(mapStepStatus("completed")).toBe("succeeded");
  });

  it("maps quarantined states", () => {
    expect(mapStepStatus("quarantined")).toBe("quarantined");
    expect(mapStepStatus("output_quarantined")).toBe("quarantined");
  });

  it("defaults to pending for unknown", () => {
    expect(mapStepStatus(undefined)).toBe("pending");
    expect(mapStepStatus("")).toBe("pending");
  });
});
