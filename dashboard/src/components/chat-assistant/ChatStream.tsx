import { useEffect, useRef } from "react";
import { motion, useReducedMotion } from "framer-motion";
import { cn } from "@/lib/utils";
import type { ChatAssistantMessage } from "@/types/chatAssistant";

interface ChatStreamProps {
  messages: ChatAssistantMessage[];
  emptyHint?: string;
  onSuggestionClick?: (text: string) => void;
}

// isAwaitingAssistantText returns true when the latest exchange has a user
// message but no assistant text yet. The indicator disappears as soon as the
// first assistant delta arrives.
function isAwaitingAssistantText(messages: ChatAssistantMessage[]): boolean {
  if (messages.length === 0) return false;
  return messages[messages.length - 1].role === "user";
}

const EMPTY_SUGGESTIONS: readonly string[] = [
  "show denied jobs today",
  "list my active workflows",
  "what policies apply to billing?",
];

export function ChatStream({ messages, emptyHint, onSuggestionClick }: ChatStreamProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastTextLengthRef = useRef(0);

  const tail = messages[messages.length - 1];
  const tailText = tail?.text ?? "";

  useEffect(() => {
    const node = scrollRef.current;
    if (!node) return;
    if (lastTextLengthRef.current === tailText.length && messages.length === 0) return;
    lastTextLengthRef.current = tailText.length;
    node.scrollTop = node.scrollHeight;
  }, [messages, tailText]);

  if (messages.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center px-6 text-center">
        <div className="font-display text-base font-semibold text-foreground">Ask Cordum</div>
        <p className="mt-2 max-w-[28ch] text-xs text-muted-foreground/80">
          {emptyHint ?? "Pick a suggestion below or ask anything about Cordum configuration."}
        </p>
        <ul
          className="mt-4 flex w-full max-w-[18rem] flex-col gap-2"
          aria-label="Suggested prompts"
        >
          {EMPTY_SUGGESTIONS.map((text) => (
            <li key={text}>
              <button
                type="button"
                aria-label={`Send suggestion: ${text}`}
                onClick={onSuggestionClick ? () => onSuggestionClick(text) : undefined}
                disabled={!onSuggestionClick}
                className="w-full rounded-xl border border-border/60 bg-surface-1/60 px-3 py-2 text-left text-xs text-foreground/90 transition-colors hover:border-cordum/40 hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:border-border/60 disabled:hover:bg-surface-1/60"
              >
                {text}
              </button>
            </li>
          ))}
        </ul>
      </div>
    );
  }

  const thinking = isAwaitingAssistantText(messages);

  return (
    <div ref={scrollRef} className="flex-1 overflow-y-auto scrollbar-thin px-3 py-3">
      <div className="space-y-3">
        {messages.map((m) => (
          <MessageBubble key={m.id} message={m} />
        ))}
        {thinking && <ThinkingBubble />}
      </div>
    </div>
  );
}

// ThinkingBubble renders an assistant-styled bubble with three dots
// pulsing in sequence while the backend is composing a response. Lives
// in the same column the next assistant message will land in, so when
// the first delta arrives the dots are replaced by content without a
// layout shift.
function ThinkingBubble() {
  const reduceMotion = useReducedMotion();
  return (
    <motion.div
      initial={{ opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={reduceMotion ? { duration: 0 } : { duration: 0.18, ease: "easeOut" }}
      role="status"
      aria-live="polite"
      aria-label="Cordum is thinking"
      className="flex flex-col items-start"
    >
      <div className="flex items-center gap-2 rounded-xl border border-border bg-surface-1 px-3 py-2.5 text-sm leading-relaxed text-muted-foreground">
        <span className="sr-only">Cordum is thinking…</span>
        <span aria-hidden="true" className="flex items-center gap-1">
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full bg-cordum/70",
              !reduceMotion && "motion-safe:animate-[thinking-dot_1.2s_ease-in-out_infinite]",
            )}
            style={!reduceMotion ? { animationDelay: "0ms" } : undefined}
          />
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full bg-cordum/70",
              !reduceMotion && "motion-safe:animate-[thinking-dot_1.2s_ease-in-out_infinite]",
            )}
            style={!reduceMotion ? { animationDelay: "180ms" } : undefined}
          />
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full bg-cordum/70",
              !reduceMotion && "motion-safe:animate-[thinking-dot_1.2s_ease-in-out_infinite]",
            )}
            style={!reduceMotion ? { animationDelay: "360ms" } : undefined}
          />
        </span>
        <span className="text-xs font-mono tracking-wide text-muted-foreground/80">thinking</span>
      </div>
    </motion.div>
  );
}

interface MessageBubbleProps {
  message: ChatAssistantMessage;
}

function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === "user";
  return (
    <div className={cn("flex flex-col", isUser ? "items-end" : "items-start")}>
      <div
        className={cn(
          "max-w-[85%] rounded-xl px-3 py-2 text-sm leading-relaxed whitespace-pre-wrap break-words",
          isUser
            ? "bg-cordum/10 text-foreground border border-cordum/20"
            : "bg-surface-1 text-foreground border border-border",
        )}
      >
        {message.text || (isUser ? "" : "…")}
      </div>
    </div>
  );
}
