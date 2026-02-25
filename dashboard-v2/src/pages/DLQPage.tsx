/*
 * DESIGN: "Control Surface" — Dead Letter Queue
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { motion } from "framer-motion";
import { get, post } from "@/api/client";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { SkeletonCard, SkeletonTable } from "@/components/ui/Skeleton";
import { Search, RefreshCw, AlertTriangle, Play, Trash2, CheckCircle2, ArrowUpRight } from "lucide-react";
import { cn, formatRelativeTime } from "@/lib/utils";
import { toast } from "sonner";

interface DLQItem {
  id: string;
  jobId: string;
  topic?: string;
  error?: string;
  attempts: number;
  failedAt: string;
  payload?: any;
}

export default function DLQPage() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["dlq"],
    queryFn: async () => {
      const res = await get<{ items: DLQItem[]; total?: number }>("/dlq?limit=200");
      return { items: res.items ?? [], total: res.total ?? (res.items ?? []).length };
    },
    refetchInterval: 15_000,
  });

  const retryMutation = useMutation({
    mutationFn: async (id: string) => { await post(`/dlq/${id}/retry`, {}); },
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["dlq"] }); toast.success("Retry queued"); },
    onError: () => toast.error("Retry failed"),
  });

  const purgeMutation = useMutation({
    mutationFn: async (id: string) => { await post(`/dlq/${id}/purge`, {}); },
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["dlq"] }); toast.success("Purged"); },
    onError: () => toast.error("Purge failed"),
  });

  const items = (data?.items ?? []).filter((d) => {
    if (!search) return true;
    const q = search.toLowerCase();
    return d.jobId.toLowerCase().includes(q) || (d.topic ?? "").toLowerCase().includes(q) || (d.error ?? "").toLowerCase().includes(q);
  });

  const avgAttempts = items.length > 0 ? (items.reduce((s, i) => s + i.attempts, 0) / items.length).toFixed(1) : "0";

  return (
    <div className="space-y-6">
      <PageHeader
        label="Platform"
        title="Dead Letter Queue"
        subtitle="Failed messages requiring attention"
        actions={
          <Button variant="outline" size="sm" onClick={() => refetch()}>
            <RefreshCw className="w-3 h-3 mr-1" />
            Refresh
          </Button>
        }
      />

      {/* KPI Row — showcase style */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="grid grid-cols-2 lg:grid-cols-3 gap-4"
      >
        {isLoading ? (
          Array.from({ length: 3 }).map((_, i) => <SkeletonCard key={i} />)
        ) : (
          <>
            <div className={cn("instrument-card p-5", items.length > 0 ? "status-danger" : "")}>
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Dead Letters</span>
                <AlertTriangle className={cn("w-4 h-4", items.length > 0 ? "text-red-400" : "text-emerald-400")} />
              </div>
              <span className={cn("font-mono text-2xl font-bold", items.length > 0 ? "text-red-400" : "text-emerald-400")}>{data?.total ?? 0}</span>
              <p className="text-xs text-muted-foreground mt-1">{items.length > 0 ? "Requires attention" : "Queue clear"}</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Avg Attempts</span>
              </div>
              <span className="font-mono text-2xl font-bold text-foreground">{avgAttempts}</span>
              <p className="text-xs text-muted-foreground mt-1">Before dead-lettering</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Status</span>
                <span className={cn("w-1.5 h-1.5 rounded-full status-pulse", items.length > 0 ? "bg-red-400" : "bg-emerald-400")} />
              </div>
              <span className={cn("font-mono text-sm font-bold", items.length > 0 ? "text-amber-400" : "text-emerald-400")}>
                {items.length > 0 ? "Attention Required" : "All Clear"}
              </span>
            </div>
          </>
        )}
      </motion.div>

      {/* Search */}
      <div className="relative max-w-sm">
        <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
        <input
          type="text"
          placeholder="Search by job ID, topic, or error..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="h-8 w-full pl-8 pr-3 text-xs bg-surface-1 border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-cordum"
        />
      </div>

      {/* Table — showcase style */}
      {isLoading ? (
        <div className="instrument-card p-5">
          <SkeletonTable rows={6} />
        </div>
      ) : items.length === 0 ? (
        <EmptyState
          icon={<CheckCircle2 className="w-5 h-5" />}
          title="DLQ is empty"
          description="No failed messages — all systems healthy"
        />
      ) : (
        <motion.div
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, delay: 0.1 }}
          className="instrument-card status-danger overflow-hidden"
        >
          <table className="w-full">
            <thead>
              <tr className="border-b border-border bg-surface-0">
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Job ID</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Topic</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Error</th>
                <th className="text-center px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Attempts</th>
                <th className="text-right px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Failed</th>
                <th className="px-5 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {items.map((d) => (
                <tr key={d.id} className="border-b border-border hover:bg-surface-1 transition-colors">
                  <td className="px-5 py-3 font-mono text-sm text-foreground">{d.jobId.slice(0, 16)}</td>
                  <td className="px-5 py-3 text-sm text-foreground">{d.topic ?? "—"}</td>
                  <td className="px-5 py-3">
                    <span className="text-xs text-red-400 truncate max-w-[250px] block font-mono">{d.error ?? "—"}</span>
                  </td>
                  <td className="px-5 py-3 text-center font-mono text-xs text-muted-foreground">{d.attempts}</td>
                  <td className="px-5 py-3 text-right text-xs text-muted-foreground font-mono">{formatRelativeTime(d.failedAt)}</td>
                  <td className="px-5 py-3">
                    <div className="flex gap-1 justify-end">
                      <button
                        onClick={() => retryMutation.mutate(d.id)}
                        className="p-1.5 rounded hover:bg-surface-2 transition-colors text-cordum"
                        title="Retry"
                      >
                        <Play className="w-3.5 h-3.5" />
                      </button>
                      <button
                        onClick={() => purgeMutation.mutate(d.id)}
                        className="p-1.5 rounded hover:bg-surface-2 transition-colors text-red-400"
                        title="Purge"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </motion.div>
      )}
    </div>
  );
}
