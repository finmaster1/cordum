import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

// FEATURE_FLAGS reads import.meta.env at module load — by the time tests
// run the values are frozen, so each test forces the flag via this mock.
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
import { screen } from "@testing-library/react";
import { act } from "@testing-library/react";
import { ChatWidget } from "./ChatWidget";
import { resetChatAssistantStore, useChatAssistantStore } from "@/state/chatAssistant";
import { useConfigStore } from "@/state/config";

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

describe("ChatWidget — overreliance affordances", () => {
  it("renders the persistent advisory disclaimer when the panel is open", async () => {
    act(() => {
      useChatAssistantStore.getState().openPanel();
    });
    renderWithProviders(<ChatWidget />);
    const note = await screen.findByRole("note", { name: /advisory disclaimer/i });
    expect(note.textContent).toMatch(/assistant suggestions are advisory/i);
    expect(note.textContent).toMatch(/approval gate and audit chain/i);
    expect(note.textContent).toMatch(/verify args before approving/i);
  });

  it("does not render the disclaimer when the panel is closed", () => {
    renderWithProviders(<ChatWidget />);
    expect(screen.queryByRole("note", { name: /advisory disclaimer/i })).toBeNull();
  });

  it("does not mount the disclaimer when availability is false (vLLM down)", async () => {
    server.use(
      http.get("*/api/v1/chat/healthz", () =>
        HttpResponse.json({ vllm: "fail: down", redis: "ok" }, { status: 503 }),
      ),
    );
    act(() => {
      useChatAssistantStore.getState().openPanel();
    });
    renderWithProviders(<ChatWidget />);
    // Allow useLicense + healthz polling to settle before asserting absence.
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole("note", { name: /advisory disclaimer/i })).toBeNull();
  });
});
