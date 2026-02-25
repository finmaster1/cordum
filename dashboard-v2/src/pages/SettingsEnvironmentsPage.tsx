import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Globe, Plus, Edit3, Trash2, X, Server, Database, Shield } from "lucide-react";

const ENVIRONMENTS = [
  { name: "production", description: "Live production environment", workers: 6, activeJobs: 124, policies: 12, busUrl: "nats://prod-bus:4222", redisUrl: "redis://prod-redis:6379", color: "var(--cordum)" },
  { name: "staging", description: "Pre-production testing environment", workers: 3, activeJobs: 47, policies: 12, busUrl: "nats://staging-bus:4222", redisUrl: "redis://staging-redis:6379", color: "#60a5fa" },
  { name: "sandbox", description: "Development and experimentation", workers: 1, activeJobs: 8, policies: 5, busUrl: "nats://sandbox-bus:4222", redisUrl: "redis://sandbox-redis:6379", color: "#a78bfa" },
];

export default function SettingsEnvironmentsPage() {
  const [showAdd, setShowAdd] = useState(false);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS</p>
          <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Environments</h1>
          <p className="text-sm text-[var(--muted-foreground)] mt-1">Manage deployment environments.</p>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center gap-2 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors"
        >
          <Plus className="w-4 h-4" /> Add Environment
        </button>
      </div>

      <div className="space-y-4">
        {ENVIRONMENTS.map((env, i) => (
          <motion.div
            key={env.name}
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.08 }}
            className="instrument-card"
            style={{ "--instrument-accent": env.color } as any}
          >
            <div className="p-5 space-y-4">
              <div className="flex items-start justify-between">
                <div>
                  <h3 className="text-lg font-display font-semibold text-[var(--foreground)]">{env.name}</h3>
                  <p className="text-sm text-[var(--muted-foreground)]">{env.description}</p>
                </div>
                <div className="flex items-center gap-2">
                  <button className="p-2 hover:bg-[var(--surface-2)] rounded-md transition-colors">
                    <Edit3 className="w-4 h-4 text-[var(--muted-foreground)]" />
                  </button>
                  <button className="p-2 hover:bg-red-400/10 rounded-md transition-colors">
                    <Trash2 className="w-4 h-4 text-red-400" />
                  </button>
                </div>
              </div>
              <div className="grid grid-cols-3 gap-4">
                <div className="flex items-center gap-2">
                  <Server className="w-4 h-4 text-[var(--muted-foreground)]" />
                  <div>
                    <p className="text-xs text-[var(--muted-foreground)]">Workers</p>
                    <p className="text-sm font-mono font-semibold text-[var(--foreground)]">{env.workers}</p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Database className="w-4 h-4 text-[var(--muted-foreground)]" />
                  <div>
                    <p className="text-xs text-[var(--muted-foreground)]">Active Jobs</p>
                    <p className="text-sm font-mono font-semibold text-[var(--foreground)]">{env.activeJobs}</p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <Shield className="w-4 h-4 text-[var(--muted-foreground)]" />
                  <div>
                    <p className="text-xs text-[var(--muted-foreground)]">Policies</p>
                    <p className="text-sm font-mono font-semibold text-[var(--foreground)]">{env.policies}</p>
                  </div>
                </div>
              </div>
              <div className="pt-3 border-t border-[var(--border)] space-y-1">
                <p className="text-xs text-[var(--muted-foreground)]">Bus: <span className="font-mono text-[var(--foreground)]">{env.busUrl}</span></p>
                <p className="text-xs text-[var(--muted-foreground)]">Redis: <span className="font-mono text-[var(--foreground)]">{env.redisUrl}</span></p>
              </div>
            </div>
          </motion.div>
        ))}
      </div>
    </div>
  );
}
