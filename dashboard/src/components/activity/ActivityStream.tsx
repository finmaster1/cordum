import { useEffect, useRef, useState, useMemo } from "react";
import { Activity, Loader2, Send, CheckCircle2, XCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "../ui/Button";
import { Textarea } from "../ui/Textarea";
import { ActivityBlock } from "./ActivityBlock";
import type { ActivityItem } from "../../types/activity";

const ACTIVE_RUN_STATUSES = ["running", "pending", "waiting", "blocked"];
const MAX_ACTIVITY_ITEMS = 100;
const TERMINAL_STATUSES = ["succeeded", "failed", "denied", "cancelled", "timed_out"];

type FilterTab = "all" | "errors" | "safety" | "progress";

function matchesFilter(item: ActivityItem, filter: FilterTab): boolean {
  if (filter === "all") return true;
  const t = (item.type ?? "").toLowerCase();
  switch (filter) {
    case "errors":
      return t.includes("error") || t.includes("fail") || t.includes("denied") || t.includes("timeout");
    case "safety":
      return t.includes("safety") || t.includes("policy") || t.includes("decision");
    case "progress":
      return t.includes("progress") || t.includes("step") || t.includes("started") || t.includes("completed");
    default:
      return true;
  }
}

type Props = {
  items: ActivityItem[];
  isLoading?: boolean;
  runStatus: string;
  isSending?: boolean;
  onSendMessage?: (content: string) => void;
  onApprove?: (jobId: string) => void;
  onReject?: (jobId: string) => void;
};

export function ActivityStream({
  items,
  isLoading,
  runStatus,
  isSending,
  onSendMessage,
  onApprove,
  onReject,
}: Props) {
  const [input, setInput] = useState("");
  const [activeFilter, setActiveFilter] = useState<FilterTab>("all");
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const isRunActive = ACTIVE_RUN_STATUSES.includes(runStatus);
  const isTerminal = TERMINAL_STATUSES.includes(runStatus);

  const filteredItems = useMemo(
    () => items.filter((item) => matchesFilter(item, activeFilter)),
    [items, activeFilter],
  );
  const hiddenCount = filteredItems.length > MAX_ACTIVITY_ITEMS ? filteredItems.length - MAX_ACTIVITY_ITEMS : 0;
  const displayItems = filteredItems.slice(-MAX_ACTIVITY_ITEMS);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [items]);

  const handleSend = () => {
    const content = input.trim();
    if (!content || !onSendMessage || isSending) {
      return;
    }
    onSendMessage(content);
    setInput("");
    inputRef.current?.focus();
  };

  const handleKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="flex h-full min-h-[520px] flex-col rounded-3xl border border-border bg-card/70 overflow-hidden">
      <div className="flex items-center justify-between border-b border-border bg-card/50 px-5 py-4">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-accent/10">
            <Activity className="h-4 w-4 text-accent" />
          </div>
          <div>
            <div className="text-sm font-semibold text-ink">Activity Stream</div>
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              {isRunActive ? "Live run narrative" : "History"}
            </div>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span className={`h-2 w-2 rounded-full ${isRunActive ? "bg-success animate-pulse" : "bg-muted"}`} />
          <span className="text-xs text-muted-foreground capitalize">{runStatus}</span>
        </div>
      </div>

      {/* Filter Tabs */}
      <div className="flex items-center gap-1 px-4 pt-2 border-b border-border">
        {(["all", "errors", "safety", "progress"] as FilterTab[]).map((tab) => (
          <button
            key={tab}
            type="button"
            onClick={() => setActiveFilter(tab)}
            className={cn(
              "px-3 py-1.5 text-xs font-medium capitalize transition-colors",
              activeFilter === tab
                ? "text-accent border-b-2 border-accent"
                : "text-muted-foreground hover:text-foreground",
            )}
          >
            {tab}
          </button>
        ))}
      </div>

      <div ref={scrollRef} className="flex-1 overflow-y-auto p-4 space-y-3 scroll-smooth">
        {isLoading ? (
          <div className="flex items-center justify-center py-16">
            <Loader2 className="h-6 w-6 text-muted-foreground animate-spin" />
          </div>
        ) : displayItems.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-accent/10 mb-4">
              <Activity className="h-8 w-8 text-accent" />
            </div>
            <div className="text-sm font-medium text-ink mb-1">No activity yet</div>
            <div className="text-xs text-muted-foreground max-w-xs">
              {isRunActive
                ? "Waiting for the first event. Messages, policy checks, and tool calls will appear here."
                : "No activity recorded for this run."}
            </div>
          </div>
        ) : (
          <>
            {hiddenCount > 0 && (
              <div className="text-center text-xs text-muted-foreground py-1">
                {hiddenCount} older messages hidden
              </div>
            )}
            {displayItems.map((item) => (
              <ActivityBlock
                key={item.id}
                activity={item}
                onApprove={onApprove}
                onReject={onReject}
              />
            ))}
            {isTerminal && (
              <div className={cn(
                "flex items-center gap-2 px-4 py-3 rounded-xl text-sm font-medium",
                runStatus === "succeeded"
                  ? "bg-[var(--color-success)]/10 text-[var(--color-success)]"
                  : "bg-destructive/10 text-destructive",
              )}>
                {runStatus === "succeeded" ? (
                  <CheckCircle2 className="w-4 h-4" />
                ) : (
                  <XCircle className="w-4 h-4" />
                )}
                Run {runStatus}
              </div>
            )}
          </>
        )}
      </div>

      {isRunActive && onSendMessage ? (
        <div className="border-t border-border bg-card/50 p-4">
          <div className="flex gap-3">
            <Textarea
              ref={inputRef}
              value={input}
              onChange={(event) => setInput(event.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Ask about this workflow run..."
              rows={2}
              className="flex-1 resize-none"
              disabled={isSending}
            />
            <Button
              variant="primary"
              onClick={handleSend}
              disabled={!input.trim() || isSending}
              className="self-end"
            >
              {isSending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
              <span className="sr-only">Send</span>
            </Button>
          </div>
          <div className="mt-2 text-xs text-muted-foreground">Press Enter to send, Shift+Enter for new line</div>
        </div>
      ) : null}
    </div>
  );
}
