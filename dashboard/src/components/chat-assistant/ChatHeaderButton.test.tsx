import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

// FEATURE_FLAGS reads import.meta.env at module load — by the time tests
// run the values are frozen, so each test toggles the flag via this mock.
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
import { fireEvent, screen, waitFor, act } from "@testing-library/react";
import { ChatHeaderButton } from "./ChatHeaderButton";
import { resetChatAssistantStore, useChatAssistantStore } from "@/state/chatAssistant";
import { useConfigStore } from "@/state/config";

const enterpriseLicense = {
  plan: "enterprise",
  entitlements: { features: { llm_chat_assistant: true } },
  rights: null,
  license: null,
  expiry_status: "active",
};

const communityLicense = {
  plan: "community",
  entitlements: { features: { llm_chat_assistant: false } },
  rights: null,
  license: null,
  expiry_status: "active",
};

beforeEach(() => {
  resetChatAssistantStore();
  useConfigStore.setState({ apiKey: "test-key" });
  flagsMock.FEATURE_FLAGS.llmChatAssistant = true;
});

afterEach(() => {
  resetChatAssistantStore();
});

describe("ChatHeaderButton", () => {
  it("renders nothing when llm_chat_assistant feature flag is off", async () => {
    flagsMock.FEATURE_FLAGS.llmChatAssistant = false;
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () => HttpResponse.json({}, { status: 200 })),
    );
    renderWithProviders(<ChatHeaderButton />);
    // Allow useLicense + healthz polling to settle.
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole("button", { name: /chat assistant/i })).toBeNull();
  });

  it("renders nothing for unentitled (Community) license", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(communityLicense)),
      http.get("*/api/v1/chat/healthz", () => HttpResponse.json({}, { status: 200 })),
    );
    renderWithProviders(<ChatHeaderButton />);
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole("button", { name: /chat assistant/i })).toBeNull();
  });

  it("renders the icon button when entitled and healthz returns 200", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () => HttpResponse.json({}, { status: 200 })),
    );
    renderWithProviders(<ChatHeaderButton />);
    const btn = await screen.findByRole("button", { name: /open chat assistant/i });
    expect(btn).toBeTruthy();
  });

  it("hides the button when healthz returns 503", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () =>
        HttpResponse.json({ vllm: "fail: down", redis: "ok" }, { status: 503 }),
      ),
    );
    renderWithProviders(<ChatHeaderButton />);
    await new Promise((r) => setTimeout(r, 30));
    expect(screen.queryByRole("button", { name: /chat assistant/i })).toBeNull();
  });

  it("toggles the panel via store on click and shows badge for pending approvals", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () => HttpResponse.json({}, { status: 200 })),
    );
    act(() => {
      useChatAssistantStore.getState().applyFrame({
        type: "approval_required",
        id: "msg-1",
        toolCallId: "tc-1",
        approvalId: "appr-1",
        tool: "cordum_approve_job",
        args: {},
      });
    });
    renderWithProviders(<ChatHeaderButton />);
    const btn = await screen.findByRole("button", { name: /open chat assistant/i });
    expect(btn.textContent).toContain("1");
    fireEvent.click(btn);
    await waitFor(() => {
      expect(useChatAssistantStore.getState().panelOpen).toBe(true);
    });
  });
});
