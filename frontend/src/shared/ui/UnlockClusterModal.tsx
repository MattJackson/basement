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
import { useUnlockCluster } from "@/shared/api/queries";

export interface UnlockClusterModalProps {
  open: boolean;
  cid: string;
  clusterLabel: string;
  /**
   * Called after a successful unlock. The caller is responsible for
   * retrying any 423-LOCKED request that triggered the modal.
   */
  onUnlocked: () => void;
  onCancel: () => void;
}

/**
 * UnlockClusterModal — per-cluster envelope encryption unlock prompt
 * (ADR-0007 / v1.12.0a).
 *
 * Two-field UI (password + Unlock button), aligns with the
 * "popups max 2 fields" doctrine from the operator's playbook. Password
 * input is type=password, no autocomplete, no value persisted beyond
 * the modal's lifetime.
 *
 * Error handling: a wrong-password 401 INVALID_PASSWORD is surfaced
 * inline (red helper text) rather than dismissing the modal — the
 * operator may try again without re-opening. Other failures (503
 * CLUSTER_SECRETS_NOT_WIRED, 404 NO_CSK_ADMIN) close the modal and
 * surface as toasts (caller handles the onCancel branch).
 */
export function UnlockClusterModal({
  open,
  cid,
  clusterLabel,
  onUnlocked,
  onCancel,
}: UnlockClusterModalProps) {
  const [password, setPassword] = useState("");
  const [inlineErr, setInlineErr] = useState<string | null>(null);
  const unlock = useUnlockCluster(cid);

  // Reset state on open/close.
  const [lastSeenOpen, setLastSeenOpen] = useState(false);
  if (open && !lastSeenOpen) {
    setLastSeenOpen(true);
    if (password !== "") setPassword("");
    if (inlineErr !== null) setInlineErr(null);
  }
  if (!open && lastSeenOpen) {
    setLastSeenOpen(false);
  }

  const submit = () => {
    if (!password) return;
    setInlineErr(null);
    unlock.mutate(
      { password },
      {
        onSuccess: () => {
          setPassword("");
          onUnlocked();
        },
        onError: (err) => {
          const code = (err as Error & { code?: string }).code ?? "";
          if (code === "INVALID_PASSWORD") {
            setInlineErr("Wrong password. Try again.");
            return;
          }
          // Bubble structural errors up to the caller for toast
          // surfacing — keep this modal focused on the password retry
          // path.
          setInlineErr(err.message || "Unlock failed");
        },
      },
    );
  };

  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onCancel();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Unlock cluster</AlertDialogTitle>
          <AlertDialogDescription>
            Cluster{" "}
            <code className="rounded bg-muted px-1 py-0.5 font-mono text-foreground">
              {clusterLabel}
            </code>{" "}
            is locked. Enter your cluster admin password to unlock its stored secrets
            for this session.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="space-y-2">
          <Input
            autoFocus
            type="password"
            autoComplete="off"
            value={password}
            onChange={(e) => {
              setPassword(e.target.value);
              if (inlineErr) setInlineErr(null);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                submit();
              }
            }}
            placeholder="Cluster admin password"
            aria-label="Cluster admin password"
            data-testid="unlock-cluster-password-input"
          />
          {inlineErr ? (
            <p className="text-sm text-destructive" data-testid="unlock-cluster-inline-error">
              {inlineErr}
            </p>
          ) : null}
        </div>
        <AlertDialogFooter>
          <AlertDialogCancel onClick={onCancel}>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={submit}
            disabled={!password || unlock.isPending}
            data-testid="unlock-cluster-action"
          >
            {unlock.isPending ? "Unlocking…" : "Unlock"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
