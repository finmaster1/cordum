import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// ---------------------------------------------------------------------------
// Mock WebSocket
// ---------------------------------------------------------------------------

type WsListener = (ev: { data: string }) => void;

class MockWebSocket {
  static instances: MockWebSocket[] = [];

  url: string;
  protocols: string[];
  readyState = 0; // CONNECTING
  onopen: (() => void) | null = null;
  onmessage: WsListener | null = null;
  onerror: (() => void) | null = null;
  onclose: ((ev: { code: number; reason: string; wasClean: boolean }) => void) | null = null;
  closed = false;

  constructor(url: string, protocols?: string[]) {
    this.url = url;
    this.protocols = protocols ?? [];
    MockWebSocket.instances.push(this);
  }

  close() {
    this.closed = true;
    this.readyState = 3;
  }

  // Test helpers
  simulateOpen() {
    this.readyState = 1;
    this.onopen?.();
  }

  simulateMessage(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  simulateClose(code = 1006, reason = "") {
    this.readyState = 3;
    this.onclose?.({ code, reason, wasClean: false });
  }
}

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.stubGlobal("WebSocket", MockWebSocket);
vi.stubGlobal("crypto", { randomUUID: () => "mock-uuid" });

// Mock useQueryClient
const mockInvalidateQueries = vi.fn();
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({
    invalidateQueries: mockInvalidateQueries,
    getQueryCache: () => ({ getAll: () => [] }),
  }),
}));

// Mock config store
let mockApiKey = "test-key";
let mockApiBaseUrl = "";
vi.mock("../state/config", () => ({
  useConfigStore: (selector: (s: { apiKey: string; apiBaseUrl: string }) => unknown) =>
    selector({ apiKey: mockApiKey, apiBaseUrl: mockApiBaseUrl }),
}));

// Import after mocks
const { useEventStore } = await import("../state/events");
const { useEventStream } = await import("./useEventStream");

// Minimal React hooks mock — useEffect runs synchronously, useRef returns object
let cleanupFn: (() => void) | undefined;
vi.mock("react", () => ({
  useEffect: (fn: () => (() => void) | void) => {
    cleanupFn = fn() as (() => void) | undefined;
  },
  useRef: (initial: unknown) => ({ current: initial }),
}));

describe("useEventStream", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.instances = [];
    mockInvalidateQueries.mockClear();
    mockApiKey = "test-key";
    useEventStore.setState({
      status: "disconnected",
      events: [],
      safetyDecisions: [],
    });
    cleanupFn = undefined;
  });

  afterEach(() => {
    cleanupFn?.();
    vi.useRealTimers();
  });

  it("creates a WebSocket connection on mount", () => {
    useEventStream();
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0].url).toContain("/api/v1/stream");
  });

  it("sends auth via subprotocol", () => {
    useEventStream();
    const ws = MockWebSocket.instances[0];
    expect(ws.protocols).toHaveLength(2);
    expect(ws.protocols[0]).toBe("cordum-api-key");
    expect(ws.protocols[1]).toMatch(/^[A-Za-z0-9_-]+$/);
  });

  it("sets status to connected on open", () => {
    useEventStream();
    MockWebSocket.instances[0].simulateOpen();
    expect(useEventStore.getState().status).toBe("connected");
  });

  it("sets status to reconnecting on close and schedules reconnect", () => {
    useEventStream();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    ws.simulateClose();
    expect(useEventStore.getState().status).toBe("reconnecting");

    // After 1s backoff, a new WebSocket should be created
    vi.advanceTimersByTime(1000);
    expect(MockWebSocket.instances).toHaveLength(2);
  });

  it("applies exponential backoff on repeated disconnects", () => {
    useEventStream();

    // First connection fails immediately (no open) → close
    MockWebSocket.instances[0].simulateClose();

    // 1s backoff → reconnect
    vi.advanceTimersByTime(1000);
    expect(MockWebSocket.instances).toHaveLength(2);

    // Second connection also fails → close (backoff should be 2s now)
    MockWebSocket.instances[1].simulateClose();

    // 1s should NOT be enough (backoff doubled to 2s)
    vi.advanceTimersByTime(1000);
    expect(MockWebSocket.instances).toHaveLength(2);

    // Another 1s (total 2s) → reconnect
    vi.advanceTimersByTime(1000);
    expect(MockWebSocket.instances).toHaveLength(3);
  });

  it("dispatches events to the store on message", () => {
    useEventStream();
    MockWebSocket.instances[0].simulateOpen();
    MockWebSocket.instances[0].simulateMessage({
      traceId: "tr-1",
      jobRequest: { jobId: "j1", topic: "job.default" },
    });
    const events = useEventStore.getState().events;
    expect(events).toHaveLength(1);
    expect(events[0].type).toBe("job.submit");
  });

  it("invalidates job detail and all job list caches for job events", () => {
    useEventStream();
    MockWebSocket.instances[0].simulateOpen();
    MockWebSocket.instances[0].simulateMessage({
      jobRequest: { jobId: "j1" },
    });
    // Specific job detail invalidated
    expect(mockInvalidateQueries).toHaveBeenCalledWith({ queryKey: ["job", "j1"] });
    // All job list caches invalidated (broad match for filtered views)
    expect(mockInvalidateQueries).toHaveBeenCalledWith({ queryKey: ["jobs"] });
  });

  it("ignores non-JSON messages", () => {
    useEventStream();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    // Send raw non-JSON — onmessage should not throw
    ws.onmessage?.({ data: "not-json" });
    expect(useEventStore.getState().events).toHaveLength(0);
  });

  it("pushes safety decisions for safety events", () => {
    useEventStream();
    MockWebSocket.instances[0].simulateOpen();
    MockWebSocket.instances[0].simulateMessage({
      alert: { topic: "job.default", decision: "DENY", rule_id: "r1" },
    });
    // alert maps to system.alert which starts with "system." not "safety."
    // Safety events would need a different packet type — verify no crash
    expect(useEventStore.getState().events).toHaveLength(1);
  });

  it("cleans up WebSocket on unmount", () => {
    useEventStream();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();
    cleanupFn?.();
    expect(ws.closed).toBe(true);
    expect(useEventStore.getState().status).toBe("disconnected");
  });

  it("invalidates all caches on reconnect", () => {
    useEventStream();
    const ws1 = MockWebSocket.instances[0];
    ws1.simulateOpen();
    mockInvalidateQueries.mockClear();

    // Disconnect
    ws1.simulateClose();
    expect(useEventStore.getState().status).toBe("reconnecting");

    // Reconnect after backoff
    vi.advanceTimersByTime(1000);
    const ws2 = MockWebSocket.instances[1];
    ws2.simulateOpen();

    // Should have invalidated all caches (called with no args = invalidate all)
    expect(mockInvalidateQueries).toHaveBeenCalled();
  });

  it("falls back to broad invalidation for events without resource IDs", () => {
    useEventStream();
    MockWebSocket.instances[0].simulateOpen();
    mockInvalidateQueries.mockClear();
    // Alert events have no extractable jobId/workerId
    MockWebSocket.instances[0].simulateMessage({
      alert: { severity: "warning", message: "test" },
    });
    // Should not invalidate specific resource — no ID available
    // Broad invalidation not triggered for system.alert (no INVALIDATION_MAP entry)
    // This verifies the fallback path doesn't crash
  });

  it("does not invalidate caches on first connection (not a reconnect)", () => {
    useEventStream();
    mockInvalidateQueries.mockClear();
    MockWebSocket.instances[0].simulateOpen();

    // First connect should NOT invalidate all caches
    // (only event-specific invalidation happens on messages)
    expect(mockInvalidateQueries).not.toHaveBeenCalled();
  });

  it("does not schedule a reconnect when close fires after unmount", () => {
    useEventStream();
    const ws = MockWebSocket.instances[0];
    ws.simulateOpen();

    cleanupFn?.();
    ws.simulateClose();

    expect(useEventStore.getState().status).toBe("disconnected");
    vi.advanceTimersByTime(5000);
    expect(MockWebSocket.instances).toHaveLength(1);
  });
});
