import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  FileJson, Search, Plus, X, ExternalLink, ArrowUpDown
} from "lucide-react";

const SCHEMAS = [
  { id: "sch-001", name: "ServiceRestartInput", type: "Input", version: "v3", usedBy: 4, updatedAt: "2m ago" },
  { id: "sch-002", name: "SlackMessagePayload", type: "Input", version: "v2", usedBy: 7, updatedAt: "1h ago" },
  { id: "sch-003", name: "DeploymentResult", type: "Output", version: "v1", usedBy: 3, updatedAt: "3h ago" },
  { id: "sch-004", name: "PRCreatedEvent", type: "Event", version: "v2", usedBy: 2, updatedAt: "1d ago" },
  { id: "sch-005", name: "DatabaseQueryInput", type: "Input", version: "v4", usedBy: 6, updatedAt: "2d ago" },
  { id: "sch-006", name: "AlertPayload", type: "Output", version: "v1", usedBy: 5, updatedAt: "4d ago" },
  { id: "sch-007", name: "WorkflowStepResult", type: "Output", version: "v3", usedBy: 8, updatedAt: "5d ago" },
  { id: "sch-008", name: "UserActionEvent", type: "Event", version: "v1", usedBy: 1, updatedAt: "1w ago" },
];

const typeColors: Record<string, string> = {
  Input: "text-blue-400 bg-blue-400/10",
  Output: "text-[var(--cordum)] bg-[var(--cordum)]/10",
  Event: "text-amber-400 bg-amber-400/10",
};

export default function SchemasPage() {
  const [search, setSearch] = useState("");
  const [typeFilter, setTypeFilter] = useState("All");
  const [showRegister, setShowRegister] = useState(false);

  const filtered = SCHEMAS.filter(s => {
    const matchSearch = s.name.toLowerCase().includes(search.toLowerCase()) || s.id.toLowerCase().includes(search.toLowerCase());
    const matchType = typeFilter === "All" || s.type === typeFilter;
    return matchSearch && matchType;
  });

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">EXTEND / Schemas</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Schemas</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Browse and manage the schema registry.</p>
      </div>

      {/* Toolbar */}
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--muted-foreground)]" />
            <input
              type="text"
              placeholder="Search schemas..."
              value={search}
              onChange={e => setSearch(e.target.value)}
              className="pl-9 pr-4 py-2 w-[280px] bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]"
            />
          </div>
          <div className="flex items-center gap-1 bg-[var(--surface-0)] rounded-lg p-1">
            {["All", "Input", "Output", "Event"].map(t => (
              <button
                key={t}
                onClick={() => setTypeFilter(t)}
                className={`px-3 py-1.5 text-xs font-medium rounded-md transition-all ${
                  typeFilter === t
                    ? "bg-[var(--cordum)]/10 text-[var(--cordum)]"
                    : "text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
                }`}
              >
                {t}
              </button>
            ))}
          </div>
        </div>
        <button
          onClick={() => setShowRegister(true)}
          className="flex items-center gap-2 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors"
        >
          <Plus className="w-4 h-4" /> Register Schema
        </button>
      </div>

      {/* Table */}
      <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} className="instrument-card overflow-hidden">
        <table className="w-full">
          <thead>
            <tr className="bg-[var(--surface-0)] border-b border-[var(--border)]">
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Schema ID</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Name</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Type</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Version</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Used By</th>
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Updated</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((s, i) => (
              <motion.tr
                key={s.id}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                transition={{ delay: i * 0.03 }}
                className="border-b border-[var(--border)] hover:bg-[var(--surface-1)] transition-colors cursor-pointer"
              >
                <td className="px-4 py-3 text-sm font-mono text-[var(--cordum)]">{s.id}</td>
                <td className="px-4 py-3 text-sm font-medium text-[var(--foreground)]">{s.name}</td>
                <td className="px-4 py-3">
                  <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${typeColors[s.type]}`}>{s.type}</span>
                </td>
                <td className="px-4 py-3 text-xs font-mono text-[var(--muted-foreground)]">{s.version}</td>
                <td className="px-4 py-3 text-sm font-mono text-[var(--foreground)]">{s.usedBy} topics</td>
                <td className="px-4 py-3 text-xs text-[var(--muted-foreground)]">{s.updatedAt}</td>
              </motion.tr>
            ))}
          </tbody>
        </table>
      </motion.div>

      {/* Register Schema Slide-over */}
      <AnimatePresence>
        {showRegister && (
          <>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="fixed inset-0 bg-black/50 z-40"
              onClick={() => setShowRegister(false)}
            />
            <motion.div
              initial={{ x: "100%" }}
              animate={{ x: 0 }}
              exit={{ x: "100%" }}
              transition={{ type: "spring", damping: 25, stiffness: 200 }}
              className="fixed right-0 top-0 bottom-0 w-[480px] bg-[var(--surface-1)] border-l border-[var(--border)] z-50 overflow-y-auto"
            >
              <div className="p-6 space-y-6">
                <div className="flex items-center justify-between">
                  <h2 className="text-lg font-display font-semibold text-[var(--foreground)]">Register Schema</h2>
                  <button onClick={() => setShowRegister(false)} className="p-1 hover:bg-[var(--surface-2)] rounded-md transition-colors">
                    <X className="w-5 h-5 text-[var(--muted-foreground)]" />
                  </button>
                </div>
                <div className="space-y-4">
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Schema Name</label>
                    <input type="text" placeholder="e.g., ServiceRestartInput" className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]" />
                  </div>
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Type</label>
                    <select className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                      <option>Input</option>
                      <option>Output</option>
                      <option>Event</option>
                    </select>
                  </div>
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">Description</label>
                    <textarea rows={2} placeholder="Describe this schema..." className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)] resize-none" />
                  </div>
                  <div>
                    <label className="block text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)] mb-1.5">JSON Schema Definition</label>
                    <textarea rows={12} placeholder='{
  "type": "object",
  "properties": {
    ...
  }
}' className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm font-mono text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)] resize-none" />
                  </div>
                </div>
                <div className="flex items-center gap-3 pt-4 border-t border-[var(--border)]">
                  <button className="flex-1 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors">Register</button>
                  <button onClick={() => setShowRegister(false)} className="px-4 py-2 bg-[var(--surface-2)] text-[var(--foreground)] text-sm font-medium rounded-lg hover:bg-[var(--surface-3)] transition-colors">Cancel</button>
                </div>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}
