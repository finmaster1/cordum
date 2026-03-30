import { describe, expect, it } from "vitest";
import {
  isRunVisibilityActive,
  isRunVisibilityTerminal,
  isTerminalRunStatus,
  normalizeRunStatusValue,
  toRunVisibilityState,
} from "./runVisibility";

describe("normalizeRunStatusValue", () => {
  it("normalizes lifecycle aliases to canonical run statuses", () => {
    expect(normalizeRunStatusValue("queued")).toBe("pending");
    expect(normalizeRunStatusValue("completed")).toBe("succeeded");
    expect(normalizeRunStatusValue("blocked")).toBe("denied");
    expect(normalizeRunStatusValue("error")).toBe("failed");
    expect(normalizeRunStatusValue("timeout")).toBe("timed_out");
    expect(normalizeRunStatusValue("canceled")).toBe("cancelled");
  });

  it("returns undefined for unknown values", () => {
    expect(normalizeRunStatusValue("mystery")).toBeUndefined();
    expect(normalizeRunStatusValue(undefined)).toBeUndefined();
  });
});

describe("toRunVisibilityState", () => {
  it("maps canonical statuses to queued/running/completed/failed/blocked", () => {
    expect(toRunVisibilityState("pending")).toBe("queued");
    expect(toRunVisibilityState("running")).toBe("running");
    expect(toRunVisibilityState("succeeded")).toBe("completed");
    expect(toRunVisibilityState("failed")).toBe("failed");
    expect(toRunVisibilityState("denied")).toBe("blocked");
  });

  it("maps lifecycle aliases directly", () => {
    expect(toRunVisibilityState("queued")).toBe("queued");
    expect(toRunVisibilityState("completed")).toBe("completed");
    expect(toRunVisibilityState("blocked")).toBe("blocked");
  });
});

describe("run visibility predicates", () => {
  it("marks only queued/running as active", () => {
    expect(isRunVisibilityActive("queued")).toBe(true);
    expect(isRunVisibilityActive("running")).toBe(true);
    expect(isRunVisibilityActive("blocked")).toBe(false);
    expect(isRunVisibilityActive("completed")).toBe(false);
  });

  it("marks completed/failed/blocked as terminal", () => {
    expect(isRunVisibilityTerminal("completed")).toBe(true);
    expect(isRunVisibilityTerminal("failed")).toBe(true);
    expect(isRunVisibilityTerminal("blocked")).toBe(true);
    expect(isRunVisibilityTerminal("running")).toBe(false);
  });

  it("keeps canonical terminal checks aligned with backend statuses", () => {
    expect(isTerminalRunStatus("succeeded")).toBe(true);
    expect(isTerminalRunStatus("denied")).toBe(true);
    expect(isTerminalRunStatus("completed")).toBe(true);
    expect(isTerminalRunStatus("blocked")).toBe(true);
    expect(isTerminalRunStatus("pending")).toBe(false);
  });
});
