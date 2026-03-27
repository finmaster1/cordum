import { useState } from "react";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Timer } from "lucide-react";

interface DrainPoolDialogProps {
  open: boolean;
  onClose: () => void;
  onConfirm: (timeoutSeconds: number) => void;
  isPending?: boolean;
  poolName: string;
  workerCount: number;
}

export function DrainPoolDialog({ open, onClose, onConfirm, isPending, poolName, workerCount }: DrainPoolDialogProps) {
  const [timeout, setTimeout] = useState(300);

  return (
    <ConfirmDialog
      open={open}
      onClose={onClose}
      onConfirm={() => onConfirm(timeout)}
      title={`Drain Pool: ${poolName}`}
      icon={Timer}
      confirmLabel="Start Drain"
      isPending={isPending}
      description={
        <div className="space-y-3">
          <p>Draining stops new job routing to this pool. In-flight jobs on {workerCount} worker(s) will complete normally.</p>
          <p className="text-xs text-muted-foreground">The pool auto-transitions to inactive when all jobs finish or the timeout expires.</p>
          <div>
            <label className="text-xs font-medium text-foreground">Drain Timeout (seconds)</label>
            <input
              type="number"
              value={timeout}
              onChange={(e) => setTimeout(Math.max(0, parseInt(e.target.value) || 0))}
              min={0}
              className="mt-1 w-full h-9 px-3 text-xs bg-surface-0 border border-border rounded-2xl text-foreground focus:outline-none focus:ring-1 focus:ring-cordum"
            />
          </div>
        </div>
      }
    />
  );
}
