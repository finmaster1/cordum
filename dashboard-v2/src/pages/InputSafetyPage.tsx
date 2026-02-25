import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { ShieldCheck, Plus, Edit3, Trash2, X, AlertTriangle } from "lucide-react";

const CUSTOM_PATTERNS = [
  { name: "Credit Card Numbers", regex: "\\b\\d{4}[- ]?\\d{4}[- ]?\\d{4}[- ]?\\d{4}\\b", action: "Block", enabled: true },
  { name: "Social Security Numbers", regex: "\\b\\d{3}-\\d{2}-\\d{4}\\b", action: "Block", enabled: true },
  { name: "Internal API Keys", regex: "ck_(live|test)_[a-zA-Z0-9]{32}", action: "Warn", enabled: true },
  { name: "Email Addresses", regex: "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}", action: "Warn", enabled: false },
];

export default function InputSafetyPage() {
  const [settings, setSettings] = useState({
    failMode: "closed",
    maxInputSize: 1024,
    inputValidation: true,
    piiDetection: true,
    piiAction: "Redact",
    injectionDetection: true,
    injectionAction: "Block",
  });
  const [patterns, setPatterns] = useState(CUSTOM_PATTERNS);

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS / Safety</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Input Safety</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Configure how the Safety Kernel handles input validation and sanitization.</p>
      </div>

      {/* Settings Card */}
      <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} className="instrument-card">
        <div className="p-5 space-y-5">
          <h2 className="text-sm font-display font-semibold text-[var(--foreground)] flex items-center gap-2">
            <ShieldCheck className="w-5 h-5 text-[var(--cordum)]" /> Input Safety Settings
          </h2>
          <div className="space-y-4">
            {/* Fail Mode */}
            <div className="flex items-start justify-between gap-8 py-3 border-b border-[var(--border)]">
              <div><p className="text-sm font-medium text-[var(--foreground)]">Fail Mode</p><p className="text-xs text-[var(--muted-foreground)]">What happens when the Safety Kernel is unreachable</p></div>
              <div className="flex items-center gap-2">
                {["open", "closed"].map(mode => (
                  <button
                    key={mode}
                    onClick={() => setSettings(s => ({ ...s, failMode: mode }))}
                    className={`px-3 py-1.5 text-xs font-medium rounded-md transition-all ${
                      settings.failMode === mode
                        ? mode === "closed" ? "bg-red-400/10 text-red-400" : "bg-amber-400/10 text-amber-400"
                        : "bg-[var(--surface-2)] text-[var(--muted-foreground)]"
                    }`}
                  >
                    Fail {mode.charAt(0).toUpperCase() + mode.slice(1)}
                  </button>
                ))}
              </div>
            </div>
            {/* Max Input Size */}
            <div className="flex items-start justify-between gap-8 py-3 border-b border-[var(--border)]">
              <div><p className="text-sm font-medium text-[var(--foreground)]">Max Input Size</p><p className="text-xs text-[var(--muted-foreground)]">Maximum allowed input payload size</p></div>
              <div className="flex items-center gap-2">
                <input type="number" value={settings.maxInputSize} onChange={e => setSettings(s => ({ ...s, maxInputSize: Number(e.target.value) }))} className="w-24 px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm font-mono text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]" />
                <span className="text-xs font-mono text-[var(--muted-foreground)]">KB</span>
              </div>
            </div>
            {/* Toggles */}
            {[
              { key: "inputValidation", label: "Input Validation", desc: "Enable JSON schema validation on inputs" },
              { key: "piiDetection", label: "PII Detection", desc: "Scan inputs for personally identifiable information" },
              { key: "injectionDetection", label: "Prompt Injection Detection", desc: "Scan for prompt injection patterns" },
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
            {/* PII Action */}
            {settings.piiDetection && (
              <div className="flex items-start justify-between gap-8 py-3 border-b border-[var(--border)]">
                <div><p className="text-sm font-medium text-[var(--foreground)]">PII Action</p><p className="text-xs text-[var(--muted-foreground)]">What to do when PII is detected</p></div>
                <select value={settings.piiAction} onChange={e => setSettings(s => ({ ...s, piiAction: e.target.value }))} className="px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                  <option>Warn</option><option>Redact</option><option>Block</option>
                </select>
              </div>
            )}
            {/* Injection Action */}
            {settings.injectionDetection && (
              <div className="flex items-start justify-between gap-8 py-3">
                <div><p className="text-sm font-medium text-[var(--foreground)]">Injection Action</p><p className="text-xs text-[var(--muted-foreground)]">What to do when injection is detected</p></div>
                <select value={settings.injectionAction} onChange={e => setSettings(s => ({ ...s, injectionAction: e.target.value }))} className="px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]">
                  <option>Warn</option><option>Block</option>
                </select>
              </div>
            )}
          </div>
        </div>
      </motion.div>

      {/* Custom Patterns */}
      <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.1 }} className="instrument-card">
        <div className="p-5 space-y-4">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-display font-semibold text-[var(--foreground)]">Custom Patterns</h2>
            <button className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-[var(--cordum)] text-[var(--surface-0)] rounded-md hover:bg-[var(--cordum-dim)] transition-colors">
              <Plus className="w-3 h-3" /> Add Pattern
            </button>
          </div>
          <div className="space-y-2">
            {patterns.map((p, i) => (
              <div key={i} className="flex items-center justify-between p-3 bg-[var(--surface-0)] rounded-lg border border-[var(--border)]">
                <div className="flex items-center gap-3 flex-1">
                  <button
                    onClick={() => setPatterns(prev => prev.map((pp, ii) => ii === i ? { ...pp, enabled: !pp.enabled } : pp))}
                    className={`relative w-9 h-5 rounded-full transition-colors ${p.enabled ? "bg-[var(--cordum)]" : "bg-[var(--surface-3)]"}`}
                  >
                    <span className={`absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform ${p.enabled ? "translate-x-4" : ""}`} />
                  </button>
                  <div>
                    <p className="text-sm font-medium text-[var(--foreground)]">{p.name}</p>
                    <p className="text-xs font-mono text-[var(--muted-foreground)] truncate max-w-[300px]">{p.regex}</p>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${p.action === "Block" ? "text-red-400 bg-red-400/10" : "text-amber-400 bg-amber-400/10"}`}>{p.action}</span>
                  <button className="p-1 hover:bg-[var(--surface-2)] rounded-md transition-colors"><Edit3 className="w-3.5 h-3.5 text-[var(--muted-foreground)]" /></button>
                  <button className="p-1 hover:bg-red-400/10 rounded-md transition-colors"><Trash2 className="w-3.5 h-3.5 text-red-400" /></button>
                </div>
              </div>
            ))}
          </div>
        </div>
      </motion.div>
    </div>
  );
}
