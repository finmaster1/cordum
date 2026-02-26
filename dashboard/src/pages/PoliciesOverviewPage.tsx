/*
 * PoliciesOverviewPage — Policy Studio landing page.
 * Wrapped in PolicyStudioLayout. Shows KPIs, evaluation chart, quick links.
 */
import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { motion } from "framer-motion";
import { PolicyStudioLayout } from "@/components/layout/PolicyStudioLayout";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { SkeletonCard } from "@/components/ui/Skeleton";
import { Progress } from "@/components/ui/progress";
import {
  AreaChart, Area, ResponsiveContainer, CartesianGrid, XAxis, YAxis, Tooltip,
} from "recharts";
import {
  Shield, ShieldCheck, ShieldAlert, Package, Wrench, FlaskConical,
  Activity, History, ArrowRight, ArrowUpRight, GitBranch,
} from "lucide-react";
import { usePolicyRules, usePolicyBundles, usePolicyAudit } from "@/hooks/usePolicies";
import { useOutputRules } from "@/hooks/useOutputRules";
import { ChartTooltip } from "@/components/ui/ChartTooltip";

export default function PoliciesOverviewPage() {
  const navigate = useNavigate();
  const { data: rulesData, isLoading: rulesLoading } = usePolicyRules();
  const { data: bundlesData } = usePolicyBundles();
  const { data: auditData } = usePolicyAudit();
  const { data: outputRulesData } = useOutputRules();

  const inputRules = rulesData?.items ?? [];
  const bundles = bundlesData?.items ?? [];
  const auditEntries = auditData?.items ?? [];
  const outputRules = outputRulesData ?? [];

  const activeInputRules = inputRules.filter((r) => r.enabled !== false);
  const publishedBundles = bundles.filter((b) => b.status === "published");

  // Derive evaluation stats from audit
  const { total, blockRate, approvalRate } = useMemo(() => {
    const t = auditEntries.length;
    const blocked = auditEntries.filter((e) => {
      const a = e.action.toLowerCase();
      return a.includes("deny") || a.includes("block") || a.includes("reject");
    }).length;
    const approvals = auditEntries.filter((e) => {
      const a = e.action.toLowerCase();
      return a.includes("approval") || a.includes("escalat");
    }).length;
    return {
      total: t,
      blockRate: t > 0 ? ((blocked / t) * 100).toFixed(1) : "0.0",
      approvalRate: t > 0 ? ((approvals / t) * 100).toFixed(1) : "0.0",
    };
  }, [auditEntries]);

  const evalData = useMemo(() => {
    const labels = ["00:00 UTC", "04:00 UTC", "08:00 UTC", "12:00 UTC", "16:00 UTC", "20:00 UTC", "Now"];
    const buckets = labels.map((time) => ({ time, allow: 0, deny: 0, approval: 0 }));
    for (const entry of auditEntries) {
      const hour = new Date(entry.timestamp).getUTCHours();
      const idx = Math.min(Math.floor(hour / 4), 5);
      const a = entry.action.toLowerCase();
      if (a.includes("deny") || a.includes("block") || a.includes("reject")) {
        buckets[idx].deny++;
      } else if (a.includes("approval") || a.includes("warn")) {
        buckets[idx].approval++;
      } else {
        buckets[idx].allow++;
      }
    }
    return buckets;
  }, [auditEntries]);

  const quickLinks = [
    { label: "Input Policy", desc: "Global, workflow-scoped, and tenant rules", icon: ShieldCheck, path: "/policies/input" },
    { label: "Output Policy", desc: "Scanners, redaction, and output rules", icon: ShieldAlert, path: "/policies/output" },
    { label: "Bundles", desc: "Manage policy bundles and snapshots", icon: Package, path: "/policies/bundles" },
    { label: "Builder", desc: "Visual rule builder with WHERE/WHEN/THEN", icon: Wrench, path: "/policies/builder" },
    { label: "Simulator", desc: "Test policies and explain decisions", icon: FlaskConical, path: "/policies/simulator" },
    { label: "Hierarchy", desc: "Visualize the scope cascade", icon: GitBranch, path: "/policies/hierarchy" },
    { label: "Analytics", desc: "Decision metrics and trends", icon: Activity, path: "/policies/analytics" },
    { label: "History", desc: "Audit trail and diff viewer", icon: History, path: "/policies/history" },
  ];

  return (
    <PolicyStudioLayout>
      {/* KPI Row */}
      <motion.div
        initial={{ opacity: 0, y: 12 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="grid grid-cols-2 lg:grid-cols-5 gap-4 mb-6"
      >
        {rulesLoading ? (
          Array.from({ length: 5 }).map((_, i) => <SkeletonCard key={i} />)
        ) : (
          <>
            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-widest">Input Rules</span>
                <ShieldCheck className="w-4 h-4 text-cordum" />
              </div>
              <span className="font-mono text-3xl font-bold text-foreground">{inputRules.length}</span>
              <p className="text-xs text-muted-foreground mt-1">{activeInputRules.length} active</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-widest">Output Rules</span>
                <ShieldAlert className="w-4 h-4 text-cordum" />
              </div>
              <span className="font-mono text-3xl font-bold text-foreground">{outputRules.length}</span>
              <p className="text-xs text-muted-foreground mt-1">scanners + rules</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-widest">Bundles</span>
                <Package className="w-4 h-4 text-cordum" />
              </div>
              <span className="font-mono text-3xl font-bold text-foreground">{bundles.length}</span>
              <p className="text-xs text-muted-foreground mt-1">{publishedBundles.length} published</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-widest">Deny Rate</span>
              </div>
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-3xl font-bold text-foreground">{blockRate}%</span>
              </div>
              <p className="text-xs text-muted-foreground mt-1">of {total} evaluations</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-widest">Approval Rate</span>
              </div>
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-3xl font-bold text-foreground">{approvalRate}%</span>
              </div>
              <p className="text-xs text-muted-foreground mt-1">require human review</p>
            </div>
          </>
        )}
      </motion.div>

      {/* Evaluation Chart */}
      <div className="instrument-card p-5 mb-6">
        <div className="flex items-center justify-between mb-4">
          <div>
            <h3 className="font-display font-semibold text-sm text-foreground">Policy Evaluations</h3>
            <p className="text-xs text-muted-foreground mt-0.5">Last 24 hours — input + output combined</p>
          </div>
        </div>
        <ResponsiveContainer width="100%" height={200}>
          <AreaChart data={evalData}>
            <defs>
              <linearGradient id="allowGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#10B981" stopOpacity={0.3} />
                <stop offset="95%" stopColor="#10B981" stopOpacity={0} />
              </linearGradient>
              <linearGradient id="denyGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#EF4444" stopOpacity={0.3} />
                <stop offset="95%" stopColor="#EF4444" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
            <XAxis dataKey="time" tick={{ fontSize: 11, fill: "#6B7A90" }} axisLine={false} tickLine={false} />
            <YAxis tick={{ fontSize: 11, fill: "#6B7A90" }} axisLine={false} tickLine={false} />
            <Tooltip content={<ChartTooltip />} />
            <Area type="monotone" dataKey="allow" stroke="#10B981" fill="url(#allowGrad)" strokeWidth={2} name="Allow" />
            <Area type="monotone" dataKey="deny" stroke="#EF4444" fill="url(#denyGrad)" strokeWidth={2} name="Deny" />
            <Area type="monotone" dataKey="approval" stroke="#F59E0B" fill="none" strokeWidth={1.5} name="Approval" strokeDasharray="4 2" />
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {/* Quick Links */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-3">
        {quickLinks.map((link) => (
          <motion.div
            key={link.path}
            whileHover={{ y: -2 }}
            className="instrument-card p-5 cursor-pointer hover:border-cordum/30 transition-colors"
            onClick={() => navigate(link.path)}
          >
            <div className="flex items-center gap-3">
              <div className="w-9 h-9 rounded-lg bg-cordum/10 border border-cordum/20 flex items-center justify-center text-cordum shrink-0">
                <link.icon className="w-4 h-4" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-display font-semibold text-foreground">{link.label}</p>
                <p className="text-xs text-muted-foreground">{link.desc}</p>
              </div>
              <ArrowRight className="w-4 h-4 text-muted-foreground shrink-0" />
            </div>
          </motion.div>
        ))}
      </div>
    </PolicyStudioLayout>
  );
}
