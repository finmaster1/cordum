import { motion } from "framer-motion";
import { Activity, CheckCircle2, AlertTriangle, XCircle, Cpu, HardDrive, Wifi, Clock } from "lucide-react";
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";

const SERVICES = [
  { name: "API Gateway", status: "healthy", uptime: "14d 6h 32m", latency: { p50: 2, p95: 8, p99: 15 }, version: "2.4.1" },
  { name: "Scheduler", status: "healthy", uptime: "14d 6h 32m", latency: { p50: 1, p95: 4, p99: 9 }, version: "2.4.1" },
  { name: "Safety Kernel", status: "healthy", uptime: "14d 6h 30m", latency: { p50: 2, p95: 5, p99: 11 }, version: "2.4.1" },
  { name: "NATS Bus", status: "healthy", uptime: "28d 12h 5m", latency: { p50: 0.5, p95: 1, p99: 2 }, version: "2.10.4" },
  { name: "Redis", status: "degraded", uptime: "7d 3h 12m", latency: { p50: 1, p95: 12, p99: 45 }, version: "7.2.4" },
  { name: "Dashboard", status: "healthy", uptime: "14d 6h 32m", latency: { p50: 15, p95: 45, p99: 120 }, version: "2.0.0" },
];

const statusConfig: Record<string, { icon: any; color: string; bg: string }> = {
  healthy: { icon: CheckCircle2, color: "text-emerald-400", bg: "bg-emerald-400/10" },
  degraded: { icon: AlertTriangle, color: "text-amber-400", bg: "bg-amber-400/10" },
  down: { icon: XCircle, color: "text-red-400", bg: "bg-red-400/10" },
};

const cpuData = Array.from({ length: 20 }, (_, i) => ({
  time: `${i}m`,
  gateway: 15 + Math.random() * 10,
  scheduler: 20 + Math.random() * 15,
  kernel: 10 + Math.random() * 8,
}));

const memData = Array.from({ length: 20 }, (_, i) => ({
  time: `${i}m`,
  gateway: 45 + Math.random() * 5,
  scheduler: 55 + Math.random() * 8,
  kernel: 35 + Math.random() * 5,
}));

export default function SettingsHealthPage() {
  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">System Health</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Monitor system health and diagnostics.</p>
      </div>

      {/* Service Health Table */}
      <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="instrument-card overflow-hidden">
        <div className="px-5 py-3 border-b border-[var(--border)]">
          <h2 className="text-sm font-display font-semibold text-[var(--foreground)]">Service Health</h2>
        </div>
        <table className="w-full">
          <thead>
            <tr className="bg-[var(--surface-0)] border-b border-[var(--border)]">
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Service</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Status</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Uptime</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Latency (P50/P95/P99)</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Version</th>
            </tr>
          </thead>
          <tbody>
            {SERVICES.map((svc, i) => {
              const cfg = statusConfig[svc.status];
              const Icon = cfg.icon;
              return (
                <motion.tr
                  key={svc.name}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{ delay: i * 0.05 }}
                  className="border-b border-[var(--border)] hover:bg-[var(--surface-1)] transition-colors"
                >
                  <td className="px-4 py-3 text-sm font-medium text-[var(--foreground)]">{svc.name}</td>
                  <td className="px-4 py-3">
                    <span className={`inline-flex items-center gap-1.5 text-xs font-medium px-2 py-0.5 rounded-full ${cfg.color} ${cfg.bg}`}>
                      <Icon className="w-3 h-3" /> {svc.status}
                    </span>
                  </td>
                  <td className="px-4 py-3 text-xs font-mono text-[var(--foreground)]">{svc.uptime}</td>
                  <td className="px-4 py-3 text-xs font-mono text-[var(--muted-foreground)]">
                    {svc.latency.p50}ms / {svc.latency.p95}ms / {svc.latency.p99}ms
                  </td>
                  <td className="px-4 py-3 text-xs font-mono text-[var(--muted-foreground)]">{svc.version}</td>
                </motion.tr>
              );
            })}
          </tbody>
        </table>
      </motion.div>

      {/* Resource Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.1 }} className="instrument-card p-5">
          <h3 className="text-sm font-display font-semibold text-[var(--foreground)] mb-4">CPU Usage (%)</h3>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={cpuData}>
              <XAxis dataKey="time" tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} domain={[0, 100]} />
              <Tooltip contentStyle={{ background: "var(--surface-2)", border: "1px solid var(--border)", borderRadius: 8, fontSize: 12 }} />
              <Area type="monotone" dataKey="gateway" stroke="var(--cordum)" fill="var(--cordum)" fillOpacity={0.1} strokeWidth={1.5} />
              <Area type="monotone" dataKey="scheduler" stroke="#60a5fa" fill="#60a5fa" fillOpacity={0.1} strokeWidth={1.5} />
              <Area type="monotone" dataKey="kernel" stroke="#a78bfa" fill="#a78bfa" fillOpacity={0.1} strokeWidth={1.5} />
            </AreaChart>
          </ResponsiveContainer>
        </motion.div>

        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.15 }} className="instrument-card p-5">
          <h3 className="text-sm font-display font-semibold text-[var(--foreground)] mb-4">Memory Usage (%)</h3>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={memData}>
              <XAxis dataKey="time" tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} />
              <YAxis tick={{ fill: "var(--muted-foreground)", fontSize: 10 }} axisLine={false} tickLine={false} domain={[0, 100]} />
              <Tooltip contentStyle={{ background: "var(--surface-2)", border: "1px solid var(--border)", borderRadius: 8, fontSize: 12 }} />
              <Area type="monotone" dataKey="gateway" stroke="var(--cordum)" fill="var(--cordum)" fillOpacity={0.1} strokeWidth={1.5} />
              <Area type="monotone" dataKey="scheduler" stroke="#60a5fa" fill="#60a5fa" fillOpacity={0.1} strokeWidth={1.5} />
              <Area type="monotone" dataKey="kernel" stroke="#a78bfa" fill="#a78bfa" fillOpacity={0.1} strokeWidth={1.5} />
            </AreaChart>
          </ResponsiveContainer>
        </motion.div>
      </div>
    </div>
  );
}
