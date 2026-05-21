import { useState } from "react";
import { createFileRoute, useNavigate, Link, useSearch } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { useUserClusters, useUserClusterBuckets, useCreateUserSync } from "@/shared/api/queries";

// NOTE: do NOT wrap in userPage() — parent layout (syncs.tsx) owns chrome.

type SyncSearch = {
  mode?: "pull" | "push";
  srcCid?: string;
  srcBid?: string;
};

// Popups-max-2-fields: StartSyncDialog had 6 fields (src cluster +
// bucket + prefix, dst cluster + bucket + prefix) AND its cluster /
// bucket dropdowns were HARDCODED to fake values ('cluster-1',
// 'bucket-a' …) — the entire sync feature was non-functional. This
// page wires up the real query hooks (useUserClusters /
// useUserClusterBuckets per side) and gives the form room to breathe.
//
// Default search params let bucket-browser 'Sync in / Sync out'
// buttons pre-fill the right side: ?mode=push&srcCid=…&srcBid=…
//
// TODO(v1.1.0e): migrate sync UI to UserRegion IDs once the sync
// engine learns to operate region-to-region. v1.1.0c kept this form
// on cluster-tier hooks deliberately — the backend's sync engine
// still expects srcConnectionId/dstConnectionId (cluster Connection
// records), and rewiring both at once was out of scope. The bucket
// browser at /files/$regionId/b/$bid drops its Sync in/Sync out
// buttons for the same reason — no region->cluster bridge yet.
export const Route = createFileRoute("/files/syncs/new")({
  component: NewSyncPage,
  validateSearch: (search): SyncSearch => ({
    mode: search.mode === "push" ? "push" : "pull",
    srcCid: typeof search.srcCid === "string" ? search.srcCid : undefined,
    srcBid: typeof search.srcBid === "string" ? search.srcBid : undefined,
  }),
});

function NewSyncPage() {
  const navigate = useNavigate();
  const search = useSearch({ from: "/files/syncs/new" }) as SyncSearch;

  const mode = search.mode ?? "pull";
  const [srcConnectionId, setSrcConnectionId] = useState(search.srcCid ?? "");
  const [srcBucket, setSrcBucket] = useState(search.srcBid ?? "");
  const [srcPrefix, setSrcPrefix] = useState("");
  const [dstConnectionId, setDstConnectionId] = useState("");
  const [dstBucket, setDstBucket] = useState("");
  const [dstPrefix, setDstPrefix] = useState("");

  const { data: clusters } = useUserClusters();
  const { data: srcBuckets } = useUserClusterBuckets(srcConnectionId);
  const { data: dstBuckets } = useUserClusterBuckets(dstConnectionId);
  const createSync = useCreateUserSync();

  const srcLocked = mode === "push" && !!search.srcCid && !!search.srcBid;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!srcConnectionId || !dstConnectionId || !srcBucket || !dstBucket) return;
    createSync.mutate(
      {
        mode,
        srcConnectionId,
        srcBucket,
        srcPrefix: srcPrefix || undefined,
        dstConnectionId,
        dstBucket,
        dstPrefix: dstPrefix || undefined,
      },
      { onSuccess: () => navigate({ to: "/files/syncs" }) },
    );
  };

  const isSubmitDisabled = !srcConnectionId || !dstConnectionId || !srcBucket || !dstBucket || createSync.isPending;

  return (
    <div className="space-y-6 max-w-4xl">
      <Link to="/files/syncs" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        ← Back to syncs
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">New sync job</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Copy objects from a source bucket to a destination bucket. Crosses clusters.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => navigate({ to: "/files/syncs" })} disabled={createSync.isPending}>Cancel</Button>
          <Button onClick={(e) => handleSubmit(e as any)} disabled={isSubmitDisabled}>
            {createSync.isPending ? "Creating…" : "Start sync"}
          </Button>
        </div>
      </header>

      <section className="space-y-4 rounded-lg border bg-card p-6">
        <h2 className="text-sm font-medium text-muted-foreground">Mode</h2>
        <RadioGroup
          value={mode}
          onValueChange={(v) => navigate({ to: "/files/syncs/new", search: { ...search, mode: v as "pull" | "push" } })}
          className="grid gap-2 sm:grid-cols-2"
        >
          <label className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${mode === "pull" ? "border-ring" : ""}`}>
            <RadioGroupItem value="pull" className="mt-1" />
            <div>
              <div className="font-medium text-sm">Pull (copy in)</div>
              <p className="text-xs text-muted-foreground mt-0.5">Copy from a source bucket into a bucket you own.</p>
            </div>
          </label>
          <label className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${mode === "push" ? "border-ring" : ""}`}>
            <RadioGroupItem value="push" className="mt-1" />
            <div>
              <div className="font-medium text-sm">Push (copy out)</div>
              <p className="text-xs text-muted-foreground mt-0.5">Copy from a bucket you own to a destination bucket.</p>
            </div>
          </label>
        </RadioGroup>
      </section>

      <section className="space-y-4 rounded-lg border bg-card p-6">
        <h2 className="text-sm font-medium text-muted-foreground">Source</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid gap-2">
            <Label htmlFor="srcCluster">Cluster *</Label>
            <select
              id="srcCluster"
              value={srcConnectionId}
              disabled={srcLocked}
              onChange={(e) => { setSrcConnectionId(e.target.value); setSrcBucket(""); }}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
            >
              <option value="">— pick a cluster —</option>
              {clusters?.map((c) => (<option key={c.id} value={c.id}>{c.label}</option>))}
            </select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="srcBucket">Bucket *</Label>
            <select
              id="srcBucket"
              value={srcBucket}
              disabled={srcLocked || !srcConnectionId}
              onChange={(e) => setSrcBucket(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
            >
              <option value="">— pick a bucket —</option>
              {srcBuckets?.map((b) => (<option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0, 12)}</option>))}
            </select>
          </div>
          <div className="grid gap-2 sm:col-span-2">
            <Label htmlFor="srcPrefix">Prefix (optional)</Label>
            <Input id="srcPrefix" value={srcPrefix} onChange={(e) => setSrcPrefix(e.target.value)} placeholder="e.g. backups/" />
          </div>
        </div>
      </section>

      <section className="space-y-4 rounded-lg border bg-card p-6">
        <h2 className="text-sm font-medium text-muted-foreground">Destination</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid gap-2">
            <Label htmlFor="dstCluster">Cluster *</Label>
            <select
              id="dstCluster"
              value={dstConnectionId}
              onChange={(e) => { setDstConnectionId(e.target.value); setDstBucket(""); }}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm"
            >
              <option value="">— pick a cluster —</option>
              {clusters?.map((c) => (<option key={c.id} value={c.id}>{c.label}</option>))}
            </select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="dstBucket">Bucket *</Label>
            <select
              id="dstBucket"
              value={dstBucket}
              disabled={!dstConnectionId}
              onChange={(e) => setDstBucket(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
            >
              <option value="">— pick a bucket —</option>
              {dstBuckets?.map((b) => (<option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0, 12)}</option>))}
            </select>
          </div>
          <div className="grid gap-2 sm:col-span-2">
            <Label htmlFor="dstPrefix">Prefix (optional)</Label>
            <Input id="dstPrefix" value={dstPrefix} onChange={(e) => setDstPrefix(e.target.value)} placeholder="e.g. mirror/" />
          </div>
        </div>
      </section>

      {createSync.error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {String(createSync.error.message ?? "Failed to start sync")}
        </div>
      )}
    </div>
  );
}
