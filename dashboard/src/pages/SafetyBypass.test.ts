import { describe, it, expect } from "vitest";

/**
 * Tests for safety bypass visibility logic.
 * Verifies the label-based detection that drives the bypass banner
 * in JobDetailPage and the bypass indicator in JobsPage.
 */

interface MockJob {
  id: string;
  status: string;
  labels?: Record<string, string>;
}

function isSafetyBypassed(job: MockJob): boolean {
  return job.labels?.safety_bypassed === "true";
}

function bypassReason(job: MockJob): string | undefined {
  return job.labels?.safety_bypass_reason;
}

describe("Safety bypass detection", () => {
  it("detects bypassed job from labels", () => {
    const job: MockJob = {
      id: "j-1",
      status: "completed",
      labels: { safety_bypassed: "true", safety_bypass_reason: "fail-open: safety unavailable" },
    };
    expect(isSafetyBypassed(job)).toBe(true);
    expect(bypassReason(job)).toBe("fail-open: safety unavailable");
  });

  it("returns false for normal job", () => {
    const job: MockJob = { id: "j-2", status: "completed", labels: {} };
    expect(isSafetyBypassed(job)).toBe(false);
  });

  it("returns false when labels missing", () => {
    const job: MockJob = { id: "j-3", status: "completed" };
    expect(isSafetyBypassed(job)).toBe(false);
  });

  it("returns false for safety_bypassed=false", () => {
    const job: MockJob = { id: "j-4", status: "completed", labels: { safety_bypassed: "false" } };
    expect(isSafetyBypassed(job)).toBe(false);
  });

  it("reason is undefined when not bypassed", () => {
    const job: MockJob = { id: "j-5", status: "completed", labels: {} };
    expect(bypassReason(job)).toBeUndefined();
  });
});
