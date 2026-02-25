/*
 * DESIGN: "Control Surface" — Agent Fleet
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { motion } from "framer-motion";
import { get } from "@/api/client";
import { mapHeartbeatToWorker, type BackendHeartbeat } from "@/api/transform";
import type { Worker } from "@/api/types";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { SkeletonCard, SkeletonTable } from "@/components/ui/Skeleton";
import {
  Cpu, Search, RefreshCw, Zap, Filter, X,
} from "lucide-react";
import { cn, formatRelativeTime } from "@/lib/utils";

function workerStatusVariant(status: string) {
  switch (status) {
    case "idle": return "healthy" as const;
    case "busy": return "info" as const;
    case "draining": return "warning" as const;
    case "offline": return "danger" as const;
    default: return "muted" as const;
  }
}

export default function AgentsPage() {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [selectedWorker, setSelectedWorker] = useState<Worker | null>(null);

  const { data: workers, isLoading, refetch } = useQuery({
    queryKey: ["workers"],
    queryFn: async () => {
      const res = await get<BackendHeartbeat[]>("/workers");
      return (res ?? []).map(mapHeartbeatToWorker).filter((w): w is Worker => !!w);
    },
    refetchInterval: 15_000,
  });

  const allWorkers = workers ?? [];
  const idleCount = allWorkers.filter((w) => w.status === "idle").length;
  const busyCount = allWorkers.filter((w) => w.status === "busy").length;
  const offlineCount = allWorkers.filter((w) => w.status === "offline").length;

  const filtered = allWorkers.filter((w) => {
    if (statusFilter !== "all" && w.status !== statusFilter) return false;
    if (search) {
      const q = search.toLowerCase();
      return (
        w.id.toLowerCase().includes(q) ||
        (w.pool ?? "").toLowerCase().includes(q) ||
        w.capabilities?.some((t: string) => t.toLowerCase().includes(q))
      );
    }
    return true;
  });

  return (
    <div className="space-y-6">
      <PageHeader
        label="Fleet"
        title="Agent Fleet"
        subtitle="Monitor and manage worker agents across all pools"
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
        className="grid grid-cols-2 lg:grid-cols-4 gap-4"
      >
        {isLoading ? (
          Array.from({ length: 4 }).map((_, i) => <SkeletonCard key={i} />)
        ) : (
          <>
            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Total Agents</span>
                <Cpu className="w-4 h-4 text-cordum" />
              </div>
              <span className="font-mono text-2xl font-bold text-foreground">{allWorkers.length}</span>
              <div className="flex gap-1 mt-3">
                {allWorkers.map((w, i) => (
                  <div
                    key={i}
                    className={cn(
                      "w-2 h-2 rounded-full",
                      w.status === "idle" || w.status === "busy" ? "bg-emerald-400" : "bg-gray-500",
                    )}
                  />
                ))}
              </div>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Idle</span>
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 status-pulse" />
              </div>
              <span className="font-mono text-2xl font-bold text-emerald-400">{idleCount}</span>
              <p className="text-xs text-muted-foreground mt-1">Ready for work</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Busy</span>
                <Zap className="w-4 h-4 text-blue-400" />
              </div>
              <span className="font-mono text-2xl font-bold text-blue-400">{busyCount}</span>
              <p className="text-xs text-muted-foreground mt-1">Processing jobs</p>
            </div>

            <div className={cn("instrument-card p-5", offlineCount > 0 && "status-danger")}>
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Offline</span>
              </div>
              <span className={cn("font-mono text-2xl font-bold", offlineCount > 0 ? "text-red-400" : "text-foreground")}>{offlineCount}</span>
              <p className="text-xs text-muted-foreground mt-1">Disconnected</p>
            </div>
          </>
        )}
      </motion.div>

      {/* Filters — showcase style */}
      <div className="flex items-center gap-3">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search agents..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-8 w-full pl-8 pr-3 text-xs bg-surface-1 border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-cordum"
          />
        </div>
        <div className="flex items-center gap-1 bg-surface-1 border border-border rounded-md p-0.5">
          {["all", "idle", "busy", "offline"].map((s) => (
            <button
              key={s}
              onClick={() => setStatusFilter(s)}
              className={cn(
                "px-3 py-1.5 text-xs font-medium rounded transition-colors",
                statusFilter === s
                  ? "bg-cordum/10 text-cordum"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {s.charAt(0).toUpperCase() + s.slice(1)}
            </button>
          ))}
        </div>
      </div>

      {/* Worker Table — showcase style */}
      {isLoading ? (
        <SkeletonTable rows={6} />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<Cpu className="w-5 h-5" />}
          title="No agents found"
          description={search ? "Try adjusting your search" : "No agents have connected yet"}
        />
      ) : (
        <div className="instrument-card overflow-hidden">
          <div className="flex items-center justify-between px-5 py-3 border-b border-border">
            <h3 className="font-display font-semibold text-sm text-foreground">Worker Pool</h3>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              <RefreshCw className="w-3 h-3 mr-1" />
              Refresh
            </Button>
          </div>
          <table className="w-full">
            <thead>
              <tr className="border-b border-border bg-surface-0">
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Worker</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Status</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Pool</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Capabilities</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Jobs</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Last Seen</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((w) => (
                <tr
                  key={w.id}
                  onClick={() => setSelectedWorker(w)}
                  className="border-b border-border hover:bg-surface-1 transition-colors cursor-pointer"
                >
                  <td className="px-5 py-3">
                    <div className="flex items-center gap-2">
                      <Zap className="w-3.5 h-3.5 text-cordum" />
                      <span className="text-sm font-medium text-foreground">{w.id.slice(0, 16)}</span>
                    </div>
                  </td>
                  <td className="px-5 py-3">
                    <StatusBadge variant={workerStatusVariant(w.status)} dot pulse={w.status === "busy"}>
                      {w.status}
                    </StatusBadge>
                  </td>
                  <td className="px-5 py-3 text-sm text-muted-foreground">{w.pool || "default"}</td>
                  <td className="px-5 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(w.capabilities ?? []).slice(0, 3).map((t: string) => (
                        <span key={t} className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-2 text-muted-foreground">
                          {t}
                        </span>
                      ))}
                      {(w.capabilities?.length ?? 0) > 3 && (
                        <span className="text-[10px] text-muted-foreground">+{w.capabilities!.length - 3}</span>
                      )}
                    </div>
                  </td>
                  <td className="px-5 py-3 font-mono text-sm text-foreground">{w.activeJobs} / {w.capacity}</td>
                  <td className="px-5 py-3 text-sm text-muted-foreground">
                    {w.lastHeartbeat ? formatRelativeTime(w.lastHeartbeat) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Worker Detail Drawer */}
      {selectedWorker && (
        <>
          <div className="fixed inset-0 bg-black/40 z-40" onClick={() => setSelectedWorker(null)} />
          <motion.div
            initial={{ x: 400 }}
            animate={{ x: 0 }}
            exit={{ x: 400 }}
            transition={{ type: "spring", stiffness: 300, damping: 30 }}
            className="fixed inset-y-0 right-0 w-[400px] bg-surface-1 border-l border-border shadow-2xl z-50 overflow-y-auto"
          >
            <div className="p-5 border-b border-border flex items-center justify-between">
              <div>
                <h2 className="font-display font-semibold text-sm text-foreground">Agent Detail</h2>
                <p className="text-xs text-muted-foreground font-mono mt-0.5">{selectedWorker.id}</p>
              </div>
              <button
                onClick={() => setSelectedWorker(null)}
                className="p-1.5 rounded-md hover:bg-surface-2 text-muted-foreground hover:text-foreground transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="p-5 space-y-5">
              <div className="flex items-center gap-2">
                <StatusBadge variant={workerStatusVariant(selectedWorker.status)} dot pulse={selectedWorker.status === "busy"}>
                  {selectedWorker.status}
                </StatusBadge>
                <span className="text-xs text-muted-foreground">Pool: {selectedWorker.pool || "default"}</span>
              </div>
              <div>
                <p className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-2">Capabilities</p>
                <div className="flex flex-wrap gap-1.5">
                  {(selectedWorker.capabilities ?? []).map((t: string) => (
                    <span key={t} className="text-xs font-mono px-2 py-1 rounded bg-surface-2 text-foreground border border-border">{t}</span>
                  ))}
                  {(selectedWorker.capabilities ?? []).length === 0 && (
                    <span className="text-xs text-muted-foreground">None</span>
                  )}
                </div>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <p className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-1">Active Jobs</p>
                  <p className="text-lg font-mono font-bold text-cordum">{selectedWorker.activeJobs} / {selectedWorker.capacity}</p>
                </div>
                <div>
                  <p className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-1">Last Heartbeat</p>
                  <p className="text-sm text-foreground">
                    {selectedWorker.lastHeartbeat ? formatRelativeTime(selectedWorker.lastHeartbeat) : "—"}
                  </p>
                </div>
              </div>
            </div>
          </motion.div>
        </>
      )}
    </div>
  );
}
