import { useState } from "react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

export interface AttachKeyToBucketDialogProps {
  open: boolean;
  /** Keys in the cluster, minus any already attached to this bucket. */
  candidates: { keyId: string; label: string }[];
  isSaving?: boolean;
  errorMessage?: string | null;
  onSubmit: (input: { keyId: string; read: boolean; write: boolean; owner: boolean }) => void;
  onCancel: () => void;
}

/**
 * Attach an existing access key to this bucket with a permission set.
 *
 * Mirror of GrantBucketAccessDialog from the bucket-detail side. The
 * symmetry matters — Garage's bucket↔key model is an undirected edge,
 * so both screens write the same /bucket/allow + /bucket/deny pair via
 * UpdateKeyPermissions.
 *
 * Two-input rule (per [[feedback-popups-max-2-fields]]):
 *   1) key picker
 *   2) R/W/O checkbox group
 */
export function AttachKeyToBucketDialog({
  open,
  candidates,
  isSaving,
  errorMessage,
  onSubmit,
  onCancel,
}: AttachKeyToBucketDialogProps) {
  const [keyId, setKeyId] = useState("");
  const [read, setRead] = useState(true);
  const [write, setWrite] = useState(false);
  const [owner, setOwner] = useState(false);

  const [openTrack, setOpenTrack] = useState(false);
  if (open && !openTrack) {
    setOpenTrack(true);
    setKeyId(candidates[0]?.keyId ?? "");
    setRead(true);
    setWrite(false);
    setOwner(false);
  } else if (!open && openTrack) {
    setOpenTrack(false);
  }

  const handleOwnerToggle = () => {
    const next = !owner;
    setOwner(next);
    if (next) {
      setRead(false);
      setWrite(false);
    }
  };

  const handleReadOrWriteToggle = (field: "read" | "write") => {
    if (owner) setOwner(false);
    if (field === "read") setRead(!read);
    else setWrite(!write);
  };

  const canSubmit = keyId !== "" && (read || write || owner) && !isSaving;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    onSubmit({ keyId, read, write, owner });
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Attach key to bucket</DialogTitle>
          <DialogDescription>
            Pick an existing access key from this cluster and choose what it can
            do on this bucket.
          </DialogDescription>
        </DialogHeader>

        {candidates.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4">
            Every key in this cluster is already attached to this bucket, or the
            cluster has no keys yet.
          </p>
        ) : (
          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <div className="grid gap-2">
              <label htmlFor="attach-key" className="text-sm font-medium">
                Key
              </label>
              <select
                id="attach-key"
                value={keyId}
                onChange={(e) => setKeyId(e.target.value)}
                disabled={isSaving}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              >
                {candidates.map((c) => (
                  <option key={c.keyId} value={c.keyId}>
                    {c.label}
                  </option>
                ))}
              </select>
            </div>

            <div className="grid gap-2">
              <span className="text-sm font-medium">Permissions</span>
              <div className="flex flex-col gap-2 rounded-md border bg-card/50 px-3 py-2.5">
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <Checkbox
                    checked={read}
                    onCheckedChange={() => handleReadOrWriteToggle("read")}
                    disabled={isSaving}
                  />
                  <span><strong>Read</strong> — list and download objects</span>
                </label>
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <Checkbox
                    checked={write}
                    onCheckedChange={() => handleReadOrWriteToggle("write")}
                    disabled={isSaving}
                  />
                  <span><strong>Write</strong> — upload, replace, and delete objects</span>
                </label>
                <label className="flex items-center gap-2 text-sm cursor-pointer">
                  <Checkbox
                    checked={owner}
                    onCheckedChange={handleOwnerToggle}
                    disabled={isSaving}
                  />
                  <span><strong>Owner</strong> — full control over the bucket itself</span>
                </label>
              </div>
            </div>

            {errorMessage && (
              <p className="text-sm text-destructive">{errorMessage}</p>
            )}

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                onClick={onCancel}
                disabled={isSaving}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={!canSubmit}>
                {isSaving ? "Attaching…" : "Attach key"}
              </Button>
            </DialogFooter>
          </form>
        )}

        {candidates.length === 0 && (
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onCancel}>
              Close
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  );
}
