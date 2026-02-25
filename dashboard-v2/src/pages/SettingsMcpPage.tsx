import { useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Server, Plus, CheckCircle2, XCircle, Wrench, RefreshCw, Edit3, Trash2, X, ChevronDown, ChevronUp } from "lucide-react";

const MCP_SERVERS = [
  {
    name: "Local Tools Server",
    transport: "stdio",
    command: "npx @cordum/mcp-tools",
    status: "connected",
    tools: 12,
    lastConnected: "2m ago",
    toolList: ["file.read", "file.write", "shell.exec", "http.request", "db.query", "cache.get", "cache.set", "log.write", "metric.push", "alert.send", "config.get", "secret.read"],
  },
  {
    name: "GitHub Integration",
    transport: "http",
    url: "https://mcp.github.com/v1",
    status: "connected",
    tools: 8,
    lastConnected: "5m ago",
    toolList: ["github.pr.create", "github.pr.merge", "github.issue.create", "github.repo.list", "github.branch.create", "github.commit.list", "github.review.request", "github.action.trigger"],
  },
  {
    name: "Slack Bot",
    transport: "http",
    url: "https://mcp.slack.cordum.io",
    status: "disconnected",
    tools: 0,
    lastConnected: "3h ago",
    toolList: [],
  },
];

export default function SettingsMcpPage() {
  const [showAdd, setShowAdd] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-xs font-mono uppercase tracking-wider text-[var(--cordum)] mb-1">SETTINGS</p>
          <h1 className="text-2xl font-display font-bold text-[var(--foreground)]">MCP Server</h1>
          <p className="text-sm text-[var(--muted-foreground)] mt-1">Configure MCP server connections for tool integration.</p>
        </div>
        <button
          onClick={() => setShowAdd(true)}
          className="flex items-center gap-2 px-4 py-2 bg-[var(--cordum)] text-[var(--surface-0)] text-sm font-medium rounded-lg hover:bg-[var(--cordum-dim)] transition-colors"
        >
          <Plus className="w-4 h-4" /> Add MCP Server
        </button>
      </div>

      <div className="space-y-4">
        {MCP_SERVERS.map((server, i) => {
          const isConnected = server.status === "connected";
          const isExpanded = expanded === server.name;
          return (
            <motion.div
              key={server.name}
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.08 }}
              className="instrument-card"
            >
              <div className="p-5 space-y-4">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-3">
                    <div className={`w-10 h-10 rounded-lg flex items-center justify-center ${isConnected ? "bg-emerald-400/10" : "bg-red-400/10"}`}>
                      <Server className={`w-5 h-5 ${isConnected ? "text-emerald-400" : "text-red-400"}`} />
                    </div>
                    <div>
                      <h3 className="font-display font-semibold text-[var(--foreground)]">{server.name}</h3>
                      <div className="flex items-center gap-2 mt-0.5">
                        <span className={`inline-flex items-center gap-1 text-xs font-medium ${isConnected ? "text-emerald-400" : "text-red-400"}`}>
                          {isConnected ? <CheckCircle2 className="w-3 h-3" /> : <XCircle className="w-3 h-3" />}
                          {server.status}
                        </span>
                        <span className="text-xs text-[var(--muted-foreground)]">·</span>
                        <span className="text-xs text-[var(--muted-foreground)]">{server.transport.toUpperCase()}</span>
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <button className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-[var(--surface-2)] text-[var(--foreground)] rounded-md hover:bg-[var(--surface-3)] transition-colors">
                      <RefreshCw className="w-3 h-3" /> Test
                    </button>
                    <button className="p-2 hover:bg-[var(--surface-2)] rounded-md transition-colors">
                      <Edit3 className="w-4 h-4 text-[var(--muted-foreground)]" />
                    </button>
                    <button className="p-2 hover:bg-red-400/10 rounded-md transition-colors">
                      <Trash2 className="w-4 h-4 text-red-400" />
                    </button>
                  </div>
                </div>

                <div className="flex items-center gap-6 text-xs text-[var(--muted-foreground)]">
                  <span>{server.transport === "stdio" ? `Command: ${server.command}` : `URL: ${server.url}`}</span>
                  <span>Tools: <span className="font-mono text-[var(--foreground)]">{server.tools}</span></span>
                  <span>Last connected: {server.lastConnected}</span>
                </div>

                {/* Tool Discovery */}
                {isConnected && server.toolList.length > 0 && (
                  <div>
                    <button
                      onClick={() => setExpanded(isExpanded ? null : server.name)}
                      className="flex items-center gap-1.5 text-xs font-medium text-[var(--cordum)] hover:text-[var(--cordum-dim)] transition-colors"
                    >
                      <Wrench className="w-3 h-3" />
                      {server.tools} available tools
                      {isExpanded ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                    </button>
                    <AnimatePresence>
                      {isExpanded && (
                        <motion.div
                          initial={{ height: 0, opacity: 0 }}
                          animate={{ height: "auto", opacity: 1 }}
                          exit={{ height: 0, opacity: 0 }}
                          className="overflow-hidden"
                        >
                          <div className="flex flex-wrap gap-1.5 mt-3 pt-3 border-t border-[var(--border)]">
                            {server.toolList.map(tool => (
                              <span key={tool} className="text-xs font-mono bg-[var(--surface-2)] text-[var(--muted-foreground)] px-2 py-0.5 rounded">{tool}</span>
                            ))}
                          </div>
                        </motion.div>
                      )}
                    </AnimatePresence>
                  </div>
                )}
              </div>
            </motion.div>
          );
        })}
      </div>
    </div>
  );
}
