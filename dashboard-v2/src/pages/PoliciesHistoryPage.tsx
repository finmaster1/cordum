import { motion } from "framer-motion";
import { Plus, Edit3, Rocket, Pause, Trash2, FlaskConical, ArrowRight } from "lucide-react";

const EVENTS = [
  { type: "deployed", icon: Rocket, color: "text-[var(--cordum)]", bg: "bg-[var(--cordum)]/10", rule: "production-restart-gate", actor: "Yaron Toren", time: "2h ago", summary: "Deployed v3 to production" },
  { type: "modified", icon: Edit3, color: "text-blue-400", bg: "bg-blue-400/10", rule: "data-export-limit", actor: "Sarah Chen", time: "5h ago", summary: "Changed decision from ALLOW to ALLOW_WITH_CONSTRAINTS" },
  { type: "simulation", icon: FlaskConical, color: "text-blue-400", bg: "bg-blue-400/10", rule: "pii-detection-v2", actor: "Alex Rivera", time: "8h ago", summary: "Simulation: 3/100 decisions changed" },
  { type: "created", icon: Plus, color: "text-[var(--cordum)]", bg: "bg-[var(--cordum)]/10", rule: "api-rate-limit", actor: "Yaron Toren", time: "1d ago", summary: "New rule created: THROTTLE for api.* actions" },
  { type: "disabled", icon: Pause, color: "text-amber-400", bg: "bg-amber-400/10", rule: "legacy-auth-check", actor: "Sarah Chen", time: "2d ago", summary: "Rule disabled pending review" },
  { type: "deleted", icon: Trash2, color: "text-red-400", bg: "bg-red-400/10", rule: "deprecated-v1-gate", actor: "Yaron Toren", time: "3d ago", summary: "Rule permanently deleted" },
  { type: "deployed", icon: Rocket, color: "text-[var(--cordum)]", bg: "bg-[var(--cordum)]/10", rule: "sandbox-allow-all", actor: "Alex Rivera", time: "4d ago", summary: "Deployed v1 to sandbox environment" },
  { type: "modified", icon: Edit3, color: "text-blue-400", bg: "bg-blue-400/10", rule: "production-restart-gate", actor: "Yaron Toren", time: "5d ago", summary: "Updated conditions: added risk_score > 0.7" },
];

export default function PoliciesHistoryPage() {
  return (
    <div className="space-y-6">
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">GOVERN / Policies</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Policy History</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Audit trail of all policy changes.</p>
      </div>

      {/* Timeline */}
      <div className="relative">
        {/* Vertical line */}
        <div className="absolute left-5 top-0 bottom-0 w-px bg-[var(--border)]" />

        <div className="space-y-4">
          {EVENTS.map((evt, i) => {
            const Icon = evt.icon;
            return (
              <motion.div
                key={i}
                initial={{ opacity: 0, x: -12 }}
                animate={{ opacity: 1, x: 0 }}
                transition={{ delay: i * 0.06 }}
                className="relative pl-12"
              >
                {/* Timeline dot */}
                <div className={`absolute left-2.5 top-4 w-5 h-5 rounded-full ${evt.bg} flex items-center justify-center ring-4 ring-[var(--background)]`}>
                  <Icon className={`w-3 h-3 ${evt.color}`} />
                </div>

                <div className="instrument-card p-4">
                  <div className="flex items-start justify-between">
                    <div className="space-y-1">
                      <div className="flex items-center gap-2">
                        <span className={`text-xs font-medium px-2 py-0.5 rounded-full ${evt.bg} ${evt.color}`}>{evt.type}</span>
                        <span className="text-sm font-mono font-semibold text-[var(--cordum)] cursor-pointer hover:underline">{evt.rule}</span>
                      </div>
                      <p className="text-sm text-[var(--foreground)]">{evt.summary}</p>
                      <div className="flex items-center gap-2 text-xs text-[var(--muted-foreground)]">
                        <span>{evt.actor}</span>
                        <span>·</span>
                        <span className="font-mono">{evt.time}</span>
                      </div>
                    </div>
                    <button className="flex items-center gap-1 text-xs font-medium text-[var(--cordum)] hover:text-[var(--cordum-dim)] transition-colors">
                      View Diff <ArrowRight className="w-3 h-3" />
                    </button>
                  </div>
                </div>
              </motion.div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
