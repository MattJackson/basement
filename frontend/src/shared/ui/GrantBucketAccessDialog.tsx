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

export interface GrantBucketAccessDialogProps {
  open: boolean;
  /** Buckets visible in the cluster, minus any the key already has access to. */
  candidates: { bucketId: string; label: string }[];
  isSaving?: boolean;
  errorMessage?: string | null;
  onSubmit: (input: { bucketId: string; read: boolean; write: boolean; owner: boolean }) => void;
  onCancel: () => void;
}

/**
 * Grant a key access to one bucket with a permission set.
 *
 * Two-input rule (per [[feedback-popups-max-2-fields]]):
 *   1) bucket picker (native <select> — small dataset, no need for combobox)
 *   2) R/W/O checkbox group
 *
 * Owner is mutually exclusive with read/write per Garage's per-edge model:
 * checking Owner clears read/write, and vice versa. Submit is disabled
 * until a bucket is picked AND at least one permission is set.
 */
export function GrantBucketAccessDialog({
  open,
  candidates,
  isSaving,
  errorMessage,
  onSubmit,
  onCancel,
}: GrantBucketAccessDialogProps) {
  const [bucketId, setBucketId] = useState("");
  const [read, setRead] = useState(true);
  const [write, setWrite] = useState(false);
  const [owner, setOwner] = useState(false);

  // Reset form when re-opened.
  const [openTrack, setOpenTrack] = useState(false);
  if (open && !openTrack) {
    setOpenTrack(true);
    setBucketId(candidates[0]?.bucketId ?? "");
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
      // Owner implies neither read nor write — Garage treats Owner as a
      // separate edge flag, not a superset. Clearing keeps the wire call
      // honest.
      setRead(false);
      setWrite(false);
    }
  };

  const handleReadOrWriteToggle = (field: "read" | "write") => {
    if (owner) setOwner(false);
    if (field === "read") setRead(!read);
    else setWrite(!write);
  };

  const canSubmit =
    bucketId !== "" && (read || write || owner) && !isSaving;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    onSubmit({ bucketId, read, write, owner });
  };

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Grant bucket access</DialogTitle>
          <DialogDescription>
            Pick a bucket from this cluster and choose what this key can do on it.
          </DialogDescription>
        </DialogHeader>

        {candidates.length === 0 ? (
          <p className="text-sm text-muted-foreground py-4">
            This key already has access to every bucket in the cluster, or the
            cluster has no buckets yet.
          </p>
        ) : (
          <form onSubmit={handleSubmit} className="flex flex-col gap-4">
            <div className="grid gap-2">
              <label htmlFor="grant-bucket" className="text-sm font-medium">
                Bucket
              </label>
              <select
                id="grant-bucket"
                value={bucketId}
                onChange={(e) => setBucketId(e.target.value)}
                disabled={isSaving}
                className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
              >
                {candidates.map((c) => (
                  <option key={c.bucketId} value={c.bucketId}>
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
                {isSaving ? "Granting…" : "Grant access"}
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
