import { cn } from "@/lib/utils";
import { Search, X, Target } from "lucide-react";

export type PolicyScope = "all" | "global" | "tenant" | "workflow";

interface PolicyFilterBarProps {
  searchText: string;
  onSearchChange: (text: string) => void;
  tenantFilter: string;
  onTenantFilterChange: (text: string) => void;
  topicFilter: string;
  onTopicFilterChange: (text: string) => void;
  capabilityFilter: string;
  onCapabilityFilterChange: (text: string) => void;
  scope: PolicyScope;
  onScopeChange: (scope: PolicyScope) => void;
  scopeCounts: Record<PolicyScope, number>;
  onClear: () => void;
  hasActiveFilter: boolean;
}

const scopes: { id: PolicyScope; label: string }[] = [
  { id: "all", label: "All" },
  { id: "global", label: "Global" },
  { id: "tenant", label: "Tenant" },
  { id: "workflow", label: "Workflow" },
];

export function PolicyFilterBar({
  searchText,
  onSearchChange,
  tenantFilter,
  onTenantFilterChange,
  topicFilter,
  onTopicFilterChange,
  capabilityFilter,
  onCapabilityFilterChange,
  scope,
  onScopeChange,
  scopeCounts,
  onClear,
  hasActiveFilter,
}: PolicyFilterBarProps) {
  return (
    <div className="instrument-card !p-3.5">
      <div className="flex flex-wrap items-center gap-2.5">
        {/* Icon + label */}
        <div className="flex items-center gap-2 shrink-0">
          <Target className="w-4 h-4 text-cordum" />
          <span className="text-xs font-semibold text-foreground whitespace-nowrap">
            What affects...
          </span>
        </div>

        {/* Search input */}
        <div className="relative flex-1 min-w-[120px] max-w-[200px]">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
          <input
            type="text"
            value={searchText}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder="Search rules..."
            className="w-full h-8 pl-8 pr-3 bg-surface-2 border border-border rounded-full font-mono text-xs text-foreground placeholder:text-muted-foreground outline-none focus:border-cordum transition-colors"
          />
        </div>

        {/* Tenant filter */}
        <input
          type="text"
          value={tenantFilter}
          onChange={(e) => onTenantFilterChange(e.target.value)}
          placeholder="Tenant"
          className="h-8 px-3 bg-surface-2 border border-border rounded-full font-mono text-xs text-foreground placeholder:text-muted-foreground outline-none focus:border-cordum transition-colors min-w-[100px] max-w-[140px]"
        />

        {/* Topic filter */}
        <input
          type="text"
          value={topicFilter}
          onChange={(e) => onTopicFilterChange(e.target.value)}
          placeholder="Topic (e.g. job.aws.*)"
          className="h-8 px-3 bg-surface-2 border border-border rounded-full font-mono text-xs text-foreground placeholder:text-muted-foreground outline-none focus:border-cordum transition-colors min-w-[140px] max-w-[220px]"
        />

        {/* Capability filter */}
        <input
          type="text"
          value={capabilityFilter}
          onChange={(e) => onCapabilityFilterChange(e.target.value)}
          placeholder="Capability"
          className="h-8 px-3 bg-surface-2 border border-border rounded-full font-mono text-xs text-foreground placeholder:text-muted-foreground outline-none focus:border-cordum transition-colors min-w-[100px] max-w-[140px]"
        />

        {/* Clear */}
        {hasActiveFilter && (
          <button
            type="button"
            onClick={onClear}
            className="flex items-center gap-1 h-8 px-3 rounded-full text-xs font-mono border border-border text-muted-foreground hover:border-destructive hover:text-destructive transition-all"
          >
            <X className="w-3 h-3" />
            Clear
          </button>
        )}

        {/* Separator */}
        <div className="w-px h-5 bg-border" />

        {/* Scope pills */}
        <div className="flex gap-1.5 flex-wrap">
          {scopes.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => onScopeChange(s.id)}
              className={cn(
                "px-3 py-1 rounded-full font-mono text-xs transition-all",
                scope === s.id
                  ? "bg-cordum/15 text-cordum"
                  : "bg-surface-2 text-muted-foreground hover:bg-surface-3 hover:text-foreground",
              )}
            >
              {s.label}
              <span className="ml-1 opacity-60">({scopeCounts[s.id]})</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
