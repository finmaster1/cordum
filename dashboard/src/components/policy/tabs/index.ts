import { lazy } from "react";
import type { LucideIcon } from "lucide-react";

// Lazy-loaded tab content — each loads its respective page with hideHeader=true
export const LazyInputRulesTab = lazy(
  () => import("@/pages/govern/InputRulesPage"),
);
export const LazyOutputRulesTab = lazy(
  () => import("@/pages/govern/OutputRulesPage"),
);
export const LazySimulatorTab = lazy(
  () => import("@/pages/govern/SimulatorPage"),
);
export const LazyBundlesTab = lazy(
  () => import("@/pages/govern/BundlesPage"),
);

export interface TabDefinition {
  id: string;
  label: string;
  count?: number;
}

export const TAB_IDS = [
  "overview",
  "input-rules",
  "output-rules",
  "simulator",
  "bundles",
] as const;

export type PolicyStudioTab = (typeof TAB_IDS)[number];

export function isValidTab(tab: string): tab is PolicyStudioTab {
  return (TAB_IDS as readonly string[]).includes(tab);
}
