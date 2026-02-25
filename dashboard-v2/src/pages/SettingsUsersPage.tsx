import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Users, Plus, Edit3, UserMinus, X, Mail, Shield } from "lucide-react";

const USERS_DATA = [
  { name: "Yaron Toren", email: "yaron@cordum.io", role: "Admin", lastLogin: "2m ago", status: "Active", avatar: "YT" },
  { name: "Sarah Chen", email: "sarah@cordum.io", role: "Operator", lastLogin: "1h ago", status: "Active", avatar: "SC" },
  { name: "Alex Rivera", email: "alex@cordum.io", role: "Policy Author", lastLogin: "3h ago", status: "Active", avatar: "AR" },
  { name: "Jordan Kim", email: "jordan@cordum.io", role: "Viewer", lastLogin: "1d ago", status: "Active", avatar: "JK" },
  { name: "Morgan Lee", email: "morgan@cordum.io", role: "Operator", lastLogin: "Never", status: "Pending", avatar: "ML" },
];

const roleColors: Record<string, string> = {
  Admin: "text-red-400 bg-red-400/10",
  Operator: "text-amber-400 bg-amber-400/10",
  "Policy Author": "text-blue-400 bg-blue-400/10",
  Viewer: "text-[var(--muted-foreground)] bg-[var(--surface-2)]",
};

const statusColors: Record<string, string> = {
  Active: "text-emerald-400 bg-emerald-400/10",
  Pending: "text-amber-400 bg-amber-400/10",
  Inactive: "text-[var(--muted-foreground)] bg-[var(--surface-2)]",
};

export default function SettingsUsersPage() {
  const [showInvite, setShowInvite] = useState(false);
  const [tab, setTab] = useState<"users" | "roles">("users");

  const ROLES = [
    { name: "Admin", description: "Full access to all features", permissions: ["All pages", "All actions", "User management", "System config"] },
    { name: "Operator", description: "Monitor + Approve, no config changes", permissions: ["Dashboard", "Jobs", "Agents", "Approvals", "Workflows (view)", "Audit Log"] },
    { name: "Policy Author", description: "Operator + full policy CRUD", permissions: ["All Operator permissions", "Policy CRUD", "Policy Builder", "Simulator", "Analytics"] },
    { name: "Viewer", description: "Read-only access", permissions: ["Dashboard (view)", "Jobs (view)", "Agents (view)", "Audit Log (view)"] },
  ];

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS</p>
          <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Users & RBAC</h1>
          <p className="text-sm text-[var(--muted-foreground)] mt-1">User management and role assignments.</p>
        </div>
        <button
          onClick={() => setShowInvite(true)}
          className="flex items-center gap-2 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors"
        >
          <Plus className="w-4 h-4" /> Invite User
        </button>
      </div>

      {/* Tabs */}
      <div className="flex items-center gap-1 bg-[var(--surface-0)] rounded-lg p-1 w-fit">
        {[{ id: "users" as const, label: "Users" }, { id: "roles" as const, label: "Roles" }].map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={`px-4 py-2 text-sm font-medium rounded-md transition-all ${
              tab === t.id ? "bg-[var(--cordum)]/10 text-[var(--cordum)]" : "text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "users" && (
        <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="instrument-card overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="bg-[var(--surface-0)] border-b border-[var(--border)]">
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">User</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Role</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Last Login</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Status</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Actions</th>
              </tr>
            </thead>
            <tbody>
              {USERS_DATA.map((user, i) => (
                <motion.tr
                  key={user.email}
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1 }}
                  transition={{ delay: i * 0.05 }}
                  className="border-b border-[var(--border)] hover:bg-[var(--surface-1)] transition-colors"
                >
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-3">
                      <div className="w-8 h-8 rounded-full bg-[var(--cordum)]/10 flex items-center justify-center text-xs font-semibold text-[var(--cordum)]">{user.avatar}</div>
                      <div>
                        <p className="text-sm font-medium text-[var(--foreground)]">{user.name}</p>
                        <p className="text-xs text-[var(--muted-foreground)]">{user.email}</p>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${roleColors[user.role]}`}>{user.role}</span>
                  </td>
                  <td className="px-4 py-3 text-xs text-[var(--muted-foreground)]">{user.lastLogin}</td>
                  <td className="px-4 py-3">
                    <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${statusColors[user.status]}`}>{user.status}</span>
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <button className="text-xs text-[var(--muted-foreground)] hover:text-[var(--foreground)] font-medium transition-colors">Edit Role</button>
                      <button className="text-xs text-red-400 hover:text-red-300 font-medium transition-colors">Deactivate</button>
                    </div>
                  </td>
                </motion.tr>
              ))}
            </tbody>
          </table>
        </motion.div>
      )}

      {tab === "roles" && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {ROLES.map((role, i) => (
            <motion.div
              key={role.name}
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.05 }}
              className="instrument-card"
            >
              <div className="p-5 space-y-3">
                <div className="flex items-center gap-2">
                  <Shield className="w-5 h-5 text-[var(--cordum)]" />
                  <h3 className="font-display font-semibold text-[var(--foreground)]">{role.name}</h3>
                </div>
                <p className="text-sm text-[var(--muted-foreground)]">{role.description}</p>
                <div className="pt-3 border-t border-[var(--border)]">
                  <p className="text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-2">Permissions</p>
                  <div className="flex flex-wrap gap-1.5">
                    {role.permissions.map(p => (
                      <span key={p} className="text-xs bg-[var(--surface-2)] text-[var(--muted-foreground)] px-2 py-0.5 rounded">{p}</span>
                    ))}
                  </div>
                </div>
              </div>
            </motion.div>
          ))}
        </div>
      )}

      {/* Invite Dialog */}
      <AnimatePresence>
        {showInvite && (
          <>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="fixed inset-0 bg-black/50 z-40" onClick={() => setShowInvite(false)} />
            <motion.div
              initial={{ opacity: 0, scale: 0.95 }}
              animate={{ opacity: 1, scale: 1 }}
              exit={{ opacity: 0, scale: 0.95 }}
              className="fixed top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[440px] bg-[var(--surface-1)] border border-[var(--border)] rounded-xl shadow-2xl z-50"
            >
              <div className="p-6 space-y-5">
                <div className="flex items-center justify-between">
                  <h2 className="text-lg font-display font-semibold text-[var(--foreground)]">Invite User</h2>
                  <button onClick={() => setShowInvite(false)} className="p-1 hover:bg-[var(--surface-2)] rounded-md transition-colors"><X className="w-5 h-5 text-[var(--muted-foreground)]" /></button>
                </div>
                <div className="space-y-4">
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Email</label>
                    <input type="email" placeholder="user@company.com" className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]" />
                  </div>
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Role</label>
                    <select className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                      <option>Admin</option>
                      <option>Operator</option>
                      <option>Policy Author</option>
                      <option>Viewer</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Welcome Message (optional)</label>
                    <textarea rows={3} placeholder="Add a personal message..." className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)] resize-none" />
                  </div>
                </div>
                <div className="flex items-center gap-3 pt-4 border-t border-[var(--border)]">
                  <button className="flex-1 flex items-center justify-center gap-2 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors">
                    <Mail className="w-4 h-4" /> Send Invite
                  </button>
                  <button onClick={() => setShowInvite(false)} className="px-4 py-2 bg-[var(--surface-2)] text-[var(--foreground)] text-sm font-medium rounded-lg hover:bg-[var(--surface-3)] transition-colors">Cancel</button>
                </div>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}
