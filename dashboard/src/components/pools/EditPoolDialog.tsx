import { useState, useEffect } from "react";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Settings } from "lucide-react";

interface EditPoolDialogProps {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: { requires: string[]; description: string }) => void;
  isPending?: boolean;
  poolName: string;
  currentRequires: string[];
  currentDescription: string;
}

export function EditPoolDialog({ open, onClose, onSubmit, isPending, poolName, currentRequires, currentDescription }: EditPoolDialogProps) {
  const [requires, setRequires] = useState(currentRequires.join(", "));
  const [description, setDescription] = useState(currentDescription);

  useEffect(() => {
    if (open) {
      setRequires(currentRequires.join(", "));
      setDescription(currentDescription);
    }
  }, [open, currentRequires, currentDescription]);

  return (
    <ConfirmDialog
      open={open}
      onClose={onClose}
      onConfirm={() => {
        const reqList = requires.split(",").map((s) => s.trim()).filter(Boolean);
        onSubmit({ requires: reqList, description: description.trim() });
      }}
      title={`Edit Pool: ${poolName}`}
      icon={Settings}
      confirmLabel="Save"
      isPending={isPending}
      description={
        <div className="space-y-3 mt-2">
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
              placeholder="Pool description"
              className="mt-1 w-full h-9 px-3 text-xs bg-surface-0 border border-border rounded-2xl text-foreground placeholder:text-muted-foreground/40 focus:outline-none focus:ring-1 focus:ring-cordum"
            />
          </div>
        </div>
      }
    />
  );
}
