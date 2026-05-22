import { useState } from "react";
import { createFileRoute, useNavigate, Link, useSearch } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  useUserRegions,
  useUserRegionBuckets,
  useCreateUserSync,
} from "@/shared/api/queries";

// NOTE: do NOT wrap in userPage() — parent layout (syncs.tsx) owns chrome.

type SyncSearch = {
  mode?: "pull" | "push";
  srcCid?: string;
  srcBid?: string;
};

// ADR-0002 (v1.1.0e + v1.1.0g): the picker reads from /user/regions
// instead of the retired /user/clusters tier. The backend now accepts
// EITHER a region ID or a Connection ID in the srcConnectionId /
// dstConnectionId fields — see internal/api/region_resolver.go. When
// the region has no admin Connection at its endpoint the backend
// returns 400 NO_ADMIN_BRIDGE; we render an inline error that points
// the user at /admin/clusters/new with the offending endpoint.
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
  const [srcRegionId, setSrcRegionId] = useState(search.srcCid ?? "");
  const [srcBucket, setSrcBucket] = useState(search.srcBid ?? "");
  const [srcPrefix, setSrcPrefix] = useState("");
  const [dstRegionId, setDstRegionId] = useState("");
  const [dstBucket, setDstBucket] = useState("");
  const [dstPrefix, setDstPrefix] = useState("");

  const { data: regions } = useUserRegions();
  const { data: srcBuckets } = useUserRegionBuckets(srcRegionId || null);
  const { data: dstBuckets } = useUserRegionBuckets(dstRegionId || null);
  const createSync = useCreateUserSync();

  const srcLocked = mode === "push" && !!search.srcCid && !!search.srcBid;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!srcRegionId || !dstRegionId || !srcBucket || !dstBucket) return;
    // The backend resolves region IDs in the connectionId fields to the
    // owning admin Connection (v1.1.0g). A 400 NO_ADMIN_BRIDGE response
    // is rendered inline below via the createSync.error block.
    createSync.mutate(
      {
        mode,
        srcConnectionId: srcRegionId,
        srcBucket,
        srcPrefix: srcPrefix || undefined,
        dstConnectionId: dstRegionId,
        dstBucket,
        dstPrefix: dstPrefix || undefined,
      },
      { onSuccess: () => navigate({ to: "/files/syncs" }) },
    );
  };

  const isSubmitDisabled = !srcRegionId || !dstRegionId || !srcBucket || !dstBucket || createSync.isPending;

  return (
    <div className="space-y-6 max-w-4xl">
      <Link to="/files/syncs" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        ← Back to syncs
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">New sync job</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Copy objects from a source bucket to a destination bucket. Crosses regions.
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
            <Label htmlFor="srcRegion">Region *</Label>
            <select
              id="srcRegion"
              value={srcRegionId}
              disabled={srcLocked}
              onChange={(e) => { setSrcRegionId(e.target.value); setSrcBucket(""); }}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
            >
              <option value="">— pick a region —</option>
              {regions?.map((r) => (
                <option key={r.id} value={r.id}>{r.alias || r.endpoint}</option>
              ))}
            </select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="srcBucket">Bucket *</Label>
            <select
              id="srcBucket"
              value={srcBucket}
              disabled={srcLocked || !srcRegionId}
              onChange={(e) => setSrcBucket(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
            >
              <option value="">— pick a bucket —</option>
              {srcBuckets?.buckets.map((b) => (<option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0, 12)}</option>))}
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
            <Label htmlFor="dstRegion">Region *</Label>
            <select
              id="dstRegion"
              value={dstRegionId}
              onChange={(e) => { setDstRegionId(e.target.value); setDstBucket(""); }}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm"
            >
              <option value="">— pick a region —</option>
              {regions?.map((r) => (
                <option key={r.id} value={r.id}>{r.alias || r.endpoint}</option>
              ))}
            </select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="dstBucket">Bucket *</Label>
            <select
              id="dstBucket"
              value={dstBucket}
              disabled={!dstRegionId}
              onChange={(e) => setDstBucket(e.target.value)}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
            >
              <option value="">— pick a bucket —</option>
              {dstBuckets?.buckets.map((b) => (<option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0, 12)}</option>))}
            </select>
          </div>
          <div className="grid gap-2 sm:col-span-2">
            <Label htmlFor="dstPrefix">Prefix (optional)</Label>
            <Input id="dstPrefix" value={dstPrefix} onChange={(e) => setDstPrefix(e.target.value)} placeholder="e.g. mirror/" />
          </div>
        </div>
      </section>

      {createSync.error && renderSyncError(createSync.error)}
    </div>
  );
}

// renderSyncError surfaces the NO_ADMIN_BRIDGE case with the offending
// endpoint and a hint to the cluster admin; other errors fall back to
// the standard inline message banner.
function renderSyncError(err: unknown) {
  const e = err as { code?: string; message?: string; details?: { endpoint?: string; field?: string } };
  if (e?.code === "NO_ADMIN_BRIDGE") {
    const endpoint = e.details?.endpoint ?? "(unknown)";
    const field = e.details?.field ?? "region";
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-3 text-sm text-destructive space-y-1">
        <p className="font-medium">No admin bridge for this region</p>
        <p>
          The {field === "srcConnectionId" ? "source" : field === "dstConnectionId" ? "destination" : ""} region at <code className="font-mono">{endpoint}</code> has no admin Connection registered. Ask a cluster admin to register this endpoint at <code className="font-mono">/admin/clusters/new</code>, then retry.
        </p>
      </div>
    );
  }
  return (
    <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {String(e?.message ?? "Failed to start sync")}
    </div>
  );
}
