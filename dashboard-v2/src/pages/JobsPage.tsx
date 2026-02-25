/*
 * DESIGN: "Control Surface" — Jobs
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { motion } from "framer-motion";
import { get } from "@/api/client";
import { mapJobRecord, type BackendJobRecord } from "@/api/transform";
import type { Job } from "@/api/types";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Search, RefreshCw, ListChecks, Plus, Eye } from "lucide-react";
import { cn, formatRelativeTime } from "@/lib/utils";

function jobStatusVariant(status: string) {
  switch (status) {
    case "running": return "healthy" as const;
    case "succeeded": return "healthy" as const;
    case "failed": return "danger" as const;
    case "pending": case "scheduled": return "warning" as const;
    case "dispatched": return "info" as const;
    default: return "muted" as const;
  }
}

function safetyVariant(decision?: string) {
  switch (decision) {
    case "allow": return "healthy" as const;
    case "deny": return "danger" as const;
    case "escalate": return "warning" as const;
    default: return "muted" as const;
  }
}

export default function JobsPage() {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [activeTab, setActiveTab] = useState("all");

  const { data, isLoading, refetch } = useQuery({
    queryKey: ["jobs"],
    queryFn: async () => {
      const res = await get<{ items: BackendJobRecord[]; total?: number }>("/jobs?limit=500");
      const items = (res.items ?? []).map(mapJobRecord).filter((j): j is Job => !!j);
      return { items, total: res.total ?? items.length };
    },
    refetchInterval: 10_000,
  });

  const jobs = data?.items ?? [];

  const tabs = [
    { id: "all", label: "All", count: jobs.length },
    { id: "running", label: "Running", count: jobs.filter(j => j.status === "running").length },
    { id: "pending", label: "Pending", count: jobs.filter(j => j.status === "pending" || j.status === "scheduled").length },
    { id: "succeeded", label: "Completed", count: jobs.filter(j => j.status === "succeeded").length },
    { id: "failed", label: "Failed", count: jobs.filter(j => j.status === "failed").length },
  ];

  const filtered = jobs.filter((j) => {
    if (activeTab !== "all") {
      if (activeTab === "pending") {
        if (j.status !== "pending" && j.status !== "scheduled") return false;
      } else if (j.status !== activeTab) return false;
    }
    if (search) {
      const q = search.toLowerCase();
      return (
        j.id.toLowerCase().includes(q) ||
        (j.topic ?? "").toLowerCase().includes(q) ||
        (j.traceId ?? "").toLowerCase().includes(q)
      );
    }
    return true;
  });

  return (
    <div className="space-y-6">
      <PageHeader
        label="Core"
        title="Jobs"
        subtitle={`${data?.total ?? 0} total jobs across all states`}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              <RefreshCw className="w-3 h-3 mr-1" />
              Refresh
            </Button>
            <Button variant="primary" size="sm">
              <Plus className="w-3 h-3 mr-1" />
              Submit Job
            </Button>
          </div>
        }
      />

      {/* Filters — showcase style */}
      <div className="flex items-center gap-3 flex-wrap">
        <div className="relative flex-1 max-w-sm">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
          <input
            type="text"
            placeholder="Search by ID, topic, or trace..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-8 w-full pl-8 pr-3 text-xs bg-surface-1 border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-cordum"
          />
        </div>
        <div className="flex items-center gap-1 bg-surface-1 border border-border rounded-md p-0.5">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setActiveTab(tab.id)}
              className={cn(
                "px-3 py-1.5 text-xs font-medium rounded transition-colors",
                activeTab === tab.id
                  ? "bg-cordum/10 text-cordum"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {tab.label}
              {tab.count > 0 && (
                <span className="ml-1.5 px-1.5 py-0.5 rounded-full text-[10px] font-mono bg-surface-2">{tab.count}</span>
              )}
            </button>
          ))}
        </div>
      </div>

      {/* Jobs Table — showcase style */}
      {isLoading ? (
        <div className="instrument-card p-5">
          <SkeletonTable rows={8} />
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<ListChecks className="w-5 h-5" />}
          title="No jobs found"
          description={search ? "Try adjusting your search or filters" : "No jobs have been submitted yet"}
          action={
            <Button variant="primary" size="sm">
              <Plus className="w-3 h-3 mr-1" />
              Submit Job
            </Button>
          }
        />
      ) : (
        <motion.div
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3 }}
          className="instrument-card overflow-hidden"
        >
          <table className="w-full">
            <thead>
              <tr className="border-b border-border bg-surface-0">
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Status</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Job ID</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Topic</th>
                <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Safety</th>
                <th className="text-center px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Attempts</th>
                <th className="text-right px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Updated</th>
                <th className="px-5 py-3"></th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((job) => (
                <tr
                  key={job.id}
                  onClick={() => navigate(`/jobs/${job.id}`)}
                  className="border-b border-border hover:bg-surface-1 transition-colors cursor-pointer"
                >
                  <td className="px-5 py-3">
                    <StatusBadge variant={jobStatusVariant(job.status)} dot pulse={job.status === "running"}>
                      {job.status}
                    </StatusBadge>
                  </td>
                  <td className="px-5 py-3 font-mono text-sm text-cordum">{job.id.slice(0, 16)}</td>
                  <td className="px-5 py-3 text-sm text-foreground">{job.topic || "—"}</td>
                  <td className="px-5 py-3">
                    {job.safetyDecision ? (
                      <StatusBadge variant={safetyVariant(job.safetyDecision.type)}>
                        {job.safetyDecision.type}
                      </StatusBadge>
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                  </td>
                  <td className="px-5 py-3 text-center font-mono text-xs text-muted-foreground">{job.attempts ?? 0}</td>
                  <td className="px-5 py-3 text-right text-xs text-muted-foreground font-mono">
                    {job.updatedAt ? formatRelativeTime(new Date(job.updatedAt).toISOString()) : "—"}
                  </td>
                  <td className="px-5 py-3">
                    <button className="p-1 rounded hover:bg-surface-2 transition-colors">
                      <Eye className="w-3.5 h-3.5 text-muted-foreground" />
                    </button>
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
