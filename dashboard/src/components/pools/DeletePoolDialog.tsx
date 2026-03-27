import { useState } from "react";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Trash2 } from "lucide-react";

interface DeletePoolDialogProps {
  open: boolean;
  onClose: () => void;
  onConfirm: (force: boolean) => void;
  isPending?: boolean;
  poolName: string;
  topicCount: number;
  workerCount: number;
}

export function DeletePoolDialog({ open, onClose, onConfirm, isPending, poolName, topicCount, workerCount }: DeletePoolDialogProps) {
  const [force, setForce] = useState(false);
  const hasTopics = topicCount > 0;

  return (
    <ConfirmDialog
      open={open}
      onClose={() => { setForce(false); onClose(); }}
      onConfirm={() => onConfirm(force)}
      title={`Delete Pool: ${poolName}`}
      icon={Trash2}
      variant="destructive"
      confirmLabel={force ? "Force Delete" : "Delete"}
      confirmText={poolName}
      isPending={isPending}
      description={
        <div className="space-y-2">
          <p>This will permanently remove the pool and its configuration.</p>
          {workerCount > 0 && (
            <p className="text-warning text-xs">{workerCount} worker(s) are currently assigned to this pool.</p>
          )}
          {hasTopics && (
            <div className="mt-2">
              <p className="text-destructive text-xs">{topicCount} topic(s) are mapped to this pool.</p>
              <label className="mt-2 flex items-center gap-2 text-xs text-foreground">
                <input
                  type="checkbox"
                  checked={force}
                  onChange={(e) => setForce(e.target.checked)}
                  className="rounded border-border"
                />
                Force delete (remove topic mappings)
              </label>
            </div>
          )}
        </div>
      }
    />
  );
}
