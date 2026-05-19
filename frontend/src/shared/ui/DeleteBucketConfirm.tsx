import { useEffect, useState } from "react";
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

export interface DeleteBucketConfirmProps {
  open: boolean;
  bucketAlias: string;
  isDeleting?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * Type-name-to-confirm guard for bucket deletion.
 *
 * Deletion is irrevocable at the storage layer; the only way to "undo"
 * is a backup. We therefore force the user to type the bucket alias
 * exactly before the Delete action is enabled — matches GitHub /
 * Vercel / DigitalOcean conventions for high-blast-radius destructive
 * ops.
 *
 * Reset behavior: the typed input resets whenever the dialog is
 * (re-)opened. Mismatch is not surfaced as an error — Delete just
 * stays disabled.
 */
export function DeleteBucketConfirm({
  open,
  bucketAlias,
  isDeleting,
  onConfirm,
  onCancel,
}: DeleteBucketConfirmProps) {
  const [typed, setTyped] = useState("");

  useEffect(() => {
    if (open) setTyped("");
  }, [open, bucketAlias]);

  const matches = typed.trim() === bucketAlias;

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete bucket?</AlertDialogTitle>
          <AlertDialogDescription>
            This is permanent — the bucket and every object inside it
            will be removed. To confirm, type{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-foreground">
              {bucketAlias}
            </code>{" "}
            below.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <Input
          autoFocus
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          placeholder={bucketAlias}
          aria-label="Type bucket name to confirm"
          data-testid="delete-bucket-confirm-input"
        />
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            disabled={!matches || isDeleting}
            data-testid="delete-bucket-confirm-action"
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {isDeleting ? "Deleting…" : "Delete bucket"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
