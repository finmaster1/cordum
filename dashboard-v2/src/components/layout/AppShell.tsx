import { NavLink, useLocation } from "react-router-dom";
import { type ReactNode, useState, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { useConfigStore } from "@/state/config";
import { useUiStore } from "@/state/ui";
import {
  LayoutGrid,
  ListChecks,
  Workflow,
  Cpu,
  UserCheck,
  Shield,
  Boxes,
  AlertTriangle,
  FileText,
  Settings,
  ChevronLeft,
  ChevronRight,
  Moon,
  Sun,
  LogOut,
  Search,
  Bell,
  Monitor,
  Command,
  ExternalLink,
} from "lucide-react";

const navSections = [
  {
    label: "Core",
    items: [
      { path: "/", label: "Overview", icon: LayoutGrid, end: true },
      { path: "/jobs", label: "Jobs", icon: ListChecks },
      { path: "/workflows", label: "Workflows", icon: Workflow },
      { path: "/agents", label: "Agent Fleet", icon: Cpu },
    ],
  },
  {
    label: "Safety",
    items: [
      { path: "/approvals", label: "Approvals", icon: UserCheck, badge: true },
      { path: "/policies", label: "Policy Studio", icon: Shield },
    ],
  },
  {
    label: "Platform",
    items: [
      { path: "/packs", label: "Packs", icon: Boxes },
      { path: "/schemas", label: "Schemas", icon: Monitor },
      { path: "/dlq", label: "Dead Letters", icon: AlertTriangle, badge: true },
      { path: "/audit", label: "Audit Log", icon: FileText },
    ],
  },
  {
    label: "System",
    items: [{ path: "/settings", label: "Settings", icon: Settings }],
  },
];

interface AppShellProps {
  children: ReactNode;
}

export function AppShell({ children }: AppShellProps) {
  const location = useLocation();
  const [collapsed, setCollapsed] = useState(false);
  const theme = useUiStore((s) => s.resolvedTheme);
  const toggleTheme = useUiStore((s) => s.toggleTheme);
  const user = useConfigStore((s) => s.user);
  const logout = useConfigStore((s) => s.logout);

  // Keyboard shortcut: Cmd/Ctrl + B to toggle sidebar
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "b") {
        e.preventDefault();
        setCollapsed((c) => !c);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  return (
    <div className="flex h-screen overflow-hidden bg-background">
      {/* Sidebar — matches showcase Layout */}
      <aside
        className={cn(
          "fixed top-0 left-0 h-screen z-50 flex flex-col border-r border-border bg-surface-0 transition-all duration-300",
          collapsed ? "w-16" : "w-60",
        )}
      >
        {/* Logo */}
        <div className="flex items-center gap-3 px-4 h-16 border-b border-border shrink-0">
          <div className="w-8 h-8 rounded-lg bg-cordum flex items-center justify-center shrink-0">
            <svg viewBox="0 0 24 24" className="w-5 h-5 text-surface-0" fill="currentColor">
              <path d="M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5" stroke="currentColor" strokeWidth="2" fill="none" strokeLinecap="round" strokeLinejoin="round"/>
            </svg>
          </div>
          {!collapsed && (
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              className="flex flex-col"
            >
              <span className="font-display font-bold text-sm text-foreground tracking-tight">
                Cordum
              </span>
              <span className="text-[10px] text-muted-foreground font-mono uppercase tracking-widest">
                Control Plane
              </span>
            </motion.div>
          )}
        </div>

        {/* Nav items */}
        <nav className="flex-1 py-4 px-2 space-y-5 overflow-y-auto">
          {navSections.map((section) => (
            <div key={section.label}>
              {!collapsed && (
                <p className="px-3 mb-1.5 text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/60">
                  {section.label}
                </p>
              )}
              <div className="space-y-0.5">
                {section.items.map((item) => (
                  <NavLink
                    key={item.path}
                    to={item.path}
                    end={(item as any).end}
                    className={({ isActive }) =>
                      cn(
                        "flex items-center gap-3 px-3 py-2.5 rounded-md text-sm font-medium transition-all duration-150 group relative",
                        isActive
                          ? "bg-cordum/10 text-cordum"
                          : "text-muted-foreground hover:text-foreground hover:bg-surface-2",
                        collapsed && "justify-center px-0",
                      )
                    }
                  >
                    {({ isActive }) => (
                      <>
                        {isActive && (
                          <motion.div
                            layoutId="sidebar-active"
                            className="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-5 bg-cordum rounded-r-full"
                            transition={{ type: "spring", stiffness: 350, damping: 30 }}
                          />
                        )}
                        <item.icon className="w-4.5 h-4.5 shrink-0" />
                        {!collapsed && <span>{item.label}</span>}
                      </>
                    )}
                  </NavLink>
                ))}
              </div>
            </div>
          ))}
        </nav>

        {/* Sidebar footer */}
        <div className="px-2 pb-3 border-t border-border pt-3 space-y-1">
          <a
            href="https://cordum.io"
            target="_blank"
            rel="noopener noreferrer"
            className={cn(
              "flex items-center gap-3 px-3 py-2.5 rounded-md text-sm text-muted-foreground hover:text-foreground hover:bg-surface-2 transition-colors",
              collapsed && "justify-center",
            )}
          >
            <ExternalLink className="w-4 h-4 shrink-0" />
            {!collapsed && <span>cordum.io</span>}
          </a>
          <button
            onClick={toggleTheme}
            className={cn(
              "flex items-center gap-3 w-full px-3 py-2.5 rounded-md text-sm text-muted-foreground hover:text-foreground hover:bg-surface-2 transition-colors",
              collapsed && "justify-center px-0",
            )}
          >
            {theme === "dark" ? (
              <Sun className="w-4 h-4 shrink-0" />
            ) : (
              <Moon className="w-4 h-4 shrink-0" />
            )}
            {!collapsed && <span>Toggle theme</span>}
          </button>
        </div>

        {/* Collapse toggle — floating button like showcase */}
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="absolute -right-3 top-20 w-6 h-6 rounded-full bg-surface-2 border border-border flex items-center justify-center text-muted-foreground hover:text-foreground hover:bg-surface-3 transition-colors"
        >
          {collapsed ? (
            <ChevronRight className="w-3.5 h-3.5" />
          ) : (
            <ChevronLeft className="w-3.5 h-3.5" />
          )}
        </button>
      </aside>

      {/* Main content area */}
      <div
        className={cn(
          "flex-1 flex flex-col overflow-hidden transition-all duration-300",
          collapsed ? "ml-16" : "ml-60",
        )}
      >
        {/* Top bar — matches showcase */}
        <header className="sticky top-0 z-40 flex items-center justify-between h-14 px-6 border-b border-border bg-background/80 backdrop-blur-xl shrink-0">
          <div className="flex items-center gap-4">
            <div className="relative">
              <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
              <input
                type="text"
                placeholder="Search..."
                className="h-8 w-56 pl-8 pr-3 text-xs bg-surface-1 border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-cordum"
              />
              <kbd className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] font-mono px-1.5 py-0.5 rounded bg-background border border-border text-muted-foreground">
                <Command className="w-2.5 h-2.5 inline" />K
              </kbd>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <span className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-mono font-medium bg-emerald-500/15 text-emerald-400 border border-emerald-500/20">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 status-pulse" />
              All Systems Nominal
            </span>
            <button className="relative p-2 rounded-md hover:bg-surface-2 transition-colors">
              <Bell className="w-4 h-4 text-muted-foreground" />
              <span className="absolute top-1.5 right-1.5 w-2 h-2 rounded-full bg-amber-400" />
            </button>
            {user ? (
              <div className="flex items-center gap-2 pl-3 border-l border-border">
                <div className="w-8 h-8 rounded-full bg-cordum/20 border border-cordum/30 flex items-center justify-center">
                  <span className="text-xs font-semibold text-cordum">
                    {(user.display_name || user.username || "U").charAt(0).toUpperCase()}
                  </span>
                </div>
                <button
                  onClick={logout}
                  className="p-1.5 rounded-md text-muted-foreground hover:text-red-400 transition-colors"
                >
                  <LogOut className="w-3.5 h-3.5" />
                </button>
              </div>
            ) : (
              <div className="w-8 h-8 rounded-full bg-cordum/20 border border-cordum/30 flex items-center justify-center">
                <span className="text-xs font-semibold text-cordum">C</span>
              </div>
            )}
          </div>
        </header>

        {/* Page content */}
        <main className="flex-1 overflow-y-auto">
          <motion.div
            key={location.pathname}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.2, ease: "easeOut" }}
            className="p-6"
          >
            {children}
          </motion.div>
        </main>
      </div>
    </div>
  );
}
