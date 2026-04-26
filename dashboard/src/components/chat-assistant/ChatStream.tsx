import { useEffect, useRef } from "react";
import { cn } from "@/lib/utils";
import { ToolCallCard } from "./ToolCallCard";
import { ApprovalInlinePrompt } from "./ApprovalInlinePrompt";
import type { ChatAssistantMessage } from "@/types/chatAssistant";

interface ChatStreamProps {
  messages: ChatAssistantMessage[];
  emptyHint?: string;
}

export function ChatStream({ messages, emptyHint }: ChatStreamProps) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const lastTextLengthRef = useRef(0);

  const tail = messages[messages.length - 1];
  const tailText = tail?.text ?? "";

  useEffect(() => {
    const node = scrollRef.current;
    if (!node) return;
    // Only auto-scroll if the content actually changed; preserves manual
    // scroll-back when nothing new arrived.
    if (lastTextLengthRef.current === tailText.length && messages.length === 0) return;
    lastTextLengthRef.current = tailText.length;
    node.scrollTop = node.scrollHeight;
  }, [messages, tailText]);

  if (messages.length === 0) {
    return (
      <div className="flex h-full flex-col items-center justify-center px-6 text-center">
        <div className="font-display text-base font-semibold text-foreground">Ask Cordum</div>
        <p className="mt-2 max-w-[28ch] text-xs text-muted-foreground/80">
          {emptyHint ??
            "Try “list failing jobs” or “submit a $40 mock-bank transfer.” Mutating actions still go through approvals."}
        </p>
      </div>
    );
  }

  return (
    <div ref={scrollRef} className="flex-1 overflow-y-auto scrollbar-thin px-3 py-3">
      <div className="space-y-3">
        {messages.map((m) => (
          <MessageBubble key={m.id} message={m} />
        ))}
      </div>
    </div>
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
      {!isUser && message.toolCalls.length > 0 && (
        <div className="w-full max-w-[85%]">
          {message.toolCalls.map((tc) => (
            <div key={tc.toolCallId}>
              <ToolCallCard toolCall={tc} />
              <ApprovalInlinePrompt toolCall={tc} />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
