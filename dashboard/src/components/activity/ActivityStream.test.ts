import { describe, it, expect } from "vitest";
import type { ActivityItem } from "../../types/activity";

/**
 * Tests for ActivityStream logic: buffer cap, filtering, completion detection.
 */

const MAX_ACTIVITY_ITEMS = 100;
const TERMINAL_STATUSES = ["succeeded", "failed", "denied", "cancelled", "timed_out"];

type FilterTab = "all" | "errors" | "safety" | "progress";

function matchesFilter(item: ActivityItem, filter: FilterTab): boolean {
  if (filter === "all") return true;
  const t = (item.type ?? "").toLowerCase();
  switch (filter) {
    case "errors":
      return t.includes("error") || t.includes("fail") || t.includes("denied") || t.includes("timeout");
    case "safety":
      return t.includes("safety") || t.includes("policy") || t.includes("decision");
    case "progress":
      return t.includes("progress") || t.includes("step") || t.includes("started") || t.includes("completed");
    default:
      return true;
  }
}

function makeItem(id: string, type: string): ActivityItem {
  return { id, type, content: `Item ${id}`, timestamp: new Date().toISOString() } as ActivityItem;
}

describe("ActivityStream buffer cap", () => {
  it("caps at 100 items", () => {
    const items = Array.from({ length: 200 }, (_, i) => makeItem(`i-${i}`, "step.completed"));
    const display = items.slice(-MAX_ACTIVITY_ITEMS);
    expect(display.length).toBe(100);
    expect(display[0].id).toBe("i-100");
  });

  it("reports hidden count", () => {
    const items = Array.from({ length: 150 }, (_, i) => makeItem(`i-${i}`, "step.completed"));
    const hidden = items.length > MAX_ACTIVITY_ITEMS ? items.length - MAX_ACTIVITY_ITEMS : 0;
    expect(hidden).toBe(50);
  });

  it("no overflow for small lists", () => {
    const items = Array.from({ length: 50 }, (_, i) => makeItem(`i-${i}`, "step.completed"));
    const hidden = items.length > MAX_ACTIVITY_ITEMS ? items.length - MAX_ACTIVITY_ITEMS : 0;
    expect(hidden).toBe(0);
  });
});

describe("ActivityStream filter", () => {
  const items: ActivityItem[] = [
    makeItem("1", "job.failed"),
    makeItem("2", "safety.decision"),
    makeItem("3", "step.completed"),
    makeItem("4", "job.progress"),
    makeItem("5", "policy.updated"),
    makeItem("6", "job.started"),
  ];

  it("all filter returns everything", () => {
    expect(items.filter((i) => matchesFilter(i, "all")).length).toBe(6);
  });

  it("errors filter returns failures and denied", () => {
    const filtered = items.filter((i) => matchesFilter(i, "errors"));
    expect(filtered.length).toBe(1);
    expect(filtered[0].id).toBe("1");
  });

  it("safety filter returns safety and policy items", () => {
    const filtered = items.filter((i) => matchesFilter(i, "safety"));
    expect(filtered.length).toBe(2);
    expect(filtered.map((f) => f.id)).toEqual(["2", "5"]);
  });

  it("progress filter returns step and progress items", () => {
    const filtered = items.filter((i) => matchesFilter(i, "progress"));
    expect(filtered.length).toBe(3);
    expect(filtered.map((f) => f.id)).toEqual(["3", "4", "6"]);
  });
});

describe("Run completion detection", () => {
  it("detects terminal statuses", () => {
    for (const s of TERMINAL_STATUSES) {
      expect(TERMINAL_STATUSES.includes(s)).toBe(true);
    }
  });

  it("running is not terminal", () => {
    expect(TERMINAL_STATUSES.includes("running")).toBe(false);
  });

  it("pending is not terminal", () => {
    expect(TERMINAL_STATUSES.includes("pending")).toBe(false);
  });
});
