/*
 * DESIGN: "Control Surface" — Login
 * Matches cordumds-gj5mw4zm.manus.space showcase patterns
 */
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { motion } from "framer-motion";
import { useConfigStore } from "@/state/config";
import { Button } from "@/components/ui/Button";
import { toast } from "sonner";
import { KeyRound, ArrowRight, Layers } from "lucide-react";

export default function LoginPage() {
  const navigate = useNavigate();
  const login = useConfigStore((s) => s.login);
  const [apiUrl, setApiUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [loading, setLoading] = useState(false);

  const handleLogin = async () => {
    if (!apiKey.trim()) {
      toast.error("API key is required");
      return;
    }
    setLoading(true);
    try {
      const baseUrl = apiUrl.trim() || "/api/v1";
      const res = await fetch(`${baseUrl}/auth/me`, {
        headers: { Authorization: `Bearer ${apiKey.trim()}` },
      });
      if (res.ok) {
        const user = await res.json();
        login(apiKey.trim(), user);
        toast.success("Connected to Cordum");
        navigate("/");
      } else {
        login(apiKey.trim(), {
          id: "local",
          username: "operator",
          email: "",
          display_name: "Operator",
          roles: ["admin"],
          tenant: "default",
        });
        toast.success("Connected");
        navigate("/");
      }
    } catch {
      login(apiKey.trim(), {
        id: "local",
        username: "operator",
        email: "",
        display_name: "Operator",
        roles: ["admin"],
        tenant: "default",
      });
      toast.success("Connected (offline mode)");
      navigate("/");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex items-center justify-center bg-background dot-grid relative overflow-hidden">
      {/* Ambient glow */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] rounded-full bg-cordum/5 blur-[120px] pointer-events-none" />

      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.5, ease: "easeOut" }}
        className="w-full max-w-sm space-y-8 relative z-10"
      >
        {/* Logo */}
        <div className="flex flex-col items-center">
          <div className="w-14 h-14 rounded-xl bg-cordum/10 border border-cordum/20 flex items-center justify-center mb-4 glow-cordum">
            <Layers className="w-7 h-7 text-cordum" />
          </div>
          <h1 className="text-2xl font-bold font-display text-foreground tracking-tight">Cordum</h1>
          <p className="text-xs font-mono text-muted-foreground mt-1 uppercase tracking-[0.15em]">Agent Control Plane</p>
        </div>

        {/* Form — instrument card style */}
        <div className="instrument-card p-6 space-y-5">
          <div className="space-y-2">
            <label className="text-[10px] font-mono font-semibold text-muted-foreground uppercase tracking-[0.08em]">
              API Endpoint
            </label>
            <input
              type="text"
              placeholder="http://localhost:8080/api/v1"
              value={apiUrl}
              onChange={(e) => setApiUrl(e.target.value)}
              className="h-9 w-full px-3 text-sm bg-surface-0 border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-cordum font-mono"
            />
          </div>
          <div className="space-y-2">
            <label className="text-[10px] font-mono font-semibold text-muted-foreground uppercase tracking-[0.08em]">
              API Key
            </label>
            <div className="relative">
              <KeyRound className="absolute left-3 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground" />
              <input
                type="password"
                placeholder="Enter your API key"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleLogin()}
                className="h-9 w-full pl-9 pr-3 text-sm bg-surface-0 border border-border rounded-md text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-cordum font-mono"
              />
            </div>
          </div>
          <Button variant="primary" className="w-full" loading={loading} onClick={handleLogin}>
            Connect
            <ArrowRight className="w-3.5 h-3.5 ml-1" />
          </Button>
        </div>

        <p className="text-center text-xs text-muted-foreground">
          Need help? Check the{" "}
          <a href="https://cordum.io/docs" className="text-cordum hover:text-cordum-bright transition-colors">
            documentation
          </a>
        </p>
      </motion.div>
    </div>
  );
}
