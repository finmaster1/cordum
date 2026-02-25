import { useState } from "react";
import { motion } from "framer-motion";
import { Settings, Shield, Gauge, Save, RotateCcw, AlertTriangle } from "lucide-react";

interface SettingField {
  label: string;
  key: string;
  type: "text" | "select" | "number" | "toggle";
  description: string;
  options?: string[];
  defaultValue: string | number | boolean;
  unit?: string;
}

const SETTING_GROUPS: { title: string; icon: any; fields: SettingField[] }[] = [
  {
    title: "General",
    icon: Settings,
    fields: [
      { label: "Instance Name", key: "instanceName", type: "text", description: "Display name for this Cordum instance", defaultValue: "Cordum Production" },
      { label: "Default Environment", key: "defaultEnv", type: "select", description: "Default environment for new jobs", options: ["production", "staging", "sandbox"], defaultValue: "production" },
      { label: "Log Level", key: "logLevel", type: "select", description: "System-wide logging verbosity", options: ["DEBUG", "INFO", "WARN", "ERROR"], defaultValue: "INFO" },
      { label: "Timezone", key: "timezone", type: "select", description: "System timezone for scheduling", options: ["UTC", "US/Eastern", "US/Pacific", "Europe/London", "Asia/Tokyo"], defaultValue: "UTC" },
    ],
  },
  {
    title: "Safety",
    icon: Shield,
    fields: [
      { label: "Default Fail Mode", key: "failMode", type: "toggle", description: "What happens when the Safety Kernel is unreachable. Fail Closed = deny all.", defaultValue: false },
      { label: "Evaluation Timeout", key: "evalTimeout", type: "number", description: "Max time for safety evaluation before timeout", defaultValue: 10, unit: "ms" },
      { label: "Max Retries", key: "maxRetries", type: "number", description: "Default retry count for failed jobs", defaultValue: 3 },
      { label: "DLQ Enabled", key: "dlqEnabled", type: "toggle", description: "Move permanently failed jobs to Dead Letter Queue", defaultValue: true },
    ],
  },
  {
    title: "Performance",
    icon: Gauge,
    fields: [
      { label: "Max Concurrent Jobs", key: "maxConcurrent", type: "number", description: "Global concurrency limit", defaultValue: 100 },
      { label: "Heartbeat Interval", key: "heartbeatInterval", type: "number", description: "How often workers send heartbeats", defaultValue: 30, unit: "s" },
      { label: "Worker Timeout", key: "workerTimeout", type: "number", description: "Mark worker offline after this duration without heartbeat", defaultValue: 90, unit: "s" },
    ],
  },
];

export default function SettingsConfigPage() {
  const [hasChanges, setHasChanges] = useState(false);
  const [values, setValues] = useState<Record<string, any>>(() => {
    const v: Record<string, any> = {};
    SETTING_GROUPS.forEach(g => g.fields.forEach(f => { v[f.key] = f.defaultValue; }));
    return v;
  });

  const updateValue = (key: string, val: any) => {
    setValues(prev => ({ ...prev, [key]: val }));
    setHasChanges(true);
  };

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">System Config</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Core system configuration and feature flags.</p>
      </div>

      {/* Unsaved changes banner */}
      {hasChanges && (
        <motion.div
          initial={{ opacity: 0, y: -8 }}
          animate={{ opacity: 1, y: 0 }}
          className="flex items-center justify-between px-4 py-3 bg-amber-400/10 border border-amber-400/20 rounded-lg"
        >
          <div className="flex items-center gap-2 text-sm text-amber-400">
            <AlertTriangle className="w-4 h-4" />
            You have unsaved changes
          </div>
          <div className="flex items-center gap-2">
            <button onClick={() => setHasChanges(false)} className="px-3 py-1.5 text-xs font-medium text-[var(--muted-foreground)] hover:text-[var(--foreground)] transition-colors">Discard</button>
            <button className="px-3 py-1.5 text-xs font-medium bg-[var(--cordum)] text-[var(--surface-0)] rounded-md hover:bg-[var(--cordum-dim)] transition-colors">Save Changes</button>
          </div>
        </motion.div>
      )}

      {/* Setting Groups */}
      {SETTING_GROUPS.map((group, gi) => (
        <motion.div
          key={group.title}
          initial={{ opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: gi * 0.08 }}
          className="instrument-card"
        >
          <div className="p-5 space-y-5">
            <div className="flex items-center gap-2">
              <group.icon className="w-5 h-5 text-[var(--cordum)]" />
              <h2 className="text-lg font-display font-semibold text-[var(--foreground)]">{group.title}</h2>
            </div>
            <div className="space-y-4">
              {group.fields.map(field => (
                <div key={field.key} className="flex items-start justify-between gap-8 py-3 border-b border-[var(--border)] last:border-0">
                  <div className="flex-1">
                    <label className="text-sm font-medium text-[var(--foreground)]">{field.label}</label>
                    <p className="text-xs text-[var(--muted-foreground)] mt-0.5">{field.description}</p>
                  </div>
                  <div className="w-[240px] flex-shrink-0">
                    {field.type === "text" && (
                      <input
                        type="text"
                        value={values[field.key] as string}
                        onChange={e => updateValue(field.key, e.target.value)}
                        className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]"
                      />
                    )}
                    {field.type === "select" && (
                      <select
                        value={values[field.key] as string}
                        onChange={e => updateValue(field.key, e.target.value)}
                        className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]"
                      >
                        {field.options?.map(o => <option key={o} value={o}>{o}</option>)}
                      </select>
                    )}
                    {field.type === "number" && (
                      <div className="flex items-center gap-2">
                        <input
                          type="number"
                          value={values[field.key] as number}
                          onChange={e => updateValue(field.key, Number(e.target.value))}
                          className="w-full px-3 py-2 bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm font-mono text-[var(--foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]"
                        />
                        {field.unit && <span className="text-xs font-mono text-[var(--muted-foreground)] whitespace-nowrap">{field.unit}</span>}
                      </div>
                    )}
                    {field.type === "toggle" && (
                      <button
                        onClick={() => updateValue(field.key, !values[field.key])}
                        className={`relative w-11 h-6 rounded-full transition-colors ${values[field.key] ? "bg-[var(--cordum)]" : "bg-[var(--surface-3)]"}`}
                      >
                        <span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform ${values[field.key] ? "translate-x-5" : ""}`} />
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </motion.div>
      ))}

      {/* Actions */}
      <div className="flex items-center gap-3">
        <button className="flex items-center gap-2 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors">
          <Save className="w-4 h-4" /> Save Changes
        </button>
        <button className="flex items-center gap-2 px-4 py-2 border border-red-400/30 text-red-400 text-sm font-medium rounded-lg hover:bg-red-400/10 transition-colors">
          <RotateCcw className="w-4 h-4" /> Reset to Defaults
        </button>
      </div>
    </div>
  );
}
