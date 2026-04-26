import { useCallback, useEffect, useRef, useState } from "react";
import { useConfigStore } from "@/state/config";
import { useChatAssistantStore } from "@/state/chatAssistant";
import { logger } from "@/lib/logger";
import { generateUUID } from "@/lib/uuid";
import type {
  ChatConnectionStatus,
  ChatFrame,
} from "@/types/chatAssistant";

const MIN_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 8_000;
const BACKOFF_FACTOR = 2;
const MAX_FAILURES = 5;

function deriveWsUrl(sessionId: string | null): string {
  const { apiBaseUrl } = useConfigStore.getState();
  const proto = typeof window !== "undefined" && window.location.protocol === "https:" ? "wss:" : "ws:";
  const override = import.meta.env.VITE_WS_URL;
  const raw = (apiBaseUrl || import.meta.env.VITE_API_URL || "/api/v1").trim();
  const trimmed = raw.endsWith("/") ? raw.slice(0, -1) : raw;
  let base: string;
  if (override) {
    base = `${String(override).replace(/\/+$/, "")}/chat/ws`;
  } else if (/^https?:\/\//i.test(trimmed)) {
    base = `${trimmed.replace(/^http/i, "ws")}/chat/ws`;
  } else {
    const host = typeof window !== "undefined" ? window.location.host : "";
    base = `${proto}//${host}${trimmed}/chat/ws`;
  }
  if (sessionId) {
    return `${base}${base.includes("?") ? "&" : "?"}session_id=${encodeURIComponent(sessionId)}`;
  }
  return base;
}

function authSubprotocols(apiKey: string): string[] | undefined {
  if (!apiKey) return undefined;
  const encoded = btoa(apiKey).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return ["cordum-api-key", encoded];
}

function safeParseFrame(raw: string): ChatFrame | null {
  try {
    const parsed = JSON.parse(raw) as ChatFrame;
    if (!parsed || typeof parsed !== "object" || typeof parsed.type !== "string") return null;
    return parsed;
  } catch {
    return null;
  }
}

export interface ChatAssistantSessionApi {
  status: ChatConnectionStatus;
  error: string | null;
  send: (text: string) => void;
  connect: () => void;
  disconnect: () => void;
}

/**
 * Maintains a single WebSocket to /api/v1/chat/ws scoped by the
 * sessionStorage-backed session id. Frames are parsed and forwarded to the
 * `useChatAssistantStore` so React components stay declarative; the only
 * imperative surface is `send(text)`.
 *
 * Exponential backoff up to MAX_BACKOFF_MS with a 5-failure cap before we
 * surface `status='closed'` and stop trying — that prevents a wedged
 * cordum-llm-chat from pinning a dashboard tab on a tight reconnect loop.
 */
export function useChatAssistantSession(enabled: boolean): ChatAssistantSessionApi {
  const apiKey = useConfigStore((s) => s.apiKey);
  const apiBaseUrl = useConfigStore((s) => s.apiBaseUrl);

  const [status, setStatus] = useState<ChatConnectionStatus>("idle");
  const [error, setError] = useState<string | null>(null);

  const wsRef = useRef<WebSocket | null>(null);
  const backoffRef = useRef(MIN_BACKOFF_MS);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const failureCountRef = useRef(0);
  const cancelledRef = useRef(false);
  const connectRef = useRef<(() => void) | null>(null);

  const closeSocket = useCallback((code: number, reason: string) => {
    if (reconnectTimerRef.current !== null) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    if (wsRef.current) {
      try {
        wsRef.current.close(code, reason);
      } catch {
        // best-effort; ignore double-close
      }
      wsRef.current = null;
    }
  }, []);

  const connect = useCallback(() => {
    if (cancelledRef.current) return;
    if (!apiKey || !enabled) {
      setStatus("idle");
      return;
    }

    setStatus(failureCountRef.current === 0 ? "connecting" : "reconnecting");

    // Read session id imperatively so this closure stays stable across the
    // mint-on-first-connect → setSession path that would otherwise cause a
    // reconnect loop via the surrounding useEffect.
    const store = useChatAssistantStore.getState();
    let assignedId = store.sessionId;
    if (!assignedId) {
      assignedId = generateUUID();
      store.setSession(assignedId);
    }

    const url = deriveWsUrl(assignedId);
    const protocols = authSubprotocols(apiKey);
    let ws: WebSocket;
    try {
      ws = protocols ? new WebSocket(url, protocols) : new WebSocket(url);
    } catch (err) {
      logger.warn("chat-session", "WS construction failed", {
        error: err instanceof Error ? err.message : String(err),
      });
      setStatus("closed");
      setError("unable to open chat connection");
      return;
    }
    wsRef.current = ws;

    ws.onopen = () => {
      if (cancelledRef.current) {
        ws.close();
        return;
      }
      backoffRef.current = MIN_BACKOFF_MS;
      failureCountRef.current = 0;
      setStatus("open");
      setError(null);
    };

    ws.onmessage = (ev) => {
      const frame = safeParseFrame(typeof ev.data === "string" ? ev.data : "");
      if (!frame) return;
      useChatAssistantStore.getState().applyFrame(frame);
    };

    ws.onerror = () => {
      logger.debug("chat-session", "ws error event");
    };

    ws.onclose = (ev) => {
      wsRef.current = null;
      if (cancelledRef.current) {
        setStatus("closed");
        return;
      }
      // 1000 (normal) and 1001 (going away) are not failures worth
      // back-off on; the server may simply have rotated us.
      const wasGraceful = ev.code === 1000 || ev.code === 1001;
      if (!wasGraceful) {
        failureCountRef.current += 1;
      }
      if (failureCountRef.current >= MAX_FAILURES) {
        setStatus("closed");
        setError("unable to reach chat service");
        return;
      }
      const delay = backoffRef.current;
      backoffRef.current = Math.min(delay * BACKOFF_FACTOR, MAX_BACKOFF_MS);
      setStatus("reconnecting");
      reconnectTimerRef.current = setTimeout(() => {
        connectRef.current?.();
      }, delay);
    };
  }, [apiKey, enabled]);

  // Allow ws.onclose to call back into the latest connect() without
  // re-binding listeners on every render.
  connectRef.current = connect;

  useEffect(() => {
    cancelledRef.current = false;
    if (!enabled || !apiKey) {
      closeSocket(1000, "disabled");
      setStatus("idle");
      return;
    }
    failureCountRef.current = 0;
    backoffRef.current = MIN_BACKOFF_MS;
    connect();
    return () => {
      cancelledRef.current = true;
      closeSocket(1000, "unmount");
    };
    // apiBaseUrl change requires a fresh URL derivation; track it explicitly.
  }, [enabled, apiKey, apiBaseUrl, connect, closeSocket]);

  const send = useCallback((text: string) => {
    const trimmed = text.trim();
    if (!trimmed) return;
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      setError("not connected");
      return;
    }
    const frame = {
      type: "user",
      id: generateUUID(),
      text: trimmed,
      at: new Date().toISOString(),
    };
    try {
      ws.send(JSON.stringify(frame));
      useChatAssistantStore.getState().applyFrame(frame as ChatFrame);
    } catch (err) {
      logger.warn("chat-session", "send failed", {
        error: err instanceof Error ? err.message : String(err),
      });
      setError("send failed");
    }
  }, []);

  const disconnect = useCallback(() => {
    cancelledRef.current = true;
    closeSocket(1000, "manual-disconnect");
    setStatus("closed");
  }, [closeSocket]);

  const reconnect = useCallback(() => {
    cancelledRef.current = false;
    failureCountRef.current = 0;
    backoffRef.current = MIN_BACKOFF_MS;
    connect();
  }, [connect]);

  return { status, error, send, connect: reconnect, disconnect };
}
