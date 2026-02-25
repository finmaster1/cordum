import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import {
  Package, Search, Download, CheckCircle2, Settings, Trash2,
  ExternalLink, Star, ArrowUpRight
} from "lucide-react";

const INSTALLED_PACKS = [
  { name: "slack", version: "1.2.0", description: "Slack ChatOps notifications and approvals", topics: ["job.slack.send", "job.slack.approve"], workflows: 2, schemas: 3 },
  { name: "github", version: "2.0.1", description: "GitHub repository management and PR automation", topics: ["job.github.create-pr", "job.github.merge", "job.github.review"], workflows: 4, schemas: 5 },
  { name: "datadog", version: "1.0.3", description: "Datadog monitoring and alerting integration", topics: ["job.datadog.alert", "job.datadog.metric"], workflows: 1, schemas: 2 },
  { name: "jira", version: "1.1.0", description: "Jira issue tracking and project management", topics: ["job.jira.create", "job.jira.update", "job.jira.transition"], workflows: 3, schemas: 4 },
];

const MARKETPLACE_PACKS = [
  { name: "pagerduty", version: "1.0.0", description: "PagerDuty incident management", installs: 2400, rating: 4.8, installed: false },
  { name: "aws", version: "3.1.0", description: "AWS service management and automation", installs: 5200, rating: 4.9, installed: false },
  { name: "kubernetes", version: "2.0.0", description: "Kubernetes cluster management", installs: 3100, rating: 4.7, installed: false },
  { name: "slack", version: "1.2.0", description: "Slack ChatOps notifications", installs: 8900, rating: 4.9, installed: true },
  { name: "terraform", version: "1.3.0", description: "Terraform infrastructure as code", installs: 1800, rating: 4.6, installed: false },
  { name: "github", version: "2.0.1", description: "GitHub repository management", installs: 7200, rating: 4.8, installed: true },
];

export default function PacksPage() {
  const [tab, setTab] = useState<"installed" | "marketplace">("installed");
  const [search, setSearch] = useState("");

  const tabs = [
    { id: "installed" as const, label: "Installed", count: INSTALLED_PACKS.length },
    { id: "marketplace" as const, label: "Marketplace", count: MARKETPLACE_PACKS.length },
  ];

  const filteredInstalled = INSTALLED_PACKS.filter(p =>
    p.name.toLowerCase().includes(search.toLowerCase()) ||
    p.description.toLowerCase().includes(search.toLowerCase())
  );

  const filteredMarketplace = MARKETPLACE_PACKS.filter(p =>
    p.name.toLowerCase().includes(search.toLowerCase()) ||
    p.description.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">EXTEND / Packs</p>
        <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">Packs</h1>
        <p className="text-sm text-[var(--muted-foreground)] mt-1">Browse and install capability packs from the catalog.</p>
      </div>

      {/* Tabs + Search */}
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-1 bg-[var(--surface-0)] rounded-lg p-1">
          {tabs.map(t => (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              className={`px-4 py-2 text-sm font-medium rounded-md transition-all ${
                tab === t.id
                  ? "bg-[var(--cordum)]/10 text-[var(--cordum)]"
                  : "text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
              }`}
            >
              {t.label}
              <span className="ml-2 text-xs font-mono opacity-60">{t.count}</span>
            </button>
          ))}
        </div>
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-[var(--muted-foreground)]" />
          <input
            type="text"
            placeholder="Search packs..."
            value={search}
            onChange={e => setSearch(e.target.value)}
            className="pl-9 pr-4 py-2 w-[280px] bg-[var(--surface-0)] border border-[var(--border)] rounded-lg text-sm text-[var(--foreground)] placeholder:text-[var(--muted-foreground)] focus:outline-none focus:ring-1 focus:ring-[var(--cordum)]"
          />
        </div>
      </div>

      {/* Installed Tab */}
      {tab === "installed" && (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          <AnimatePresence>
            {filteredInstalled.map((pack, i) => (
              <motion.div
                key={pack.name}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.05 }}
                className="instrument-card"
              >
                <div className="p-5 space-y-4">
                  <div className="flex items-start justify-between">
                    <div className="flex items-center gap-3">
                      <div className="w-10 h-10 rounded-lg bg-[var(--cordum)]/10 flex items-center justify-center">
                        <Package className="w-5 h-5 text-[var(--cordum)]" />
                      </div>
                      <div>
                        <h3 className="font-display font-semibold text-[var(--foreground)]">{pack.name}</h3>
                        <span className="text-xs font-mono text-[var(--muted-foreground)]">v{pack.version}</span>
                      </div>
                    </div>
                    <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-400 bg-emerald-400/10 px-2 py-0.5 rounded-full">
                      <CheckCircle2 className="w-3 h-3" /> Installed
                    </span>
                  </div>
                  <p className="text-sm text-[var(--muted-foreground)]">{pack.description}</p>
                  <div className="space-y-2">
                    <div className="flex flex-wrap gap-1">
                      {pack.topics.map(t => (
                        <span key={t} className="text-xs font-mono bg-[var(--surface-2)] text-[var(--muted-foreground)] px-2 py-0.5 rounded">{t}</span>
                      ))}
                    </div>
                    <div className="flex items-center gap-4 text-xs text-[var(--muted-foreground)]">
                      <span>Workflows: <span className="font-mono text-[var(--foreground)]">{pack.workflows}</span></span>
                      <span>Schemas: <span className="font-mono text-[var(--foreground)]">{pack.schemas}</span></span>
                    </div>
                  </div>
                  <div className="flex items-center gap-2 pt-2 border-t border-[var(--border)]">
                    <button className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-[var(--surface-2)] text-[var(--foreground)] rounded-md hover:bg-[var(--surface-3)] transition-colors">
                      <Settings className="w-3 h-3" /> Configure
                    </button>
                    <button className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-red-400 hover:bg-red-400/10 rounded-md transition-colors">
                      <Trash2 className="w-3 h-3" /> Uninstall
                    </button>
                  </div>
                </div>
              </motion.div>
            ))}
          </AnimatePresence>
        </div>
      )}

      {/* Marketplace Tab */}
      {tab === "marketplace" && (
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
          <AnimatePresence>
            {filteredMarketplace.map((pack, i) => (
              <motion.div
                key={pack.name}
                initial={{ opacity: 0, y: 12 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: i * 0.05 }}
                className="instrument-card cursor-pointer hover:border-[var(--cordum)]/30 transition-colors"
              >
                <div className="p-5 space-y-4">
                  <div className="flex items-start justify-between">
                    <div className="flex items-center gap-3">
                      <div className="w-10 h-10 rounded-lg bg-[var(--surface-2)] flex items-center justify-center">
                        <Package className="w-5 h-5 text-[var(--muted-foreground)]" />
                      </div>
                      <div>
                        <h3 className="font-display font-semibold text-[var(--foreground)]">{pack.name}</h3>
                        <span className="text-xs font-mono text-[var(--muted-foreground)]">v{pack.version}</span>
                      </div>
                    </div>
                    {pack.installed ? (
                      <span className="inline-flex items-center gap-1 text-xs font-medium text-emerald-400 bg-emerald-400/10 px-2 py-0.5 rounded-full">
                        <CheckCircle2 className="w-3 h-3" /> Installed
                      </span>
                    ) : (
                      <button className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-[var(--cordum)] text-[var(--surface-0)] rounded-md hover:bg-[var(--cordum-dim)] transition-colors">
                        <Download className="w-3 h-3" /> Install
                      </button>
                    )}
                  </div>
                  <p className="text-sm text-[var(--muted-foreground)]">{pack.description}</p>
                  <div className="flex items-center gap-4 text-xs text-[var(--muted-foreground)]">
                    <span className="flex items-center gap-1">
                      <Download className="w-3 h-3" />
                      {pack.installs >= 1000 ? `${(pack.installs / 1000).toFixed(1)}k` : pack.installs} installs
                    </span>
                    <span className="flex items-center gap-1">
                      <Star className="w-3 h-3 text-amber-400" />
                      {pack.rating}
                    </span>
                  </div>
                </div>
              </motion.div>
            ))}
          </AnimatePresence>
        </div>
      )}
    </div>
  );
}
