import { useEffect } from "react";
import { motion, AnimatePresence, useReducedMotion } from "framer-motion";
import { X, AlertCircle, Info } from "lucide-react";
import { cn } from "@/lib/utils";
import { useChatAssistantStore } from "@/state/chatAssistant";
import { useChatAssistantSession } from "@/hooks/useChatAssistantSession";
import { useChatAssistantAvailability } from "@/hooks/useChatAssistantAvailability";
import { ChatStream } from "./ChatStream";
import { ChatComposer } from "./ChatComposer";

// OWASP LLM09 — operator-overreliance affordance. The disclaimer is rendered
// persistently below the composer so users never mistake informational guidance
// for an executed Cordum action.
const ADVISORY_DISCLAIMER =
  "Informational only: this chat explains Cordum docs and configuration, but does not execute actions. Use the dashboard or CLI for jobs, workflows, and approvals.";

export function ChatWidget() {
  const availability = useChatAssistantAvailability();
  const panelOpen = useChatAssistantStore((s) => s.panelOpen);
  const closePanel = useChatAssistantStore((s) => s.closePanel);
  const messages = useChatAssistantStore((s) => s.messages);
  const markRead = useChatAssistantStore((s) => s.markRead);
  const reduceMotion = useReducedMotion();
  const enabled = availability.available && panelOpen;
  const session = useChatAssistantSession(enabled);

  useEffect(() => {
    if (panelOpen) markRead();
  }, [panelOpen, messages.length, markRead]);

  // Esc closes the panel — but only when the panel is open AND we're not
  // currently typing in the composer (the textarea handles its own keys).
  useEffect(() => {
    if (!panelOpen) return;
    function onKey(e: KeyboardEvent) {
      if (e.key !== "Escape") return;
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === "TEXTAREA" || target.tagName === "INPUT")) return;
      closePanel();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [panelOpen, closePanel]);

  if (!availability.available) return null;

  const composerDisabled = session.status !== "open";

  return (
    <AnimatePresence>
      {panelOpen && (
        <motion.aside
          role="complementary"
          aria-label="Cordum chat assistant"
          initial={{ opacity: 0, x: 24 }}
          animate={{ opacity: 1, x: 0 }}
          exit={{ opacity: 0, x: 24 }}
          transition={reduceMotion ? { duration: 0 } : { duration: 0.18, ease: "easeOut" }}
          className={cn(
            "fixed z-50 flex flex-col",
            "inset-0 max-w-none rounded-none",
            "sm:inset-auto sm:top-16 sm:bottom-4 sm:right-4 sm:w-[380px] sm:max-w-[calc(100vw-2rem)] sm:rounded-2xl",
            "border border-cordum/30 bg-surface-0 shadow-soft",
            "overflow-hidden",
          )}
        >
          <header className="flex items-center justify-between border-b border-border/50 px-3 h-11 shrink-0">
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  "h-2 w-2 rounded-full",
                  session.status === "open"
                    ? "bg-status-healthy"
                    : session.status === "reconnecting" || session.status === "connecting"
                      ? "bg-status-warning"
                      : "bg-muted-foreground/40",
                )}
                aria-hidden="true"
              />
              <span className="font-display text-sm font-semibold text-foreground">Cordum chat</span>
              <span className="text-[10px] font-mono uppercase tracking-widest text-muted-foreground/60">
                {session.status}
              </span>
            </div>
            <button
              type="button"
              onClick={closePanel}
              aria-label="Close chat"
              className="flex h-11 w-11 sm:h-7 sm:w-7 items-center justify-center rounded-xl text-muted-foreground hover:bg-surface-2 hover:text-foreground"
            >
              <X className="h-4 w-4" />
            </button>
          </header>
          <ChatStream
            messages={messages}
            onSuggestionClick={composerDisabled ? undefined : session.send}
          />
          {session.error && (
            <div
              role="alert"
              aria-live="assertive"
              className="flex items-start gap-2 border-t border-status-error/30 bg-status-error/10 px-3 py-2 text-xs text-status-error"
            >
              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
              <span>{session.error}</span>
            </div>
          )}
          <ChatComposer
            onSubmit={session.send}
            disabled={composerDisabled}
            placeholder={composerDisabled ? "Connecting..." : undefined}
          />
          <div
            role="note"
            aria-label="advisory disclaimer"
            className="flex items-start gap-2 border-t border-border/50 bg-surface-1/50 px-3 py-2 text-[11px] text-muted-foreground"
          >
            <Info className="mt-0.5 h-3 w-3 shrink-0" aria-hidden="true" />
            <span>{ADVISORY_DISCLAIMER}</span>
          </div>
        </motion.aside>
      )}
    </AnimatePresence>
  );
}
