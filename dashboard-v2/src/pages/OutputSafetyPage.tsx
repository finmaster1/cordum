import { useState } from "react";
import { motion } from "framer-motion";
import { ShieldAlert, CheckCircle2, XCircle, Eye, ArrowRight } from "lucide-react";

const QUARANTINE_QUEUE = [
  { jobId: "job-9f2a1e", agent: "DataExporter-02", reason: "Sensitive data detected: API key pattern", quarantinedAt: "5m ago" },
  { jobId: "job-7b3c4d", agent: "ReportGen-01", reason: "Output exceeds max size (2.1MB > 1MB limit)", quarantinedAt: "22m ago" },
  { jobId: "job-1e5f8g", agent: "CodeAssist-03", reason: "PII detected: email addresses in output", quarantinedAt: "1h ago" },
];

export default function OutputSafetyPage() {
  const [settings, setSettings] = useState({
    quarantine: true,
    quarantineMode: "Review Flagged",
    maxOutputSize: 1024,
    sensitiveDetection: true,
    sensitiveAction: "Redact",
    schemaValidation: true,
  });

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS / Safety</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Output Safety</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Configure output quarantine and validation settings.</p>
      </div>

      {/* Settings */}
      <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} className="instrument-card">
        <div className="p-5 space-y-5">
          <h2 className="text-sm font-display font-semibold text-[var(--foreground)] flex items-center gap-2">
            <ShieldAlert className="w-5 h-5 text-[var(--cordum)]" /> Output Safety Settings
          </h2>
          <div className="space-y-4">
            {[
              { key: "quarantine", label: "Output Quarantine", desc: "Enable output quarantine for flagged outputs" },
              { key: "sensitiveDetection", label: "Sensitive Data Detection", desc: "Scan outputs for API keys, passwords, secrets" },
              { key: "schemaValidation", label: "Output Schema Validation", desc: "Validate outputs against registered schemas" },
            ].map(item => (
              <div key={item.key} className="flex items-start justify-between gap-8 py-3 border-b border-[var(--border)]">
                <div><p className="text-sm font-medium text-[var(--foreground)]">{item.label}</p><p className="text-xs text-[var(--muted-foreground)]">{item.desc}</p></div>
                <button
                  onClick={() => setSettings(s => ({ ...s, [item.key]: !s[item.key as keyof typeof s] }))}
                  className={`relative w-11 h-6 rounded-full transition-colors ${(settings as any)[item.key] ? "bg-[var(--cordum)]" : "bg-[var(--surface-3)]"}`}
                >
                  <span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform ${(settings as any)[item.key] ? "translate-x-5" : ""}`} />
                </button>
              </div>
            ))}
            {settings.quarantine && (
              <div className="flex items-start justify-between gap-8 py-3 border-b border-[var(--border)]">
                <div><p className="text-sm font-medium text-[var(--foreground)]">Quarantine Mode</p><p className="text-xs text-[var(--muted-foreground)]">How quarantined outputs are handled</p></div>
                <select value={settings.quarantineMode} onChange={e => setSettings(s => ({ ...s, quarantineMode: e.target.value }))} className="px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                  <option>Review All</option><option>Review Flagged</option><option>Log Only</option>
                </select>
              </div>
            )}
            <div className="flex items-start justify-between gap-8 py-3 border-b border-[var(--border)]">
              <div><p className="text-sm font-medium text-[var(--foreground)]">Max Output Size</p><p className="text-xs text-[var(--muted-foreground)]">Maximum allowed output payload size</p></div>
              <div className="flex items-center gap-2">
                <input type="number" value={settings.maxOutputSize} onChange={e => setSettings(s => ({ ...s, maxOutputSize: Number(e.target.value) }))} className="w-24 px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm font-mono text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]" />
                <span className="text-xs font-mono text-[var(--muted-foreground)]">KB</span>
              </div>
            </div>
            {settings.sensitiveDetection && (
              <div className="flex items-start justify-between gap-8 py-3">
                <div><p className="text-sm font-medium text-[var(--foreground)]">Sensitive Data Action</p><p className="text-xs text-[var(--muted-foreground)]">What to do when sensitive data is detected</p></div>
                <select value={settings.sensitiveAction} onChange={e => setSettings(s => ({ ...s, sensitiveAction: e.target.value }))} className="px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                  <option>Redact</option><option>Block</option><option>Warn</option>
                </select>
              </div>
            )}
          </div>
        </div>
      </motion.div>

      {/* Quarantine Queue */}
      {settings.quarantine && (
        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.1 }} className="instrument-card overflow-hidden">
          <div className="px-5 py-3 border-b border-[var(--border)]">
            <h2 className="text-sm font-display font-semibold text-[var(--foreground)]">Quarantine Queue ({QUARANTINE_QUEUE.length})</h2>
          </div>
          <table className="w-full">
            <thead>
              <tr className="bg-[var(--surface-0)] border-b border-[var(--border)]">
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Job ID</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Agent</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Reason</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Quarantined</th>
                <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Actions</th>
              </tr>
            </thead>
            <tbody>
              {QUARANTINE_QUEUE.map((item, i) => (
                <tr key={item.jobId} className="border-b border-[var(--border)] hover:bg-[var(--surface-1)] transition-colors">
                  <td className="px-4 py-3 text-sm font-mono text-[var(--cordum)]">{item.jobId}</td>
                  <td className="px-4 py-3 text-sm text-[var(--foreground)]">{item.agent}</td>
                  <td className="px-4 py-3 text-xs text-[var(--muted-foreground)] max-w-[300px] truncate">{item.reason}</td>
                  <td className="px-4 py-3 text-xs font-mono text-[var(--muted-foreground)]">{item.quarantinedAt}</td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <button className="text-xs font-medium text-emerald-400 hover:text-emerald-300 transition-colors">Release</button>
                      <button className="text-xs font-medium text-red-400 hover:text-red-300 transition-colors">Block</button>
                      <button className="text-xs font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)] transition-colors">View</button>
                    </div>
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
