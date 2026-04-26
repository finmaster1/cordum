import { http, HttpResponse } from "msw";

export const baseHandlers = [
  http.get("*/api/v1/approvals", () =>
    HttpResponse.json({ items: [], next_cursor: null }),
  ),
  http.get("*/api/v1/mcp/approvals", () =>
    HttpResponse.json({ items: [] }),
  ),
  http.get("*/api/v1/mcp/approvals/:id", ({ params }) =>
    HttpResponse.json({
      id: String(params.id),
      tenant: "default",
      agent_id: "agent-test",
      tool_name: "test.tool",
      args_hash: "hash-test",
      status: "pending",
      created_at: 0,
      expires_at: 0,
    }),
  ),
  http.get("*/api/v1/copilot/sessions/:sessionId", ({ params }) =>
    HttpResponse.json({
      session: {
        id: String(params.sessionId),
        title: "Test Copilot Session",
        userId: "test-user",
        createdAt: "2026-04-26T07:00:00Z",
        updatedAt: "2026-04-26T07:00:00Z",
        messages: [],
        metadata: {},
      },
      jobs: [],
      decisions: [],
      truncated: false,
    }),
  ),
  // Chat-assistant default handlers — the widget polls /chat/healthz on a
  // 10s timer and lists sessions on the admin page. Tests that need
  // available=true override these via server.use(...).
  http.get("*/api/v1/chat/healthz", () =>
    HttpResponse.json({ redis: "fail: unconfigured", vllm: "fail: unconfigured" }, { status: 503 }),
  ),
  http.get("*/api/v1/chat/sessions", () =>
    HttpResponse.json({ items: [], next_cursor: null }),
  ),
  http.get("*/api/v1/chat/sessions/:sessionId", ({ params }) =>
    HttpResponse.json({
      sessionId: String(params.sessionId),
      principal: "test-user",
      tenant: "default",
      createdAt: "2026-04-26T07:00:00Z",
      lastActiveAt: "2026-04-26T07:00:00Z",
      messageCount: 0,
      messages: [],
    }),
  ),
];
