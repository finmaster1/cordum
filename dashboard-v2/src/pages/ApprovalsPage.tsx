/*
 * DESIGN: "Control Surface" — Approvals
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { motion } from "framer-motion";
import { get, post } from "@/api/client";
import { mapApprovalItem, type BackendApprovalItem } from "@/api/transform";
import type { Approval } from "@/api/types";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Button } from "@/components/ui/Button";
import { EmptyState } from "@/components/ui/EmptyState";
import { SkeletonCard, SkeletonTable } from "@/components/ui/Skeleton";
import { Search, RefreshCw, UserCheck, CheckCircle2, XCircle, Shield, Clock, X } from "lucide-react";
import { cn, formatRelativeTime } from "@/lib/utils";
import { toast } from "sonner";

function approvalStatusVariant(status: string) {
  switch (status) {
    case "pending": return "warning" as const;
    case "approved": return "healthy" as const;
    case "denied": return "danger" as const;
    case "expired": return "muted" as const;
    default: return "muted" as const;
  }
}

export default function ApprovalsPage() {
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const [activeTab, setActiveTab] = useState("pending");
  const [selectedApproval, setSelectedApproval] = useState<Approval | null>(null);

  const { data: approvals, isLoading, refetch } = useQuery({
    queryKey: ["approvals"],
    queryFn: async () => {
      const res = await get<{ items: BackendApprovalItem[] }>("/approvals?limit=500");
      return (res.items ?? []).map(mapApprovalItem).filter((a): a is Approval => !!a);
    },
    refetchInterval: 5_000,
  });

  const approveMutation = useMutation({
    mutationFn: async ({ id, decision, reason }: { id: string; decision: "approve" | "deny"; reason?: string }) => {
      await post(`/approvals/${id}/${decision}`, { reason });
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["approvals"] });
      toast.success("Decision recorded");
      setSelectedApproval(null);
    },
    onError: () => toast.error("Failed to record decision"),
  });

  const all = approvals ?? [];
  const pending = all.filter((a) => a.status === "pending");
  const approved = all.filter((a) => a.status === "approved");
  const denied = all.filter((a) => a.status === "denied");

  const filtered = all
    .filter((a) => {
      if (activeTab !== "all" && a.status !== activeTab) return false;
      if (search) {
        const q = search.toLowerCase();
        return a.id.toLowerCase().includes(q) || (a.topic ?? "").toLowerCase().includes(q) || (a.jobId ?? "").toLowerCase().includes(q);
      }
      return true;
    })
    .sort((a, b) => {
      if (a.status === "pending" && b.status !== "pending") return -1;
      if (b.status === "pending" && a.status !== "pending") return 1;
      return new Date(b.requestedAt ?? 0).getTime() - new Date(a.requestedAt ?? 0).getTime();
    });

  return (
    <div className="space-y-6">
      <PageHeader
        label="Safety"
        title="Approvals"
        subtitle="Human-in-the-loop review queue for agent actions"
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
        className="grid grid-cols-3 gap-4"
      >
        {isLoading ? (
          Array.from({ length: 3 }).map((_, i) => <SkeletonCard key={i} />)
        ) : (
          <>
            <div className={cn("instrument-card p-5", pending.length > 0 && "status-warning")}>
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Pending</span>
                <Clock className="w-4 h-4 text-amber-400" />
              </div>
              <span className={cn("font-mono text-2xl font-bold", pending.length > 0 ? "text-amber-400" : "text-foreground")}>{pending.length}</span>
              <p className="text-xs text-muted-foreground mt-1">Awaiting human review</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Approved</span>
                <CheckCircle2 className="w-4 h-4 text-emerald-400" />
              </div>
              <span className="font-mono text-2xl font-bold text-emerald-400">{approved.length}</span>
              <p className="text-xs text-muted-foreground mt-1">Actions permitted</p>
            </div>

            <div className={cn("instrument-card p-5", denied.length > 0 && "status-danger")}>
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Denied</span>
                <XCircle className="w-4 h-4 text-red-400" />
              </div>
              <span className={cn("font-mono text-2xl font-bold", denied.length > 0 ? "text-red-400" : "text-foreground")}>{denied.length}</span>
              <p className="text-xs text-muted-foreground mt-1">Actions blocked</p>
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
            placeholder="Search approvals..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="h-8 w-full pl-8 pr-3 text-xs bg-surface-1 border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-cordum"
          />
        </div>
        <div className="flex items-center gap-1 bg-surface-1 border border-border rounded-md p-0.5">
          {[
            { id: "pending", label: "Pending", count: pending.length },
            { id: "approved", label: "Approved", count: approved.length },
            { id: "denied", label: "Denied", count: denied.length },
            { id: "all", label: "All", count: all.length },
          ].map((tab) => (
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

      {/* Approval Cards — showcase style */}
      {isLoading ? (
        <SkeletonTable rows={5} />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={<UserCheck className="w-5 h-5" />}
          title={activeTab === "pending" ? "No pending approvals" : "No approvals found"}
          description={activeTab === "pending" ? "All clear — no actions waiting for review" : "Try adjusting your search or filters"}
        />
      ) : (
        <div className="space-y-3">
          {filtered.map((approval) => (
            <motion.div
              key={approval.id}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              className={cn(
                "instrument-card p-5 cursor-pointer",
                approval.status === "pending" && "status-warning",
                approval.status === "denied" && "status-danger",
              )}
              onClick={() => setSelectedApproval(approval)}
            >
              <div className="flex items-start justify-between">
                <div className="flex-1">
                  <div className="flex items-center gap-3 mb-2">
                    <span className="font-mono text-sm text-cordum">{approval.id.slice(0, 16)}</span>
                    <StatusBadge variant={approvalStatusVariant(approval.status)} dot pulse={approval.status === "pending"}>
                      {approval.status}
                    </StatusBadge>
                    <span className="text-xs text-muted-foreground">
                      {approval.requestedAt ? formatRelativeTime(approval.requestedAt) : "—"}
                    </span>
                  </div>
                  <h3 className="font-display font-semibold text-foreground">
                    {approval.topic || "Approval Request"} — <span className="font-mono text-sm">{approval.jobId?.slice(0, 8) || approval.id.slice(0, 8)}</span>
                  </h3>
                </div>
                {approval.status === "pending" && (
                  <div className="flex gap-2 ml-4 shrink-0">
                    <Button
                      size="sm"
                      variant="danger"
                      loading={approveMutation.isPending}
                      onClick={(e) => {
                        e.stopPropagation();
                        approveMutation.mutate({ id: approval.id, decision: "deny" });
                      }}
                    >
                      <XCircle className="w-3.5 h-3.5 mr-1" />
                      Deny
                    </Button>
                    <Button
                      size="sm"
                      variant="primary"
                      loading={approveMutation.isPending}
                      onClick={(e) => {
                        e.stopPropagation();
                        approveMutation.mutate({ id: approval.id, decision: "approve" });
                      }}
                    >
                      <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
                      Approve
                    </Button>
                  </div>
                )}
              </div>
            </motion.div>
          ))}
        </div>
      )}

      {/* Detail Drawer */}
      {selectedApproval && (
        <>
          <div className="fixed inset-0 bg-black/40 z-40" onClick={() => setSelectedApproval(null)} />
          <motion.div
            initial={{ x: 440 }}
            animate={{ x: 0 }}
            transition={{ type: "spring", stiffness: 300, damping: 30 }}
            className="fixed inset-y-0 right-0 w-[440px] bg-surface-1 border-l border-border shadow-2xl z-50 overflow-y-auto"
          >
            <div className="p-5 border-b border-border flex items-center justify-between">
              <div>
                <h2 className="font-display font-semibold text-sm text-foreground">Approval Detail</h2>
                <p className="text-xs text-muted-foreground font-mono mt-0.5">{selectedApproval.id}</p>
              </div>
              <button
                onClick={() => setSelectedApproval(null)}
                className="p-1.5 rounded-md hover:bg-surface-2 text-muted-foreground hover:text-foreground transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
            <div className="p-5 space-y-5">
              <StatusBadge variant={approvalStatusVariant(selectedApproval.status)} dot pulse={selectedApproval.status === "pending"}>
                {selectedApproval.status}
              </StatusBadge>
              <dl className="space-y-3">
                {[
                  ["Topic", selectedApproval.topic],
                  ["Job ID", selectedApproval.jobId],
                  ["Requested", selectedApproval.requestedAt ? formatRelativeTime(selectedApproval.requestedAt) : "—"],
                  ["Decided By", selectedApproval.actor],
                  ["Reason", selectedApproval.reason],
                ].map(([label, value]) => (
                  <div key={label as string}>
                    <dt className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-0.5">{label}</dt>
                    <dd className="text-sm text-foreground">{(value as string) || "—"}</dd>
                  </div>
                ))}
              </dl>
              {selectedApproval.jobContext && (
                <div>
                  <p className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground mb-1">Context</p>
                  <div className="rounded-md bg-surface-2/50 border border-border p-3 font-mono text-xs text-foreground overflow-auto max-h-[200px]">
                    <pre>{JSON.stringify(selectedApproval.jobContext, null, 2)}</pre>
                  </div>
                </div>
              )}
              {selectedApproval.status === "pending" && (
                <div className="flex gap-2 pt-2">
                  <Button
                    variant="primary"
                    className="flex-1"
                    loading={approveMutation.isPending}
                    onClick={() => approveMutation.mutate({ id: selectedApproval.id, decision: "approve" })}
                  >
                    <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
                    Approve
                  </Button>
                  <Button
                    variant="danger"
                    className="flex-1"
                    loading={approveMutation.isPending}
                    onClick={() => approveMutation.mutate({ id: selectedApproval.id, decision: "deny" })}
                  >
                    <XCircle className="w-3.5 h-3.5 mr-1" />
                    Deny
                  </Button>
                </div>
              )}
            </div>
          </motion.div>
        </>
      )}
    </div>
  );
}
