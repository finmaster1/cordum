import { useState } from "react";
import { usePolicyBundleContext } from "../components/policy/PolicyBundleContext";
import { PolicyBundleEditor } from "../components/policy/PolicyBundleEditor";
import { VisualRuleBuilder } from "../components/policy/VisualRuleBuilder";
import { cn } from "../lib/utils";
import { usePageTitle } from "../hooks/usePageTitle";
import { usePolicyBundle } from "../hooks/usePolicies";
import { EmptyState } from "../components/ui/EmptyState";

type BuilderTab = "visual" | "yaml";

export default function PoliciesBuilderPage() {
  usePageTitle("Policies - Builder");
  const { bundleId } = usePolicyBundleContext();
  const { data: bundle } = usePolicyBundle(bundleId);
  const [tab, setTab] = useState<BuilderTab>("visual");

  if (!bundleId) {
    return (
      <EmptyState title="No policy bundles found." description="Create one to get started." />
    );
  }

  return (
    <div className="space-y-4">
      {/* Tab toggle */}
      <div className="flex gap-1 rounded-xl border border-border bg-surface2/40 p-1 w-fit">
        {(["visual", "yaml"] as const).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={cn(
              "rounded-2xl px-4 py-1.5 text-xs font-semibold transition",
              tab === t
                ? "bg-surface1 text-ink shadow-sm"
                : "text-muted-foreground hover:text-ink",
            )}
          >
            {t === "visual" ? "Visual" : "YAML"}
          </button>
        ))}
      </div>

      {/* Tab content */}
      {tab === "visual" ? (
        <VisualRuleBuilder bundleId={bundleId} onEditYaml={() => setTab("yaml")} />
      ) : (
        <PolicyBundleEditor
          bundleId={bundleId}
          currentContent={bundle?.content ?? ""}
          onClose={() => setTab("visual")}
        />
      )}
    </div>
  );
}
