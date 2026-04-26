import { MessageSquare } from "lucide-react";
import { cn } from "@/lib/utils";
import { useChatAssistantStore } from "@/state/chatAssistant";
import { useChatAssistantAvailability } from "@/hooks/useChatAssistantAvailability";
import { FEATURE_FLAGS } from "@/config/flags";

export function ChatHeaderButton() {
  const availability = useChatAssistantAvailability();
  const panelOpen = useChatAssistantStore((s) => s.panelOpen);
  const togglePanel = useChatAssistantStore((s) => s.togglePanel);
  const unreadCount = useChatAssistantStore((s) => s.unreadCount);
  const pendingCount = useChatAssistantStore((s) => s.pendingApprovalIds.length);

  if (!FEATURE_FLAGS.llmChatAssistant) return null;
  if (!availability.available) return null;

  const badge = unreadCount + pendingCount;

  return (
    <button
      type="button"
      onClick={togglePanel}
      aria-label={panelOpen ? "Close chat assistant" : "Open chat assistant"}
      aria-pressed={panelOpen}
      className={cn(
        "relative flex h-7 w-7 items-center justify-center rounded-xl transition-colors",
        panelOpen
          ? "bg-cordum/10 text-cordum"
          : "text-muted-foreground hover:bg-surface-2 hover:text-foreground",
      )}
    >
      <MessageSquare className="h-4 w-4" />
      {badge > 0 && (
        <span
          aria-live="polite"
          className={cn(
            "absolute -right-1 -top-1 flex h-4 min-w-[16px] items-center justify-center rounded-full px-1",
            "text-[10px] font-mono font-bold",
            pendingCount > 0
              ? "bg-status-warning text-surface-0"
              : "bg-cordum text-surface-0",
          )}
        >
          {badge > 9 ? "9+" : badge}
        </span>
      )}
    </button>
  );
}
