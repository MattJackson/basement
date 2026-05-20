import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCreateUserSync } from "@/shared/api/queries";

interface StartSyncDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  direction?: "pull" | "push";
  defaultSrcConnectionId?: string;
  defaultSrcBucket?: string;
}

export function StartSyncDialog({ 
  open, 
  onOpenChange, 
  direction = "pull",
  defaultSrcConnectionId,
  defaultSrcBucket,
}: StartSyncDialogProps) {
  const [srcConnectionId, setSrcConnectionId] = useState("");
  const [srcBucket, setSrcBucket] = useState("");
  const [srcPrefix, setSrcPrefix] = useState("");
  const [dstConnectionId, setDstConnectionId] = useState("");
  const [dstBucket, setDstBucket] = useState("");
  const [dstPrefix, setDstPrefix] = useState("");

  const createMutation = useCreateUserSync();

  useEffect(() => {
    if (open && direction === "push") {
      if (defaultSrcConnectionId) setSrcConnectionId(defaultSrcConnectionId);
      if (defaultSrcBucket) setSrcBucket(defaultSrcBucket);
    } else if (direction === "pull") {
      setDstConnectionId("");
      setDstBucket("");
    }
  }, [open, direction, defaultSrcConnectionId, defaultSrcBucket]);

  useEffect(() => {
    return () => {
      setSrcConnectionId("");
      setSrcBucket("");
      setSrcPrefix("");
      setDstConnectionId("");
      setDstBucket("");
      setDstPrefix("");
    };
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!srcConnectionId || !dstConnectionId || !srcBucket || !dstBucket) {
      return;
    }

    await createMutation.mutateAsync({
      mode: direction as "pull" | "push",
      srcConnectionId,
      srcBucket,
      srcPrefix: srcPrefix || undefined,
      dstConnectionId,
      dstBucket,
      dstPrefix: dstPrefix || undefined,
    });

    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={(open) => {
      if (!open) {
        setSrcConnectionId("");
        setDstConnectionId("");
        setSrcBucket("");
        setDstBucket("");
        setSrcPrefix("");
        setDstPrefix("");
      }
      onOpenChange(open);
    }}>
      <DialogContent className="sm:max-w-[525px]">
        <DialogHeader>
          <DialogTitle>{direction === "push" ? "Sync out…" : "Sync in…"} </DialogTitle>
          <DialogDescription>
            {direction === "push" 
              ? "Copy objects from this bucket to a bucket on another cluster."
              : "Copy objects from a bucket on another cluster to this bucket."}
          </DialogDescription>
        </DialogHeader>

         <form id="sync-form" onSubmit={handleSubmit} className="space-y-4">
            <div className="grid gap-4 md:grid-cols-2">
              {direction === "push" ? (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="src-cluster">Source cluster</Label>
                    <select
                      id="src-cluster"
                      value={srcConnectionId}
                      disabled
                      className="flex h-10 w-full rounded-md border border-input bg-muted px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 opacity-75"
                    >
                      <option value={srcConnectionId}>Current cluster</option>
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="src-bucket">Source bucket</Label>
                    <select
                      id="src-bucket"
                      value={srcBucket}
                      disabled
                      className="flex h-10 w-full rounded-md border border-input bg-muted px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 opacity-75"
                    >
                      <option value={srcBucket}>{defaultSrcBucket || "Current bucket"}</option>
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="src-prefix">Source prefix (optional)</Label>
                    <Input
                      id="src-prefix"
                      placeholder="e.g., folder/subfolder/"
                      value={srcPrefix}
                      onChange={(e) => setSrcPrefix(e.target.value)}
                    />
                  </div>
                </>
              ) : (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="src-cluster">Source cluster</Label>
                    <select
                      id="src-cluster"
                      value={srcConnectionId}
                      onChange={(e) => setSrcConnectionId(e.target.value)}
                      className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      <option value="">Select source cluster...</option>
                      {["cluster-1", "cluster-2"].map((id) => (
                        <option key={id} value={id}>{`Cluster ${id}`}</option>
                      ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="src-bucket">Source bucket</Label>
                    <select
                      id="src-bucket"
                      value={srcBucket}
                      onChange={(e) => setSrcBucket(e.target.value)}
                      disabled={!srcConnectionId}
                      className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50"
                    >
                      <option value="">Select source bucket...</option>
                      {["bucket-a", "bucket-b"].map((id) => (
                        <option key={id} value={id}>{`Bucket ${id}`}</option>
                      ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="src-prefix">Source prefix (optional)</Label>
                    <Input
                      id="src-prefix"
                      placeholder="e.g., folder/subfolder/"
                      value={srcPrefix}
                      onChange={(e) => setSrcPrefix(e.target.value)}
                    />
                  </div>
                </>
              )}

              {direction === "push" ? (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="dst-cluster">Destination cluster</Label>
                    <select
                      id="dst-cluster"
                      value={dstConnectionId}
                      onChange={(e) => setDstConnectionId(e.target.value)}
                      className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      <option value="">Select destination cluster...</option>
                      {["cluster-1", "cluster-2"].map((id) => (
                        <option key={id} value={id}>{`Cluster ${id}`}</option>
                      ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="dst-bucket">Destination bucket</Label>
                    <select
                      id="dst-bucket"
                      value={dstBucket}
                      onChange={(e) => setDstBucket(e.target.value)}
                      disabled={!dstConnectionId}
                      className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50"
                    >
                      <option value="">Select destination bucket...</option>
                      {["bucket-x", "bucket-y"].map((id) => (
                        <option key={id} value={id}>{`Bucket ${id}`}</option>
                      ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="dst-prefix">Destination prefix (optional)</Label>
                    <Input
                      id="dst-prefix"
                      placeholder="e.g., mirror-folder/"
                      value={dstPrefix}
                      onChange={(e) => setDstPrefix(e.target.value)}
                    />
                  </div>
                </>
              ) : (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="dst-cluster">Destination cluster</Label>
                    <select
                      id="dst-cluster"
                      value={dstConnectionId}
                      onChange={(e) => setDstConnectionId(e.target.value)}
                      className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    >
                      <option value="">Select destination cluster...</option>
                      {["cluster-1", "cluster-2"].map((id) => (
                        <option key={id} value={id}>{`Cluster ${id}`}</option>
                      ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="dst-bucket">Destination bucket</Label>
                    <select
                      id="dst-bucket"
                      value={dstBucket}
                      onChange={(e) => setDstBucket(e.target.value)}
                      disabled={!dstConnectionId}
                      className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-50"
                    >
                      <option value="">Select destination bucket...</option>
                      {["bucket-x", "bucket-y"].map((id) => (
                        <option key={id} value={id}>{`Bucket ${id}`}</option>
                      ))}
                    </select>
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="dst-prefix">Destination prefix (optional)</Label>
                    <Input
                      id="dst-prefix"
                      placeholder="e.g., mirror-folder/"
                      value={dstPrefix}
                      onChange={(e) => setDstPrefix(e.target.value)}
                    />
                  </div>
                </>
              )}
            </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button 
              type="submit" 
              form="sync-form"
              disabled={createMutation.isPending || !srcConnectionId || !dstConnectionId || !srcBucket || !dstBucket}
            >
              {createMutation.isPending ? "Creating..." : direction === "push" ? "Sync out…" : "Start sync"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
