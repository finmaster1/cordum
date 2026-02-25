/*
 * DESIGN: "Control Surface" — Policy Studio
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { motion } from "framer-motion";
import { get } from "@/api/client";
import { PageHeader } from "@/components/layout/PageHeader";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { Button } from "@/components/ui/Button";
import { SkeletonCard } from "@/components/ui/Skeleton";
import { AreaChart, Area, ResponsiveContainer, CartesianGrid, XAxis, YAxis, Tooltip } from "recharts";
import { Shield, Plus, ArrowRight, Activity, History, FlaskConical, ArrowUpRight } from "lucide-react";
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

export default function PoliciesOverviewPage() {
  const navigate = useNavigate();

  const { data: rules, isLoading } = useQuery({
    queryKey: ["policy-rules"],
    queryFn: async () => {
      const res = await get<{ items: any[] }>("/policies/rules?limit=500");
      return res.items ?? [];
    },
  });

  const allRules = rules ?? [];
  const activeRules = allRules.filter((r) => r.enabled !== false);

  // Mock evaluation data
  const evalData = Array.from({ length: 7 }, (_, i) => ({
    time: ["00:00", "04:00", "08:00", "12:00", "16:00", "20:00", "Now"][i],
    allow: Math.floor(Math.random() * 80 + 20),
    deny: Math.floor(Math.random() * 10),
    escalate: Math.floor(Math.random() * 8),
  }));

  const quickLinks = [
    { label: "Policy Rules", desc: "Manage allow/deny/escalate rules", icon: Shield, path: "/policies/rules" },
    { label: "Rule Builder", desc: "Create new policy rules", icon: Plus, path: "/policies/rules/new" },
    { label: "Simulator", desc: "Test policies against payloads", icon: FlaskConical, path: "/policies/simulator" },
    { label: "History", desc: "View policy change log", icon: History, path: "/policies/history" },
    { label: "Analytics", desc: "Decision metrics & trends", icon: Activity, path: "/policies/analytics" },
  ];

  return (
    <div className="space-y-6">
      <PageHeader
        label="Safety"
        title="Policy Studio"
        subtitle="Manage safety policies governing agent actions"
        actions={
          <Button variant="primary" size="sm" onClick={() => navigate("/policies/rules/new")}>
            <Plus className="w-3 h-3 mr-1" />
            New Rule
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
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Total Rules</span>
                <Shield className="w-4 h-4 text-cordum" />
              </div>
              <span className="font-mono text-2xl font-bold text-foreground">{allRules.length}</span>
              <p className="text-xs text-muted-foreground mt-1">{activeRules.length} active</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Active</span>
                <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 status-pulse" />
              </div>
              <span className="font-mono text-2xl font-bold text-emerald-400">{activeRules.length}</span>
              <Progress value={allRules.length > 0 ? (activeRules.length / allRules.length) * 100 : 100} className="mt-3 h-1.5" />
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Evaluations (24h)</span>
                <Activity className="w-4 h-4 text-cordum" />
              </div>
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-2xl font-bold text-foreground">2.4k</span>
                <span className="text-xs font-mono text-emerald-400 flex items-center">
                  <ArrowUpRight className="w-3 h-3" />15%
                </span>
              </div>
              <p className="text-xs text-muted-foreground mt-1">vs yesterday</p>
            </div>

            <div className="instrument-card p-5">
              <div className="flex items-center justify-between mb-3">
                <span className="text-xs font-mono text-muted-foreground uppercase tracking-wider">Deny Rate</span>
              </div>
              <div className="flex items-baseline gap-2">
                <span className="font-mono text-2xl font-bold text-foreground">3.2%</span>
                <span className="text-xs font-mono text-emerald-400 flex items-center">
                  <ArrowUpRight className="w-3 h-3" />-0.5%
                </span>
              </div>
              <p className="text-xs text-muted-foreground mt-1">Trending down</p>
            </div>
          </>
        )}
      </motion.div>

      {/* Evaluation Chart — showcase style */}
      <div className="instrument-card p-5">
        <div className="flex items-center justify-between mb-4">
          <div>
            <h3 className="font-display font-semibold text-sm text-foreground">Policy Evaluations</h3>
            <p className="text-xs text-muted-foreground mt-0.5">Last 24 hours</p>
          </div>
        </div>
        <ResponsiveContainer width="100%" height={240}>
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
            <Area type="monotone" dataKey="escalate" stroke="#F59E0B" fill="none" strokeWidth={1.5} name="Escalate" strokeDasharray="4 2" />
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {/* Quick Links — showcase card style */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
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
    </div>
  );
}
