import { cn } from "@/lib/utils";

type SafetyDecisionType = "allow" | "deny" | "require_approval" | "allow_with_constraints" | "throttle";

const decisionConfig: Record<SafetyDecisionType, { color: string; bg: string; label: string }> = {
  allow: { color: "text-emerald-400", bg: "bg-emerald-400/10", label: "ALLOW" },
  deny: { color: "text-red-400", bg: "bg-red-400/10", label: "DENY" },
  require_approval: { color: "text-amber-400", bg: "bg-amber-400/10", label: "APPROVAL" },
  allow_with_constraints: { color: "text-blue-400", bg: "bg-blue-400/10", label: "CONSTRAINED" },
  throttle: { color: "text-orange-400", bg: "bg-orange-400/10", label: "THROTTLE" },
};

interface SafetyDecisionBadgeProps {
  decision?: string;
  matchedRules?: string[];
}

export function SafetyDecisionBadge({ decision, matchedRules }: SafetyDecisionBadgeProps) {
  const c = decisionConfig[decision as SafetyDecisionType] ?? {
    color: "text-muted-foreground",
    bg: "bg-surface-2",
    label: decision?.toUpperCase() || "\u2014",
  };

  return (
    <span
      className={cn(
        "group relative inline-flex items-center gap-1 px-2 py-0.5 rounded font-mono text-[10px] font-semibold tracking-wider cursor-default",
        c.color,
        c.bg,
      )}
    >
      {c.label}
      {matchedRules && matchedRules.length > 0 && (
        <span className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 hidden group-hover:block z-50 min-w-[180px]">
          <span className="block bg-surface-3 border border-border rounded-lg p-2 shadow-xl text-[10px] text-muted-foreground font-normal tracking-normal">
            <span className="block text-foreground font-semibold mb-1">
              {matchedRules.length} matched rule{matchedRules.length > 1 ? "s" : ""}
            </span>
            {matchedRules.map((r, i) => (
              <span key={i} className="block truncate">{r}</span>
            ))}
          </span>
        </span>
      )}
    </span>
  );
}
