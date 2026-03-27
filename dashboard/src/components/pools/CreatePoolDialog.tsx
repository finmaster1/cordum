import { useState } from "react";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Layers } from "lucide-react";

interface CreatePoolDialogProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: { name: string; requires: string[]; description: string }) => void;
  isPending?: boolean;
}

const POOL_NAME_RE = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;

export function CreatePoolDialog({ open, onClose, onSubmit, isPending }: CreatePoolDialogProps) {
  const [name, setName] = useState("");
  const [requires, setRequires] = useState("");
  const [description, setDescription] = useState("");
  const [error, setError] = useState("");

  const handleConfirm = () => {
    const trimmed = name.trim();
    if (trimmed.length < 3 || trimmed.length > 63) {
      setError("Pool name must be 3-63 characters");
      return;
    }
    if (!POOL_NAME_RE.test(trimmed) || trimmed.includes("--")) {
      setError("Lowercase alphanumeric and hyphens only, no consecutive hyphens");
      return;
    }
    setError("");
    const reqList = requires
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
    onSubmit({ name: trimmed, requires: reqList, description: description.trim() });
  };

  const handleClose = () => {
    setName("");
    setRequires("");
    setDescription("");
    setError("");
    onClose();
  };

  return (
    <ConfirmDialog
      open={open}
      onClose={handleClose}
      onConfirm={handleConfirm}
      title="Create Pool"
      icon={Layers}
      confirmLabel="Create"
      isPending={isPending}
      description={
        <div className="space-y-3 mt-2">
          <div>
            <label className="text-xs font-medium text-foreground">Pool Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => { setName(e.target.value); setError(""); }}
              placeholder="my-pool-name"
              className="mt-1 w-full h-9 px-3 text-xs bg-surface-0 border border-border rounded-2xl text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-1 focus:ring-cordum font-mono"
            />
            {error && <p className="mt-1 text-xs text-destructive">{error}</p>}
          </div>
          <div>
            <label className="text-xs font-medium text-foreground">Requires (comma-separated)</label>
            <input
              type="text"
              value={requires}
              onChange={(e) => setRequires(e.target.value)}
              placeholder="docker, gpu"
              className="mt-1 w-full h-9 px-3 text-xs bg-surface-0 border border-border rounded-2xl text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-1 focus:ring-cordum"
            />
          </div>
          <div>
            <label className="text-xs font-medium text-foreground">Description</label>
            <input
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional pool description"
              className="mt-1 w-full h-9 px-3 text-xs bg-surface-0 border border-border rounded-2xl text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-1 focus:ring-cordum"
            />
          </div>
        </div>
      }
    />
  );
}
