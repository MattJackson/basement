import { useState } from "react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";

export interface DeleteClusterConfirmProps {
  open: boolean;
  clusterLabel: string;
  isDeleting?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * Type-name-to-confirm guard for cluster deletion.
 *
 * Deletion is irrevocable at the storage layer; removing a connection
 * leaves all buckets and keys orphaned (inaccessible). We force the user
 * to type the cluster label exactly before Delete is enabled — matches
 * GitHub / Vercel / DigitalOcean conventions for high-blast-radius destructive ops.
 *
 * Reset behavior: the typed input resets whenever the dialog is
 * (re-)opened. Mismatch is not surfaced as an error — Delete just
 * stays disabled.
 */
export function DeleteClusterConfirm({
  open,
  clusterLabel,
  isDeleting,
  onConfirm,
  onCancel,
}: DeleteClusterConfirmProps) {
  const [typed, setTyped] = useState("");
  const [lastSeenKey, setLastSeenKey] = useState<string | null>(null);
  const key = open ? `${clusterLabel}::open` : null;
  
  if (key !== null && key !== lastSeenKey) {
    setLastSeenKey(key);
    if (typed !== "") setTyped("");
  }
  if (key === null && lastSeenKey !== null) {
    setLastSeenKey(null);
  }

  const matches = typed.trim() === clusterLabel;

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete cluster?</AlertDialogTitle>
          <AlertDialogDescription>
            This is permanent — the connection will be removed and all its buckets
            and keys will become inaccessible. To confirm, type{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-foreground">
              {clusterLabel}
            </code>{" "}
            below.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <Input
          autoFocus
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder={clusterLabel}
          aria-label="Type cluster label to confirm"
          data-testid="delete-cluster-confirm-input"
        />
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            disabled={!matches || isDeleting}
            data-testid="delete-cluster-confirm-action"
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {isDeleting ? "Deleting…" : "Delete cluster"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
