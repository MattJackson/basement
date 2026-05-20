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

export interface RevokeAccessConfirmProps {
  open: boolean;
  /** What's being revoked from what — surface both sides so the operator
   * can read the dialog and immediately know which edge is going away. */
  subject: string;
  target: string;
  isRevoking?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

/**
 * Confirmation dialog for revoking a single key↔bucket grant.
 *
 * Used from both:
 *   - key detail "Bucket access" rows (subject = key, target = bucket)
 *   - bucket detail "Attached keys" rows (subject = bucket, target = key)
 *
 * The action is non-destructive at the storage layer (the key keeps
 * existing, the bucket keeps existing — only the edge is dropped), so
 * this is a single-button "Are you sure?" rather than the type-the-name
 * pattern used for irreversible deletes.
 */
export function RevokeAccessConfirm({
  open,
  subject,
  target,
  isRevoking,
  onConfirm,
  onCancel,
}: RevokeAccessConfirmProps) {
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Revoke access?</AlertDialogTitle>
          <AlertDialogDescription>
            <span className="font-medium text-foreground">{subject}</span> will
            no longer have access to{" "}
            <span className="font-medium text-foreground">{target}</span>. The
            grant can be re-added at any time.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            disabled={isRevoking}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {isRevoking ? "Revoking…" : "Revoke access"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
