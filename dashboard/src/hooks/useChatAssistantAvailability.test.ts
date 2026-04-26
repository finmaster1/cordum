import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { http, HttpResponse, server } from "@/test-utils/msw";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ensureMswServerListening, resetMswServer } from "@/test-utils/msw";
import { useConfigStore } from "@/state/config";
import { type ReactNode } from "react";
import { createElement } from "react";
import { useChatAssistantAvailability } from "./useChatAssistantAvailability";

function wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

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
  ensureMswServerListening();
  useConfigStore.setState({ apiKey: "test-key" });
});
afterEach(() => {
  resetMswServer();
});

function makeClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0, refetchOnWindowFocus: false },
    },
  });
}

describe("useChatAssistantAvailability", () => {
  it("returns unentitled without making any healthz request when license lacks the feature", async () => {
    const calls: string[] = [];
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(communityLicense)),
      http.get("*/api/v1/chat/healthz", () => {
        calls.push("healthz");
        return HttpResponse.json({}, { status: 200 });
      }),
    );
    const qc = makeClient();
    const { result } = renderHook(() => useChatAssistantAvailability(), {
      wrapper: wrapper(qc),
    });
    await waitFor(() => {
      expect(result.current).toEqual({ available: false, reason: "unentitled" });
    });
    expect(calls).toHaveLength(0);
  });

  it("returns available=true when entitled and healthz returns 200", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () => HttpResponse.json({}, { status: 200 })),
    );
    const qc = makeClient();
    const { result } = renderHook(() => useChatAssistantAvailability(), {
      wrapper: wrapper(qc),
    });
    await waitFor(() => {
      expect(result.current).toEqual({ available: true });
    });
  });

  it("returns vllm_down on 503 with vllm fail body", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () =>
        HttpResponse.json({ vllm: "fail: not connected", redis: "ok" }, { status: 503 }),
      ),
    );
    const qc = makeClient();
    const { result } = renderHook(() => useChatAssistantAvailability(), {
      wrapper: wrapper(qc),
    });
    await waitFor(() => {
      expect(result.current).toEqual({ available: false, reason: "vllm_down" });
    });
  });

  it("returns redis_down when only redis is failing", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () =>
        HttpResponse.json({ vllm: "ok", redis: "fail: timeout" }, { status: 503 }),
      ),
    );
    const qc = makeClient();
    const { result } = renderHook(() => useChatAssistantAvailability(), {
      wrapper: wrapper(qc),
    });
    await waitFor(() => {
      expect(result.current).toEqual({ available: false, reason: "redis_down" });
    });
  });

  it("returns unauthorized for 401 responses", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () => HttpResponse.json({}, { status: 401 })),
    );
    const qc = makeClient();
    const { result } = renderHook(() => useChatAssistantAvailability(), {
      wrapper: wrapper(qc),
    });
    await waitFor(() => {
      expect(result.current).toEqual({ available: false, reason: "unauthorized" });
    });
  });

  it("returns vllm_down on network error", async () => {
    server.use(
      http.get("*/api/v1/license", () => HttpResponse.json(enterpriseLicense)),
      http.get("*/api/v1/chat/healthz", () => HttpResponse.error()),
    );
    const qc = makeClient();
    const { result } = renderHook(() => useChatAssistantAvailability(), {
      wrapper: wrapper(qc),
    });
    await waitFor(() => {
      expect(result.current).toEqual({ available: false, reason: "vllm_down" });
    });
  });
});
