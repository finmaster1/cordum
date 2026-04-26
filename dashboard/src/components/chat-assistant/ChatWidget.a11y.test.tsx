import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

const flagsMock = vi.hoisted(() => ({
  FEATURE_FLAGS: {
    governanceTimeline: false,
    governanceTimelineMocks: false,
    evalsPage: false,
    evalsPageMocks: false,
    delegationDashboard: false,
    llmChatAssistant: true,
  },
}));
vi.mock("@/config/flags", () => flagsMock);

import { http, HttpResponse, server } from "@/test-utils/msw";
import { renderWithProviders } from "@/test-utils/render";
import { act, screen, waitFor } from "@testing-library/react";
import { assertNoSeriousAxeViolations } from "@/test-utils/a11y";
import { ChatWidget } from "./ChatWidget";
import { ChatHeaderButton } from "./ChatHeaderButton";
import { ApprovalInlinePrompt } from "./ApprovalInlinePrompt";
import { resetChatAssistantStore, useChatAssistantStore } from "@/state/chatAssistant";
import { useConfigStore } from "@/state/config";
import type { AttachedToolCall } from "@/types/chatAssistant";

const enterpriseLicense = {
  plan: "enterprise",
  entitlements: { features: { llm_chat_assistant: true } },
  rights: null,
  license: null,
  expiry_status: "active",
};

beforeEach(() => {
  resetChatAssistantStore();
  useConfigStore.setState({ apiKey: "test-key" });
  flagsMock.FEATURE_FLAGS.llmChatAssistant = true;
  server.use(
    http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
    http.get("*/api/v1/chat/healthz", () => HttpResponse.json({}, { status: 200 })),
  );
});

afterEach(() => {
  resetChatAssistantStore();
});

describe("Chat assistant accessibility (axe-core, jsdom)", () => {
  it("has zero serious axe violations when the panel is closed (header button only)", async () => {
    const { container } = renderWithProviders(<ChatHeaderButton />);
    await screen.findByRole("button", { name: /open chat assistant/i });
    await assertNoSeriousAxeViolations(container);
  });

  it("has zero serious axe violations when the panel is open and empty", async () => {
    act(() => {
      useChatAssistantStore.getState().openPanel();
    });
    const { container } = renderWithProviders(<ChatWidget />);
    await screen.findByRole("complementary", { name: /cordum chat assistant/i });
    await assertNoSeriousAxeViolations(container);
  });

  it("has zero serious axe violations with a populated transcript", async () => {
    act(() => {
      const store = useChatAssistantStore.getState();
      store.openPanel();
      store.applyFrame({
        type: "user",
        id: "msg-user-1",
        text: "list my failing jobs",
      });
      store.applyFrame({
        type: "assistant_delta",
        id: "msg-assistant-1",
        delta: "Here are the failing jobs in the last 24h.",
      });
      store.applyFrame({
        type: "tool_call",
        id: "msg-assistant-1",
        toolCallId: "tc-1",
        tool: "cordum_list_jobs",
        args: { status: "FAILED", limit: 10 },
      });
      store.applyFrame({
        type: "tool_result",
        id: "msg-assistant-1",
        toolCallId: "tc-1",
        ok: true,
        resultPreview: '{"items":[{"id":"job-1","status":"FAILED"}]}',
      });
    });
    const { container } = renderWithProviders(<ChatWidget />);
    await waitFor(() => {
      expect(screen.queryByText(/list my failing jobs/i)).not.toBeNull();
    });
    await assertNoSeriousAxeViolations(container);
  });

  it("has zero serious axe violations when the approval-required inline prompt is rendered", async () => {
    const toolCall: AttachedToolCall = {
      toolCallId: "tc-approval-1",
      tool: "cordum_approve_job",
      args: { job_id: "job-abc", reason: "investigated" },
      approval: { approvalId: "appr-1", status: "pending" },
    };
    const { container } = renderWithProviders(<ApprovalInlinePrompt toolCall={toolCall} />);
    await screen.findByRole("region", { name: /approval required/i });
    await assertNoSeriousAxeViolations(container);
  });

  it("has zero serious axe violations on the chat widget in dark mode", async () => {
    act(() => {
      useChatAssistantStore.getState().openPanel();
    });
    const { container } = renderWithProviders(<ChatWidget />);
    await screen.findByRole("complementary", { name: /cordum chat assistant/i });
    await assertNoSeriousAxeViolations(container, { mode: "dark" });
  });
});
