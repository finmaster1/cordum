import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Key, Plus, Copy, Eye, EyeOff, Trash2, X, AlertTriangle, CheckCircle2 } from "lucide-react";

const API_KEYS = [
  { name: "Production API", prefix: "ck_live_8a2f...", role: "Admin", created: "2026-01-15", lastUsed: "2m ago", expires: "Never" },
  { name: "CI/CD Pipeline", prefix: "ck_live_9b3e...", role: "Operator", created: "2026-02-01", lastUsed: "1h ago", expires: "2026-05-01" },
  { name: "Monitoring Service", prefix: "ck_live_4c7d...", role: "Viewer", created: "2026-02-10", lastUsed: "30m ago", expires: "2026-08-10" },
  { name: "Dev Testing", prefix: "ck_test_2e1f...", role: "Admin", created: "2026-02-20", lastUsed: "Never", expires: "2026-03-20" },
];

const roleColors: Record<string, string> = {
  Admin: "text-red-400 bg-red-400/10",
  Operator: "text-amber-400 bg-amber-400/10",
  Viewer: "text-blue-400 bg-blue-400/10",
};

export default function SettingsKeysPage() {
  const [showCreate, setShowCreate] = useState(false);
  const [showKey, setShowKey] = useState<string | null>(null);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS</p>
          <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">API Keys</h1>
          <p className="text-sm text-[var(--muted-foreground)] mt-1">Manage API keys and access tokens.</p>
        </div>
        <button
          onClick={() => setShowCreate(true)}
          className="flex items-center gap-2 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors"
        >
          <Plus className="w-4 h-4" /> Create API Key
        </button>
      </div>

      {/* Keys Table */}
      <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="instrument-card overflow-hidden">
        <table className="w-full">
          <thead>
            <tr className="bg-[var(--surface-0)] border-b border-[var(--border)]">
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Name</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Key Prefix</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Role</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Created</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Last Used</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Expires</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Actions</th>
            </tr>
          </thead>
          <tbody>
            {API_KEYS.map((key, i) => (
              <motion.tr
                key={key.prefix}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ delay: i * 0.05 }}
                className="border-b border-[var(--border)] hover:bg-[var(--surface-1)] transition-colors"
              >
                <td className="px-4 py-3 text-sm font-medium text-[var(--foreground)]">{key.name}</td>
                <td className="px-4 py-3 text-sm font-mono text-[var(--muted-foreground)]">{key.prefix}</td>
                <td className="px-4 py-3">
                  <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${roleColors[key.role]}`}>{key.role}</span>
                </td>
                <td className="px-4 py-3 text-xs text-[var(--muted-foreground)]">{key.created}</td>
                <td className="px-4 py-3 text-xs text-[var(--muted-foreground)]">{key.lastUsed}</td>
                <td className="px-4 py-3 text-xs text-[var(--muted-foreground)]">{key.expires}</td>
                <td className="px-4 py-3">
                  <button className="text-xs text-red-400 hover:text-red-300 font-medium transition-colors">Revoke</button>
                </td>
              </motion.tr>
            ))}
          </tbody>
        </table>
      </motion.div>

      {/* Create Key Dialog */}
      <AnimatePresence>
        {showCreate && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="fixed inset-0 bg-black/50 z-40" onClick={() => setShowCreate(false)} />
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              className="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[440px] bg-[var(--surface-1)] border border-[var(--border)] rounded-xl shadow-2xl z-50"
            >
              <div className="p-6 space-y-5">
                <div className="flex items-center justify-between">
                  <h2 className="text-lg font-display font-semibold text-[var(--foreground)]">Create API Key</h2>
                  <button onClick={() => setShowCreate(false)} className="p-1 hover:bg-[var(--surface-2)] rounded-md transition-colors">
                    <X className="w-5 h-5 text-[var(--muted-foreground)]" />
                  </button>
                </div>
                <div className="space-y-4">
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Name</label>
                    <input type="text" placeholder="e.g., Production API" className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]" />
                  </div>
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Role</label>
                    <select className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                      <option>Admin</option>
                      <option>Operator</option>
                      <option>Viewer</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Expiration</label>
                    <select className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                      <option>30 days</option>
                      <option>90 days</option>
                      <option>1 year</option>
                      <option>Never</option>
                    </select>
                  </div>
                </div>
                <div className="flex items-center gap-3 pt-4 border-t border-[var(--border)]">
                  <button className="flex-1 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors">Create Key</button>
                  <button onClick={() => setShowCreate(false)} className="px-4 py-2 bg-[var(--surface-2)] text-[var(--foreground)] text-sm font-medium rounded-lg hover:bg-[var(--surface-3)] transition-colors">Cancel</button>
                </div>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}
