import { useState, useMemo, useCallback } from "react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Tabs } from "@/components/ui/Tabs";
import {
  PostureSummary,
  PolicyFilterBar,
  BundleOverviewCard,
  AllRulesTable,
  ByTopicTable,
  type PolicyScope,
} from "@/components/policy/overview";
import { usePolicyBundles, usePolicyRules } from "@/hooks/usePolicies";
import { Loader2 } from "lucide-react";
import type { PolicyBundle, PolicyRule } from "@/api/types";

/* ── helpers ── */

function matchesScope(bundle: PolicyBundle, scope: PolicyScope): boolean {
  if (scope === "all") return true;
  const rules = bundle.rules ?? [];
  if (scope === "global") {
    return rules.some(
      (r) =>
        !r.match?.tenants || r.match.tenants.length === 0,
    );
  }
  if (scope === "tenant") {
    return rules.some(
      (r) => r.match?.tenants && r.match.tenants.length > 0,
    );
  }
  if (scope === "workflow") {
    return rules.some(
      (r) =>
        r.match?.topics?.some((t) => t.includes("workflow")) ||
        r.match?.capabilities?.some((c) => c.includes("workflow")),
    );
  }
  return true;
}

function matchesFilter(
  bundle: PolicyBundle,
  searchText: string,
  tenantFilter: string,
  topicFilter: string,
  capabilityFilter: string,
): boolean {
  const rules = bundle.rules ?? [];
  const lower = searchText.toLowerCase();
  const tenantLower = tenantFilter.toLowerCase();
  const topicLower = topicFilter.toLowerCase();
  const capLower = capabilityFilter.toLowerCase();

  // Bundle-level match
  const bundleMatch =
    !lower ||
    bundle.name.toLowerCase().includes(lower) ||
    bundle.id.toLowerCase().includes(lower);

  // Rule-level match
  const ruleMatch =
    !lower ||
    rules.some(
      (r) =>
        r.name.toLowerCase().includes(lower) ||
        r.id.toLowerCase().includes(lower) ||
        r.decision?.toLowerCase().includes(lower) ||
        r.reason?.toLowerCase().includes(lower),
    );

  // Tenant filter
  const tenantMatch =
    !tenantLower ||
    rules.some(
      (r) =>
        r.match?.tenants?.some((t) => t.toLowerCase().includes(tenantLower)),
    );

  // Topic filter
  const topicMatch =
    !topicLower ||
    rules.some(
      (r) =>
        r.match?.topics?.some((t) => t.toLowerCase().includes(topicLower)),
    );

  // Capability filter
  const capMatch =
    !capLower ||
    rules.some(
      (r) =>
        r.match?.capabilities?.some((c) =>
          c.toLowerCase().includes(capLower),
        ),
    );

  return (bundleMatch || ruleMatch) && tenantMatch && topicMatch && capMatch;
}

function countScopeRules(bundles: PolicyBundle[], scope: PolicyScope): number {
  if (scope === "all") {
    return bundles.reduce((sum, b) => sum + (b.rules?.length ?? 0), 0);
  }
  return bundles
    .filter((b) => matchesScope(b, scope))
    .reduce((sum, b) => sum + (b.rules?.length ?? 0), 0);
}

/* ── page ── */

type ViewTab = "bundles" | "all-rules" | "by-topic";

const viewTabs = [
  { id: "bundles" as const, label: "Bundles" },
  { id: "all-rules" as const, label: "All Rules" },
  { id: "by-topic" as const, label: "By Topic" },
];

export default function PolicyOverviewPage() {
  // Data
  const { data: bundlesRes, isLoading: bundlesLoading } = usePolicyBundles();
  const { data: rulesRes, isLoading: rulesLoading } = usePolicyRules();

  // Filters
  const [searchText, setSearchText] = useState("");
  const [tenantFilter, setTenantFilter] = useState("");
  const [topicFilter, setTopicFilter] = useState("");
  const [capabilityFilter, setCapabilityFilter] = useState("");
  const [scope, setScope] = useState<PolicyScope>("all");
  const [activeView, setActiveView] = useState<ViewTab>("bundles");

  const bundles = bundlesRes?.items ?? [];
  const allRules = rulesRes?.items ?? [];

  const hasActiveFilter =
    searchText !== "" ||
    tenantFilter !== "" ||
    topicFilter !== "" ||
    capabilityFilter !== "" ||
    scope !== "all";

  const clearFilters = useCallback(() => {
    setSearchText("");
    setTenantFilter("");
    setTopicFilter("");
    setCapabilityFilter("");
    setScope("all");
  }, []);

  // Filtered bundles
  const filteredBundles = useMemo(() => {
    return bundles
      .filter((b) => matchesScope(b, scope))
      .filter((b) =>
        matchesFilter(b, searchText, tenantFilter, topicFilter, capabilityFilter),
      );
  }, [bundles, scope, searchText, tenantFilter, topicFilter, capabilityFilter]);

  // Scope counts
  const scopeCounts = useMemo(
    () => ({
      all: bundles.reduce((sum, b) => sum + (b.rules?.length ?? 0), 0),
      global: countScopeRules(bundles, "global"),
      tenant: countScopeRules(bundles, "tenant"),
      workflow: countScopeRules(bundles, "workflow"),
    }),
    [bundles],
  );

  // Tab counts
  const tabsWithCounts = useMemo(() => {
    const totalRules = filteredBundles.reduce(
      (sum, b) => sum + (b.rules?.length ?? 0),
      0,
    );
    return viewTabs.map((t) => ({
      ...t,
      count:
        t.id === "bundles"
          ? filteredBundles.length
          : t.id === "all-rules"
            ? totalRules
            : totalRules,
    }));
  }, [filteredBundles]);

  // Combined search text for child components
  const combinedFilter = [searchText, tenantFilter, topicFilter, capabilityFilter]
    .filter(Boolean)
    .join(" ");

  const isLoading = bundlesLoading || rulesLoading;

  return (
    <div className="space-y-6 animate-rise">
      {/* Page Header */}
      <PageHeader
        label="Govern"
        title="Policy Overview"
        subtitle="Your active policy posture. See what bundles are installed, what rules they contain, and what they affect."
      />

      {isLoading ? (
        <div className="flex items-center justify-center py-20">
          <Loader2 className="w-6 h-6 text-cordum animate-spin" />
          <span className="ml-3 text-sm text-muted-foreground">
            Loading policy data...
          </span>
        </div>
      ) : (
        <>
          {/* Posture Summary */}
          <PostureSummary bundles={bundles} allRules={allRules} />

          {/* Filter Bar */}
          <PolicyFilterBar
            searchText={searchText}
            onSearchChange={setSearchText}
            tenantFilter={tenantFilter}
            onTenantFilterChange={setTenantFilter}
            topicFilter={topicFilter}
            onTopicFilterChange={setTopicFilter}
            capabilityFilter={capabilityFilter}
            onCapabilityFilterChange={setCapabilityFilter}
            scope={scope}
            onScopeChange={setScope}
            scopeCounts={scopeCounts}
            onClear={clearFilters}
            hasActiveFilter={hasActiveFilter}
          />

          {/* View Tabs */}
          <Tabs
            tabs={tabsWithCounts}
            activeTab={activeView}
            onChange={(id) => setActiveView(id as ViewTab)}
          />

          {/* Content */}
          {activeView === "bundles" && (
            <div className="space-y-4">
              {filteredBundles.length === 0 ? (
                <div className="text-center py-12 text-muted-foreground">
                  <p className="text-sm">
                    {hasActiveFilter
                      ? "No bundles match the current filters."
                      : "No policy bundles installed."}
                  </p>
                </div>
              ) : (
                <>
                  <span className="text-xs font-mono text-muted-foreground uppercase tracking-widest block">
                    Installed Bundles ({filteredBundles.length})
                  </span>
                  {filteredBundles.map((bundle) => (
                    <BundleOverviewCard
                      key={bundle.id}
                      bundle={bundle}
                      filterText={combinedFilter || undefined}
                    />
                  ))}
                </>
              )}
            </div>
          )}

          {activeView === "all-rules" && (
            <AllRulesTable
              bundles={filteredBundles}
              filterText={combinedFilter || undefined}
            />
          )}

          {activeView === "by-topic" && (
            <ByTopicTable
              bundles={filteredBundles}
              filterText={combinedFilter || undefined}
            />
          )}
        </>
      )}
    </div>
  );
}
