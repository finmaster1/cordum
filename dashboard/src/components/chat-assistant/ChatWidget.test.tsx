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

describe("ChatWidget — informational-only affordances", () => {
  it("renders the persistent informational-only disclaimer when the panel is open", async () => {
    act(() => {
      useChatAssistantStore.getState().openPanel();
    });
    renderWithProviders(<ChatWidget />);
    const note = await screen.findByRole("note", { name: /advisory disclaimer/i });
    expect(note.textContent).toMatch(/informational only/i);
    expect(note.textContent).toMatch(/does not execute actions/i);
    expect(note.textContent).toMatch(/use the dashboard or CLI/i);
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

// Probe 11 (task-f2507515) — locks in worker-eeb9's responsive overlay
// (commit 9796d177) so future class-list edits cannot silently regress
// mobile fullscreen layout, and adds WCAG 2.5.5 enhanced 44px touch
// targets for the close + send buttons at the ≤sm breakpoint.
describe("ChatWidget — responsive layout + WCAG 2.5.5 touch targets", () => {
  it("root renders mobile-fullscreen overlay classes that flip to docked panel at sm", async () => {
    act(() => {
      useChatAssistantStore.getState().openPanel();
    });
    renderWithProviders(<ChatWidget />);
    const root = await screen.findByRole("complementary", { name: /cordum chat assistant/i });
    // Literal-substring assertions catch future drift to md:/lg: variants
    // that would skip tablet portrait at 600-768px.
    for (const cls of ["inset-0", "max-w-none", "rounded-none", "sm:inset-auto", "sm:rounded-2xl"]) {
      expect(root.className).toContain(cls);
    }
  });

  it("close + send buttons expand to 44px at mobile and revert at sm", async () => {
    act(() => {
      useChatAssistantStore.getState().openPanel();
    });
    renderWithProviders(<ChatWidget />);
    const closeButton = await screen.findByRole("button", { name: /close chat/i });
    for (const cls of ["h-11", "w-11", "sm:h-7", "sm:w-7"]) {
      expect(closeButton.className).toContain(cls);
    }
    const sendButton = screen.getByRole("button", { name: /send message/i });
    for (const cls of ["h-11", "w-11", "sm:h-9", "sm:w-9"]) {
      expect(sendButton.className).toContain(cls);
    }
  });
});
