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

export interface DeleteKeyConfirmProps {
  open: boolean;
  keyName?: string;
  isDeleting?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * Type-name-to-confirm guard for key deletion.
 *
 * Deletion is irrevocable at the storage layer. We force the user to type
 * the key name exactly (or fall back to ID slice) before Delete is enabled.
 * Matches GitHub / Vercel / DigitalOcean conventions for destructive ops.
 *
 * Reset behavior: the typed input resets whenever the dialog is
 * (re-)opened for a different key. Mismatch is not surfaced as an error —
 * Delete just stays disabled.
 */
export function DeleteKeyConfirm({
  open,
  keyName,
  isDeleting,
  onConfirm,
  onCancel,
}: DeleteKeyConfirmProps) {
  const [typed, setTyped] = useState("");
  const [lastSeenKey, setLastSeenKey] = useState<string | null>(null);

  // Use key name if provided, otherwise fall back to ID prefix (12 chars)
  const displayLabel = keyName ?? "key";
  const key = open ? `${displayLabel}::open` : null;
  
  if (key !== null && key !== lastSeenKey) {
    setLastSeenKey(key);
    if (typed !== "") setTyped("");
  }
  if (key === null && lastSeenKey !== null) {
    setLastSeenKey(null);
  }

  const matches = typed.trim() === displayLabel;

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete access key?</AlertDialogTitle>
          <AlertDialogDescription>
            This is permanent — the key and all its bucket permissions will be removed. To confirm, type{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-foreground">
              {displayLabel}
            </code>{" "}
            below.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <Input
          autoFocus
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder={displayLabel}
          aria-label="Type key name to confirm"
          data-testid="delete-key-confirm-input"
        />
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            disabled={!matches || isDeleting}
            data-testid="delete-key-confirm-action"
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {isDeleting ? "Deleting…" : "Delete key"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
