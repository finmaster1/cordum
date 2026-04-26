import { useEffect, useRef, useState } from "react";
import { useConfigStore } from "@/state/config";
import { useLicense } from "@/hooks/useLicense";
import { logger } from "@/lib/logger";
import type { AvailabilityReason, AvailabilityStatus } from "@/types/chatAssistant";

const POLL_INTERVAL_MS = 10_000;
const PROBE_TIMEOUT_MS = 5_000;
const HEALTHZ_PATH = "/chat/healthz";

interface HealthzBody {
  redis?: string;
  vllm?: string;
  reason?: string;
}

function reasonFromBody(body: HealthzBody | null): AvailabilityReason {
  if (!body) return "unknown";
  if (body.reason && /vllm/i.test(body.reason)) return "vllm_down";
  if (body.reason && /redis/i.test(body.reason)) return "redis_down";
  if (typeof body.vllm === "string" && body.vllm !== "ok") return "vllm_down";
  if (typeof body.redis === "string" && body.redis !== "ok") return "redis_down";
  return "vllm_down";
}

function deriveBaseUrl(): string {
  const { apiBaseUrl } = useConfigStore.getState();
  const raw = (apiBaseUrl || import.meta.env.VITE_API_URL || "/api/v1").trim();
  return raw.endsWith("/") ? raw.slice(0, -1) : raw;
}

function authHeaders(): Record<string, string> {
  const { apiKey, tenantId, principalId, principalRole } = useConfigStore.getState();
  const h: Record<string, string> = {};
  if (apiKey) h["X-API-Key"] = apiKey;
  if (tenantId) h["X-Tenant-ID"] = tenantId;
  if (principalId) h["X-Principal-Id"] = principalId;
  if (principalRole) h["X-Principal-Role"] = principalRole;
  return h;
}

/**
 * Poll the cordum-llm-chat readiness probe and surface a tagged
 * `AvailabilityStatus`. The widget renders nothing while we are
 * unentitled or unavailable — we never show a greyed-out chat button
 * (per epic rail #5: "Users never see a broken chat UI").
 *
 * The hook itself does NOT request anything when the license is missing
 * or the LLM-chat entitlement is off; this avoids generating noisy 401s
 * on Community-tier deployments where the route does not exist.
 */
export function useChatAssistantAvailability(): AvailabilityStatus {
  const { data: license } = useLicense();
  const [status, setStatus] = useState<AvailabilityStatus>({
    available: false,
    reason: "unknown",
  });
  const lastEntitledRef = useRef<boolean | null>(null);

  const features = license?.entitlements?.features;
  const entitled = !!(features && features["llm_chat_assistant"]);

  useEffect(() => {
    // Update visible status if entitlement state flipped.
    if (lastEntitledRef.current !== entitled) {
      lastEntitledRef.current = entitled;
      if (!entitled) {
        setStatus({ available: false, reason: "unentitled" });
      }
    }
    if (!entitled) return;

    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    async function probe() {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), PROBE_TIMEOUT_MS);
      try {
        const res = await fetch(`${deriveBaseUrl()}${HEALTHZ_PATH}`, {
          method: "GET",
          headers: authHeaders(),
          credentials: "include",
          signal: controller.signal,
        });
        if (cancelled) return;
        if (res.ok) {
          setStatus({ available: true });
          return;
        }
        if (res.status === 401 || res.status === 403) {
          setStatus({ available: false, reason: "unauthorized" });
          return;
        }
        let body: HealthzBody | null = null;
        try {
          body = (await res.json()) as HealthzBody;
        } catch {
          body = null;
        }
        setStatus({ available: false, reason: reasonFromBody(body) });
      } catch (err) {
        if (cancelled) return;
        // Network error / timeout / aborted — treat as backend unreachable.
        if (!(err instanceof DOMException && err.name === "AbortError")) {
          logger.debug("chat-availability", "probe failed", {
            error: err instanceof Error ? err.message : String(err),
          });
        }
        setStatus({ available: false, reason: "vllm_down" });
      } finally {
        clearTimeout(timeout);
      }
    }

    function schedule() {
      if (cancelled) return;
      timer = setTimeout(async () => {
        await probe();
        schedule();
      }, POLL_INTERVAL_MS);
    }

    void probe().then(schedule);

    return () => {
      cancelled = true;
      if (timer !== null) clearTimeout(timer);
    };
  }, [entitled]);

  return status;
}
