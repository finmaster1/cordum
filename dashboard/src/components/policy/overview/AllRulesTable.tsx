import { useState, useMemo } from "react";
import { cn } from "@/lib/utils";
import { SafetyDecisionBadge } from "@/components/ui/SafetyDecisionBadge";
import { StatusBadge } from "@/components/ui/StatusBadge";
import { RuleDetailRow } from "./RuleDetailRow";
import {
  ArrowUpDown,
  ChevronDown,
  Tag,
  AlertTriangle,
  Shield,
} from "lucide-react";
import type { PolicyBundle, PolicyRule } from "@/api/types";

interface AllRulesTableProps {
  bundles: PolicyBundle[];
  filterText?: string;
}

interface FlatRule {
  rule: PolicyRule;
  bundleName: string;
  bundleId: string;
}

type SortKey = "name" | "bundle" | "decision" | "priority" | "topics";

export function AllRulesTable({ bundles, filterText }: AllRulesTableProps) {
  const [sortKey, setSortKey] = useState<SortKey>("priority");
  const [sortAsc, setSortAsc] = useState(true);
  const [expandedRule, setExpandedRule] = useState<string | null>(null);

  const flatRules = useMemo(() => {
    const result: FlatRule[] = [];
    for (const b of bundles) {
      for (const r of b.rules ?? []) {
        result.push({
          rule: r,
          bundleName: b.name,
          bundleId: b.id,
        });
      }
    }
    return result;
  }, [bundles]);

  const filteredRules = useMemo(() => {
    if (!filterText) return flatRules;
    const lower = filterText.toLowerCase();
    return flatRules.filter(
      (fr) =>
        fr.rule.name.toLowerCase().includes(lower) ||
        fr.rule.id.toLowerCase().includes(lower) ||
        fr.bundleName.toLowerCase().includes(lower) ||
        fr.rule.decision?.toLowerCase().includes(lower) ||
        fr.rule.match?.topics?.some((t) => t.toLowerCase().includes(lower)) ||
        fr.rule.match?.capabilities?.some((c) => c.toLowerCase().includes(lower)) ||
        fr.rule.match?.risk_tags?.some((t) => t.toLowerCase().includes(lower)),
    );
  }, [flatRules, filterText]);

  const sortedRules = useMemo(() => {
    const sorted = [...filteredRules];
    sorted.sort((a, b) => {
      let cmp = 0;
      switch (sortKey) {
        case "name":
          cmp = a.rule.name.localeCompare(b.rule.name);
          break;
        case "bundle":
          cmp = a.bundleName.localeCompare(b.bundleName);
          break;
        case "decision":
          cmp = (a.rule.decision || "").localeCompare(b.rule.decision || "");
          break;
        case "priority":
          cmp = (a.rule.priority ?? 0) - (b.rule.priority ?? 0);
          break;
        case "topics":
          cmp = (a.rule.match?.topics?.[0] || "").localeCompare(
            b.rule.match?.topics?.[0] || "",
          );
          break;
      }
      return sortAsc ? cmp : -cmp;
    });
    return sorted;
  }, [filteredRules, sortKey, sortAsc]);

  const handleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortAsc(!sortAsc);
    } else {
      setSortKey(key);
      setSortAsc(true);
    }
  };

  const SortHeader = ({
    label,
    sortId,
    className,
  }: {
    label: string;
    sortId: SortKey;
    className?: string;
  }) => (
    <th
      className={cn(
        "px-3 py-2.5 text-left font-mono text-xs uppercase tracking-widest text-muted-foreground bg-surface-2 cursor-pointer hover:text-foreground transition-colors select-none",
        className,
      )}
      onClick={() => handleSort(sortId)}
    >
      <span className="flex items-center gap-1">
        {label}
        <ArrowUpDown
          className={cn(
            "w-3 h-3",
            sortKey === sortId ? "text-cordum" : "opacity-30",
          )}
        />
      </span>
    </th>
  );

  return (
    <div className="overflow-hidden rounded-2xl border border-border bg-card">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-xs">
          <thead>
            <tr>
              <SortHeader label="Rule" sortId="name" className="rounded-tl-2xl" />
              <SortHeader label="Bundle" sortId="bundle" />
              <SortHeader label="Topics" sortId="topics" />
              <SortHeader label="Decision" sortId="decision" />
              <SortHeader label="Priority" sortId="priority" />
              <th className="px-3 py-2.5 text-left font-mono text-xs uppercase tracking-widest text-muted-foreground bg-surface-2 rounded-tr-2xl">
                Risk Tags
              </th>
            </tr>
          </thead>
          <tbody>
            {sortedRules.map((fr, i) => (
              <tr
                key={`${fr.bundleId}:${fr.rule.id}:${i}`}
                className={cn(
                  "border-b border-border/30 last:border-b-0 transition-colors",
                  "hover:bg-cordum/[0.03] cursor-pointer",
                  expandedRule === `${fr.bundleId}:${fr.rule.id}` && "bg-cordum/[0.05]",
                  !fr.rule.enabled && "opacity-50",
                )}
                onClick={() =>
                  setExpandedRule(
                    expandedRule === `${fr.bundleId}:${fr.rule.id}`
                      ? null
                      : `${fr.bundleId}:${fr.rule.id}`,
                  )
                }
              >
                <td className="px-3 py-2.5 font-mono">
                  <div className="flex items-center gap-2">
                    <span className="text-foreground font-medium">{fr.rule.name}</span>
                    {!fr.rule.enabled && (
                      <StatusBadge variant="muted" className="text-xs">Off</StatusBadge>
                    )}
                  </div>
                  {fr.rule.reason && (
                    <p className="text-xs text-muted-foreground/60 italic mt-0.5 truncate max-w-[200px]">
                      {fr.rule.reason}
                    </p>
                  )}
                </td>
                <td className="px-3 py-2.5 font-mono text-cordum">
                  {fr.bundleName}
                </td>
                <td className="px-3 py-2.5 font-mono text-muted-foreground">
                  {fr.rule.match?.topics?.join(", ") || "—"}
                </td>
                <td className="px-3 py-2.5">
                  <SafetyDecisionBadge decision={fr.rule.decision} />
                </td>
                <td className="px-3 py-2.5 font-mono text-muted-foreground text-center">
                  {fr.rule.priority}
                </td>
                <td className="px-3 py-2.5 font-mono text-muted-foreground">
                  {fr.rule.match?.risk_tags?.join(", ") || "—"}
                </td>
              </tr>
            ))}
            {sortedRules.length === 0 && (
              <tr>
                <td colSpan={6} className="px-3 py-8 text-center text-muted-foreground">
                  No rules found.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
