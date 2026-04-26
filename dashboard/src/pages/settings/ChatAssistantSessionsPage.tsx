import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { MessageSquare, RefreshCcw } from "lucide-react";
import { ApiError } from "@/api/client";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorBanner } from "@/components/ui/ErrorBanner";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useConfigStore } from "@/state/config";
import { formatRelativeTime } from "@/lib/utils";
import { useChatAssistantSessions } from "@/hooks/useChatAssistantSessions";

const PAGE_LIMIT = 50;

export default function ChatAssistantSessionsPage() {
  const navigate = useNavigate();
  const principalRole = useConfigStore((s) => s.principalRole);
  const [cursor, setCursor] = useState<string | undefined>();
  const isAdmin = principalRole === "admin";

  const { data, isLoading, isError, error, refetch, isFetching } = useChatAssistantSessions({
    cursor,
    limit: PAGE_LIMIT,
    enabled: isAdmin,
  });

  const errorMessage = useMemo(() => {
    if (!isError) return null;
    if (error instanceof ApiError) {
      if (error.status === 403) return "Forbidden — chat.read_all permission required.";
      if (error.status === 401) return "Session expired — please sign in again.";
      return `${error.status} ${error.message}`;
    }
    return error instanceof Error ? error.message : "Unknown error";
  }, [isError, error]);

  if (!isAdmin) {
    return (
      <div className="space-y-6">
        <PageHeader
          label="Settings"
          title="Chat assistant sessions"
          subtitle="Auditable transcript index of every Cordum chat assistant session."
        />
        <ErrorBanner
          title="Admin access required"
          message="The chat session viewer is only available to operators with the chat.read_all permission."
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        label="Settings"
        title="Chat assistant sessions"
        subtitle="Auditable transcript index of every Cordum chat assistant session. Click a row to open the full transcript."
        actions={
          <Button
            variant="ghost"
            size="sm"
            onClick={() => refetch()}
            disabled={isFetching}
            aria-label="Refresh"
          >
            <RefreshCcw className="mr-1.5 h-3.5 w-3.5" />
            Refresh
          </Button>
        }
      />

      {errorMessage && (
        <ErrorBanner title="Could not load sessions" message={errorMessage} />
      )}

      {isLoading && <SkeletonTable rows={6} />}

      {!isLoading && data && data.items.length === 0 && (
        <EmptyState
          icon={<MessageSquare className="h-5 w-5" />}
          title="No chat sessions yet"
          description="Sessions appear here once an operator opens the chat assistant widget."
        />
      )}

      {!isLoading && data && data.items.length > 0 && (
        <div className="instrument-card overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border/50 bg-surface-1/40 text-left">
                <th className="px-4 py-2 font-mono uppercase tracking-widest text-[10px] text-muted-foreground/70">
                  Session
                </th>
                <th className="px-4 py-2 font-mono uppercase tracking-widest text-[10px] text-muted-foreground/70">
                  Principal
                </th>
                <th className="px-4 py-2 font-mono uppercase tracking-widest text-[10px] text-muted-foreground/70">
                  Tenant
                </th>
                <th className="px-4 py-2 font-mono uppercase tracking-widest text-[10px] text-muted-foreground/70">
                  Messages
                </th>
                <th className="px-4 py-2 font-mono uppercase tracking-widest text-[10px] text-muted-foreground/70">
                  Last active
                </th>
              </tr>
            </thead>
            <tbody>
              {data.items.map((s) => (
                <tr
                  key={s.sessionId}
                  className="cursor-pointer border-b border-border/30 last:border-0 hover:bg-surface-1/40"
                  onClick={() => navigate(`/copilot/sessions/${encodeURIComponent(s.sessionId)}`)}
                  role="link"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault();
                      navigate(`/copilot/sessions/${encodeURIComponent(s.sessionId)}`);
                    }
                  }}
                >
                  <td className="px-4 py-3 font-mono text-xs text-foreground">
                    {s.sessionId.slice(0, 12)}…
                  </td>
                  <td className="px-4 py-3 text-sm">{s.principal || "—"}</td>
                  <td className="px-4 py-3 text-sm">{s.tenant || "default"}</td>
                  <td className="px-4 py-3 font-mono text-xs">{s.messageCount}</td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {s.lastActiveAt ? formatRelativeTime(s.lastActiveAt) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {data && (data.nextCursor || cursor) && (
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <Button
            variant="ghost"
            size="sm"
            disabled={!cursor}
            onClick={() => setCursor(undefined)}
          >
            First page
          </Button>
          {data.nextCursor && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setCursor(data.nextCursor ?? undefined)}
            >
              Next page
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
