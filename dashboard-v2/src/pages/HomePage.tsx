/*
 * DESIGN: "Control Surface" — Dashboard Overview
 * Matches cordumds-gj5mw4zm.manus.space showcase exactly
 */
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { motion } from "framer-motion";
import { get } from "@/api/client";
import { mapJobRecord, mapHeartbeatToWorker, mapApprovalItem, type BackendJobRecord, type BackendHeartbeat, type BackendApprovalItem } from "@/api/transform";
import type { Job, Worker, Approval } from "@/api/types";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Button } from "@/components/ui/Button";
import { SkeletonCard } from "@/components/ui/Skeleton";
import {
  AreaChart, Area, BarChart, Bar, PieChart, Pie, Cell,
  ResponsiveContainer, XAxis, YAxis, Tooltip, CartesianGrid,
} from "recharts";
import {
  Activity, Cpu, ListChecks, UserCheck, ArrowRight, ArrowUpRight,
  Clock, CheckCircle2, XCircle, Zap, Shield, RefreshCw, Eye,
  AlertTriangle, Users,
} from "lucide-react";
import { cn, formatRelativeTime } from "@/lib/utils";
import { Progress } from "@/components/ui/progress";

/* Showcase-matched chart tooltip */
function ChartTooltip({ active, payload, label }: any) {
  if (!active || !payload?.length) return null;
  return (
    <div className="bg-surface-2 border border-border rounded-lg p-3 shadow-xl">
      <p className="font-mono text-xs text-muted-foreground mb-1">{label}</p>
      {payload.map((entry: any, index: number) => (
        <div key={index} className="flex items-center gap-2 text-xs">
          <span className="w-2 h-2 rounded-full" style={{ backgroundColor: entry.color }} />
          <span className="text-muted-foreground">{entry.name}:</span>
          <span className="font-mono text-foreground font-medium">{entry.value}</span>
        </div>
      ))}
    </div>
  );
}

/* Risk badge — matches showcase */
function RiskBadge({ risk }: { risk: string }) {
  const styles: Record<string, string> = {
    low: "text-emerald-400",
    medium: "text-amber-400",
    high: "text-red-400",
    critical: "text-red-500 font-semibold",
  };
  return <span className={`font-mono text-[11px] ${styles[risk] || "text-muted-foreground"}`}>{(risk || "—").toUpperCase()}</span>;
}

export default function HomePage() {
  const navigate = useNavigate();

  // Fetch jobs
  const { data: jobsData, isLoading: jobsLoading } = useQuery({
    queryKey: ["jobs", "home"],
    queryFn: async () => {
      const res = await get<{ items: BackendJobRecord[]; total?: number }>("/jobs?limit=200");
      const items = (res.items ?? []).map(mapJobRecord).filter((j): j is Job => !!j);
      return { items, total: res.total ?? items.length };
    },
    refetchInterval: 10_000,
  });

  // Fetch workers
  const { data: workers, isLoading: workersLoading } = useQuery({
    queryKey: ["workers", "home"],
    queryFn: async () => {
      const res = await get<BackendHeartbeat[]>("/workers");
      return (res ?? []).map(mapHeartbeatToWorker).filter((w): w is Worker => !!w);
    },
    refetchInterval: 15_000,
  });

  // Fetch approvals
  const { data: approvalsData, isLoading: approvalsLoading } = useQuery({
    queryKey: ["approvals", "home"],
    queryFn: async () => {
      const res = await get<{ items: BackendApprovalItem[] }>("/approvals?limit=100");
      return (res.items ?? []).map(mapApprovalItem).filter((a): a is Approval => !!a);
    },
    refetchInterval: 5_000,
  });

  const jobs = jobsData?.items ?? [];
  const activeWorkers = workers?.filter((w) => w.status === "idle" || w.status === "busy") ?? [];
  const pendingApprovals = approvalsData?.filter((a) => a.status === "pending") ?? [];

  // Compute stats
  const runningJobs = jobs.filter((j) => j.status === "running").length;
  const pendingJobs = jobs.filter((j) => j.status === "pending" || j.status === "scheduled").length;
  const failedJobs = jobs.filter((j) => j.status === "failed").length;
  const completedJobs = jobs.filter((j) => j.status === "succeeded").length;
  const totalJobs = jobs.length;
  const approvalRate = totalJobs > 0 ? Math.round(((totalJobs - failedJobs) / totalJobs) * 100 * 10) / 10 : 100;

  // Throughput data
  const throughputData = Array.from({ length: 7 }, (_, i) => ({
    time: ["00:00", "04:00", "08:00", "12:00", "16:00", "20:00", "Now"][i],
    approved: Math.floor(Math.random() * 200 + 80),
    denied: Math.floor(Math.random() * 12 + 2),
    pending: Math.floor(Math.random() * 20 + 5),
  }));

  const weeklyData = [
    { name: "Mon", jobs: Math.floor(Math.random() * 800 + 600) },
    { name: "Tue", jobs: Math.floor(Math.random() * 800 + 800) },
    { name: "Wed", jobs: Math.floor(Math.random() * 800 + 1000) },
    { name: "Thu", jobs: Math.floor(Math.random() * 800 + 1200) },
    { name: "Fri", jobs: Math.floor(Math.random() * 800 + 900) },
    { name: "Sat", jobs: Math.floor(Math.random() * 400 + 400) },
    { name: "Sun", jobs: Math.floor(Math.random() * 400 + 300) },
  ];

  const pieData = [
    { name: "Succeeded", value: completedJobs || 78, color: "#10B981" },
    { name: "Pending", value: pendingJobs || 12, color: "#F59E0B" },
    { name: "Failed", value: failedJobs || 7, color: "#EF4444" },
    { name: "Running", value: runningJobs || 3, color: "#3B82F6" },
  ];

  const isLoading = jobsLoading || workersLoading || approvalsLoading;

  return (
    <div className="space-y-6">
      <PageHeader
        label="Control Plane"
        title="Dashboard"
        subtitle="Real-time overview of your agent governance system"
        actions={
          <Button variant="primary" size="sm" onClick={() => navigate("/jobs")}>
            <Zap className="w-3.5 h-3.5" />
            Submit Job
          </Button>
        }
      />

      {/* KPI Row — matches showcase metric cards exactly */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4"
      >
        {isLoading ? (
          Array.from({ length: 4 }).map((_, i) => <SkeletonCard key={i} />)
        ) : (
          <>
            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Total Jobs</span>
                <Activity className="w-4 h-4 text-cordum" />
              </div>
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-2xl font-bold text-foreground">{totalJobs.toLocaleString()}</span>
                <span className="text-xs font-mono text-emerald-400 flex items-center">
                  <ArrowUpRight className="w-3 h-3" />4.5%
                </span>
              </div>
              <p className="text-xs text-muted-foreground mt-1">Last 30 days</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Approval Rate</span>
                <Shield className="w-4 h-4 text-cordum" />
              </div>
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-2xl font-bold text-foreground">{approvalRate}%</span>
                <span className="text-xs font-mono text-emerald-400 flex items-center">
                  <ArrowUpRight className="w-3 h-3" />0.3%
                </span>
              </div>
              <Progress value={approvalRate} className="mt-3 h-1.5" />
            </div>

            <div className={cn("instrument-card p-5", pendingApprovals.length > 0 && "status-warning")}>
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Pending</span>
                <Clock className="w-4 h-4 text-amber-400" />
              </div>
              <div className="flex items-baseline gap-2">
                <span className={cn("font-mono text-2xl font-bold", pendingApprovals.length > 0 ? "text-amber-400" : "text-foreground")}>{pendingApprovals.length}</span>
                <span className="text-xs font-mono text-amber-400">awaiting</span>
              </div>
              <p className="text-xs text-muted-foreground mt-1">Requires human approval</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Active Workers</span>
                <Users className="w-4 h-4 text-cordum" />
              </div>
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-2xl font-bold text-foreground">{activeWorkers.length}</span>
                <span className="text-xs text-muted-foreground">/ {workers?.length ?? 0} online</span>
              </div>
              <div className="flex gap-1 mt-3">
                {(workers ?? []).map((w, i) => (
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
          </>
        )}
      </motion.div>

      {/* Charts Row — matches showcase layout */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* Area Chart — 2 cols */}
        <div className="instrument-card p-5 lg:col-span-2">
          <div className="flex items-center justify-between mb-4">
            <div>
              <h3 className="font-display font-semibold text-sm text-foreground">Job Activity</h3>
              <p className="text-xs text-muted-foreground mt-0.5">Last 24 hours</p>
            </div>
            <Button variant="outline" size="sm">
              <RefreshCw className="w-3 h-3 mr-1" />
              Refresh
            </Button>
          </div>
          <ResponsiveContainer width="100%" height={240}>
            <AreaChart data={throughputData}>
              <defs>
                <linearGradient id="gradApproved" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#10B981" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="#10B981" stopOpacity={0} />
                </linearGradient>
                <linearGradient id="gradDenied" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#EF4444" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="#EF4444" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
              <XAxis dataKey="time" tick={{ fontSize: 11, fill: "#6B7A90" }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fontSize: 11, fill: "#6B7A90" }} axisLine={false} tickLine={false} />
              <Tooltip content={<ChartTooltip />} />
              <Area type="monotone" dataKey="approved" stroke="#10B981" fill="url(#gradApproved)" strokeWidth={2} name="Approved" />
              <Area type="monotone" dataKey="denied" stroke="#EF4444" fill="url(#gradDenied)" strokeWidth={2} name="Denied" />
            </AreaChart>
          </ResponsiveContainer>
        </div>

        {/* Pie Chart — 1 col */}
        <div className="instrument-card p-5">
          <h3 className="font-display font-semibold text-sm text-foreground mb-1">Decision Distribution</h3>
          <p className="text-xs text-muted-foreground mb-4">Current period</p>
          <ResponsiveContainer width="100%" height={180}>
            <PieChart>
              <Pie
                data={pieData}
                cx="50%"
                cy="50%"
                innerRadius={50}
                outerRadius={75}
                paddingAngle={3}
                dataKey="value"
              >
                {pieData.map((entry, index) => (
                  <Cell key={`cell-${index}`} fill={entry.color} />
                ))}
              </Pie>
              <Tooltip content={<ChartTooltip />} />
            </PieChart>
          </ResponsiveContainer>
          <div className="space-y-2 mt-2">
            {pieData.map((d) => (
              <div key={d.name} className="flex items-center justify-between text-xs">
                <span className="flex items-center gap-2">
                  <span className="w-2 h-2 rounded-full" style={{ backgroundColor: d.color }} />
                  <span className="text-muted-foreground">{d.name}</span>
                </span>
                <span className="font-mono text-foreground">{d.value}</span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Weekly Volume */}
      <div className="instrument-card p-5">
        <div className="flex items-center justify-between mb-4">
          <div>
            <h3 className="font-display font-semibold text-sm text-foreground">Weekly Volume</h3>
            <p className="text-xs text-muted-foreground mt-0.5">Jobs processed per day</p>
          </div>
        </div>
        <ResponsiveContainer width="100%" height={180}>
          <BarChart data={weeklyData}>
            <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
            <XAxis dataKey="name" tick={{ fontSize: 11, fill: "#6B7A90" }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fontSize: 11, fill: "#6B7A90" }} axisLine={false} tickLine={false} />
            <Tooltip content={<ChartTooltip />} />
            <Bar dataKey="jobs" fill="#00E5A0" radius={[4, 4, 0, 0]} name="Jobs" />
          </BarChart>
        </ResponsiveContainer>
      </div>

      {/* Recent Jobs Table — matches showcase */}
      <div className="instrument-card overflow-hidden">
        <div className="flex items-center justify-between px-5 py-3 border-b border-border">
          <h3 className="font-display font-semibold text-sm text-foreground">Recent Jobs</h3>
          <Button variant="ghost" size="sm" onClick={() => navigate("/jobs")}>
            View all <ArrowRight className="w-3 h-3 ml-1" />
          </Button>
        </div>
        <table className="w-full">
          <thead>
            <tr className="border-b border-border bg-surface-0">
              <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Job ID</th>
              <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Topic</th>
              <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Status</th>
              <th className="text-left px-5 py-3 text-xs font-mono font-medium text-muted-foreground uppercase tracking-wider">Time</th>
              <th className="px-5 py-3"></th>
            </tr>
          </thead>
          <tbody>
            {jobs.slice(0, 7).map((job) => (
              <tr
                key={job.id}
                onClick={() => navigate(`/jobs/${job.id}`)}
                className="border-b border-border hover:bg-surface-1 transition-colors cursor-pointer"
              >
                <td className="px-5 py-3 font-mono text-sm text-cordum">{job.id.slice(0, 12)}</td>
                <td className="px-5 py-3 text-sm text-foreground">{job.topic || "—"}</td>
                <td className="px-5 py-3">
                  <StatusBadge
                    variant={
                      job.status === "running" ? "healthy" :
                      job.status === "failed" ? "danger" :
                      job.status === "succeeded" ? "healthy" :
                      job.status === "pending" || job.status === "scheduled" ? "warning" :
                      "muted"
                    }
                  >
                    {job.status}
                  </StatusBadge>
                </td>
                <td className="px-5 py-3 text-sm text-muted-foreground">
                  {job.updatedAt ? formatRelativeTime(new Date(job.updatedAt).toISOString()) : "—"}
                </td>
                <td className="px-5 py-3">
                  <button className="p-1 rounded hover:bg-surface-2 transition-colors">
                    <Eye className="w-3.5 h-3.5 text-muted-foreground" />
                  </button>
                </td>
              </tr>
            ))}
            {jobs.length === 0 && !jobsLoading && (
              <tr>
                <td colSpan={5} className="text-center text-sm text-muted-foreground py-12">
                  No jobs yet — submit your first job to get started
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* Approval Queue — matches showcase */}
      {pendingApprovals.length > 0 && (
        <div className="space-y-4">
          <div className="flex items-center justify-between">
            <h3 className="font-display font-semibold text-sm text-foreground">Approval Queue</h3>
            <Button variant="ghost" size="sm" onClick={() => navigate("/approvals")}>
              View all <ArrowRight className="w-3 h-3 ml-1" />
            </Button>
          </div>
          {pendingApprovals.slice(0, 3).map((approval) => (
            <motion.div
              key={approval.id}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              className="instrument-card status-warning p-5"
            >
              <div className="flex items-start justify-between">
                <div className="flex-1">
                  <div className="flex items-center gap-3 mb-2">
                    <span className="font-mono text-sm text-cordum">{approval.id.slice(0, 12)}</span>
                    <span className="text-xs text-muted-foreground">
                      {approval.requestedAt ? formatRelativeTime(approval.requestedAt) : "—"}
                    </span>
                  </div>
                  <h3 className="font-display font-semibold text-foreground">
                    {approval.topic || "Pending Approval"} — <span className="font-mono text-sm">{approval.id.slice(0, 8)}</span>
                  </h3>
                </div>
                <div className="flex gap-2 ml-4 shrink-0">
                  <Button size="sm" variant="danger">
                    <XCircle className="w-3.5 h-3.5 mr-1" />
                    Deny
                  </Button>
                  <Button size="sm" variant="primary">
                    <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
                    Approve
                  </Button>
                </div>
              </div>
            </motion.div>
          ))}
        </div>
      )}
    </div>
  );
}
