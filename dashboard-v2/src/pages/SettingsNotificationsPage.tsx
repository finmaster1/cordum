import { useState } from "react";
import { motion } from "framer-motion";
import { Bell, Mail, MessageSquare, Webhook, Send, CheckCircle2 } from "lucide-react";

const CHANNELS = [
  { id: "inapp", name: "In-App", icon: Bell, enabled: true, description: "Notifications in the dashboard bell icon", config: [] },
  { id: "email", name: "Email", icon: Mail, enabled: true, description: "Email notifications via SMTP", config: [
    { label: "SMTP Host", value: "smtp.cordum.io" },
    { label: "Port", value: "587" },
    { label: "From Address", value: "alerts@cordum.io" },
  ]},
  { id: "slack", name: "Slack", icon: MessageSquare, enabled: true, description: "Slack webhook notifications", config: [
    { label: "Webhook URL", value: "https://hooks.slack.com/services/T00.../B00.../xxx" },
    { label: "Channel", value: "#cordum-alerts" },
  ]},
  { id: "webhook", name: "Webhook", icon: Webhook, enabled: false, description: "Custom webhook with signature verification", config: [
    { label: "URL", value: "" },
    { label: "Secret", value: "" },
  ]},
];

const EVENTS = [
  { name: "Approval Required", inapp: true, email: true, slack: true, webhook: true },
  { name: "Job Failed", inapp: true, email: false, slack: true, webhook: true },
  { name: "Worker Offline", inapp: true, email: true, slack: true, webhook: false },
  { name: "Policy Deployed", inapp: true, email: false, slack: false, webhook: true },
  { name: "DLQ Item Added", inapp: true, email: false, slack: true, webhook: true },
  { name: "System Health Alert", inapp: true, email: true, slack: true, webhook: true },
  { name: "Workflow Failed", inapp: true, email: true, slack: true, webhook: false },
];

export default function SettingsNotificationsPage() {
  const [channels, setChannels] = useState(CHANNELS);
  const [events, setEvents] = useState(EVENTS);

  const toggleChannel = (id: string) => {
    setChannels(prev => prev.map(c => c.id === id ? { ...c, enabled: !c.enabled } : c));
  };

  const toggleEvent = (eventIdx: number, channel: string) => {
    setEvents(prev => prev.map((e, i) => i === eventIdx ? { ...e, [channel]: !e[channel as keyof typeof e] } : e));
  };

  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Notifications</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Notification channels and preferences.</p>
      </div>

      {/* Channels */}
      <div className="space-y-4">
        <h2 className="text-sm font-display font-semibold text-[var(--foreground)]">Channels</h2>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          {channels.map((ch, i) => {
            const Icon = ch.icon;
            return (
              <motion.div
                key={ch.id}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.05 }}
                className="instrument-card"
              >
                <div className="p-5 space-y-4">
                  <div className="flex items-start justify-between">
                    <div className="flex items-center gap-3">
                      <div className="w-10 h-10 rounded-lg bg-[var(--surface-2)] flex items-center justify-center">
                        <Icon className="w-5 h-5 text-[var(--cordum)]" />
                      </div>
                      <div>
                        <h3 className="font-display font-semibold text-[var(--foreground)]">{ch.name}</h3>
                        <p className="text-xs text-[var(--muted-foreground)]">{ch.description}</p>
                      </div>
                    </div>
                    <button
                      onClick={() => toggleChannel(ch.id)}
                      className={`relative w-11 h-6 rounded-full transition-colors ${ch.enabled ? "bg-[var(--cordum)]" : "bg-[var(--surface-3)]"}`}
                    >
                      <span className={`absolute top-0.5 left-0.5 w-5 h-5 bg-white rounded-full transition-transform ${ch.enabled ? "translate-x-5" : ""}`} />
                    </button>
                  </div>
                  {ch.config.length > 0 && ch.enabled && (
                    <div className="space-y-2 pt-3 border-t border-[var(--border)]">
                      {ch.config.map(c => (
                        <div key={c.label} className="flex items-center justify-between">
                          <span className="text-xs text-[var(--muted-foreground)]">{c.label}</span>
                          <span className="text-xs font-mono text-[var(--foreground)] truncate max-w-[200px]">{c.value || "Not configured"}</span>
                        </div>
                      ))}
                      <button className="flex items-center gap-1.5 mt-2 text-xs font-medium text-[var(--cordum)] hover:text-[var(--cordum-dim)] transition-colors">
                        <Send className="w-3 h-3" /> Send Test
                      </button>
                    </div>
                  )}
                </div>
              </motion.div>
            );
          })}
        </div>
      </div>

      {/* Event Preferences Matrix */}
      <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.2 }} className="instrument-card overflow-hidden">
        <div className="px-5 py-3 border-b border-[var(--border)]">
          <h2 className="text-sm font-display font-semibold text-[var(--foreground)]">Event Preferences</h2>
        </div>
        <table className="w-full">
          <thead>
            <tr className="bg-[var(--surface-0)] border-b border-[var(--border)]">
              <th className="text-left px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Event</th>
              <th className="text-center px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">In-App</th>
              <th className="text-center px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Email</th>
              <th className="text-center px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Slack</th>
              <th className="text-center px-4 py-3 text-xs font-mono uppercase tracking-wider text-[var(--muted-foreground)]">Webhook</th>
            </tr>
          </thead>
          <tbody>
            {events.map((evt, i) => (
              <tr key={evt.name} className="border-b border-[var(--border)] hover:bg-[var(--surface-1)] transition-colors">
                <td className="px-4 py-3 text-sm text-[var(--foreground)]">{evt.name}</td>
                {(["inapp", "email", "slack", "webhook"] as const).map(ch => (
                  <td key={ch} className="px-4 py-3 text-center">
                    <button
                      onClick={() => toggleEvent(i, ch)}
                      className={`w-5 h-5 rounded border transition-colors ${
                        evt[ch]
                          ? "bg-[var(--cordum)] border-[var(--cordum)]"
                          : "bg-transparent border-[var(--border)] hover:border-[var(--muted-foreground)]"
                      } flex items-center justify-center`}
                    >
                      {evt[ch] && <CheckCircle2 className="w-3 h-3 text-white" />}
                    </button>
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </motion.div>
    </div>
  );
}
