import { useMemo, useState } from "react";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  useUserBackup,
  useUserBackupSnapshots,
  useUserRegions,
  useUserRegionBuckets,
  useRestoreUserBackup,
  type BackupSnapshotEntry,
  type RestoreResult,
} from "@/shared/api/queries";

// /files/backups/$id/restore — three-step restore wizard.
//
// Step semantics:
//   1. Pick snapshot     — radio: "latest" (default) or specific timestamp
//   2. Pick destination  — region + bucket dropdowns, default to backup's
//                          original source; optional sub-prefix
//   3. Confirm + run     — overwrite toggle + warning, then submit
//
// On submit the mutation hits POST /user/backups/{id}/restore
// synchronously; the response carries the per-object summary which
// we render inline as a result card. No background polling needed —
// the backend keeps the request open until the copy finishes.
//
// Deep-link: ?ts=YYYY-MM-DD_HH:MM:SS pre-fills the snapshot selector
// from the detail page's per-row Restore button.
export const Route = createFileRoute("/files/backups/$id/restore")({
  component: RestoreBackupPage,
  validateSearch: (search: Record<string, unknown>) => ({
    ts: typeof search.ts === "string" ? search.ts : undefined,
  }),
});

type Step = 1 | 2 | 3;

function RestoreBackupPage() {
  const { id } = Route.useParams();
  const { ts: tsFromQuery } = Route.useSearch();
  const navigate = useNavigate();

  const { data: backup, isLoading: backupLoading } = useUserBackup(id);
  const isSnapshot = backup?.mode === "snapshot";
  const { data: snapshots = [] } = useUserBackupSnapshots(id, !!backup && isSnapshot);
  const { data: regions = [] } = useUserRegions();
  const restoreMutation = useRestoreUserBackup();

  const [step, setStep] = useState<Step>(1);

  // Step 1 — snapshot selection. "latest" is the default; an explicit
  // timestamp may come from the ?ts deep-link.
  const [snapshotChoice, setSnapshotChoice] = useState<"latest" | "explicit">(
    tsFromQuery ? "explicit" : "latest",
  );
  const [explicitTimestamp, setExplicitTimestamp] = useState<string>(tsFromQuery ?? "");

  // Step 2 — destination. Defaults track the backup's original source
  // once `backup` resolves. Local state still works while loading: we
  // just won't be able to advance from step 1 until backup loads.
  const [dstRegionId, setDstRegionId] = useState<string>("");
  const [dstBucket, setDstBucket] = useState<string>("");
  const [dstPrefix, setDstPrefix] = useState<string>("");

  // Step 3 — copy semantics.
  const [overwriteExisting, setOverwriteExisting] = useState(false);

  // Result of the most recent run; cleared each time the operator
  // backs out of the result step to retry.
  const [result, setResult] = useState<RestoreResult | null>(null);

  // When `backup` becomes available + the dst fields are empty,
  // pre-fill them with the original source. We do this in a derived
  // memo so the operator can still override.
  const hasInitializedDst = dstRegionId !== "" || dstBucket !== "";
  if (!hasInitializedDst && backup) {
    // Pre-fill on first render after backup loads. Direct setState
    // during render is fine because we guard via hasInitializedDst.
    // (React batches the two updates.)
    setDstRegionId(backup.srcRegionId);
    setDstBucket(backup.srcBucket);
  }

  const { data: dstBuckets } = useUserRegionBuckets(dstRegionId || null);

  const sizeBytesForChoice = useMemo(() => {
    if (snapshotChoice === "latest") {
      // Snapshots come back newest-first from the API.
      return snapshots[0]?.bytes ?? 0;
    }
    const match = snapshots.find((s) => isoToSnapshotTimestamp(s.timestamp) === explicitTimestamp);
    return match?.bytes ?? 0;
  }, [snapshotChoice, explicitTimestamp, snapshots]);

  const objectsForChoice = useMemo(() => {
    if (snapshotChoice === "latest") return snapshots[0]?.objects ?? 0;
    const match = snapshots.find((s) => isoToSnapshotTimestamp(s.timestamp) === explicitTimestamp);
    return match?.objects ?? 0;
  }, [snapshotChoice, explicitTimestamp, snapshots]);

  const restoringInPlace =
    backup &&
    dstRegionId === backup.srcRegionId &&
    dstBucket === backup.srcBucket &&
    (dstPrefix === "" || dstPrefix === (backup.srcPrefix ?? ""));

  if (backupLoading || !backup) {
    return (
      <div className="space-y-6 max-w-3xl">
        <Link to="/files/backups" className="text-sm text-muted-foreground hover:underline">
          ← Back to backups
        </Link>
        <p className="text-sm text-muted-foreground">Loading backup…</p>
      </div>
    );
  }

  if (!isSnapshot) {
    return (
      <div className="space-y-6 max-w-3xl">
        <Link
          to="/files/backups/$id"
          params={{ id }}
          className="text-sm text-muted-foreground hover:underline"
        >
          ← Back to backup
        </Link>
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          Restore is only available for snapshot-mode backups. This backup is in mirror mode —
          its destination already holds the latest copy.
        </div>
      </div>
    );
  }

  const canAdvance = (s: Step): boolean => {
    if (s === 1) {
      if (snapshotChoice === "latest") return snapshots.length > 0;
      return !!explicitTimestamp;
    }
    if (s === 2) return !!dstRegionId && !!dstBucket;
    return true;
  };

  const handleSubmit = async () => {
    const body = {
      snapshotTimestamp: snapshotChoice === "latest" ? "latest" : explicitTimestamp,
      dstRegionId,
      dstBucket,
      dstPrefix: dstPrefix || undefined,
      overwriteExisting,
    };
    try {
      const res = await restoreMutation.mutateAsync({ id, body });
      setResult(res);
    } catch (e) {
      // mutation error renders below via restoreMutation.error.
      console.error("restore failed", e);
    }
  };

  // Result view — terminal step rendered in place of step 3.
  if (result) {
    return (
      <div className="space-y-6 max-w-3xl">
        <Link
          to="/files/backups/$id"
          params={{ id }}
          className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
        >
          ← Back to backup
        </Link>
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Restore complete</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Snapshot <span className="font-mono">{result.resolvedSnapshot}</span> copied to{" "}
            <span className="font-mono">{dstBucket}</span>.
          </p>
        </header>
        <section className="rounded-lg border bg-card p-6 space-y-3">
          <div
            className={`text-sm font-medium ${
              result.success ? "text-green-600" : "text-red-600"
            }`}
          >
            {result.success ? "Success" : "Completed with errors"}
          </div>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">Objects copied</dt>
            <dd className="font-mono">{result.objectsCopied}</dd>
            <dt className="text-muted-foreground">Objects skipped (already existed)</dt>
            <dd className="font-mono">{result.objectsSkipped}</dd>
            <dt className="text-muted-foreground">Bytes copied</dt>
            <dd className="font-mono">{formatBytes(result.bytesCopied)}</dd>
            <dt className="text-muted-foreground">Started</dt>
            <dd>{new Date(result.startedAt).toLocaleString()}</dd>
            <dt className="text-muted-foreground">Completed</dt>
            <dd>{new Date(result.completedAt).toLocaleString()}</dd>
          </dl>
          {result.errors && result.errors.length > 0 && (
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm">
              <div className="font-medium text-destructive mb-1">
                {result.errors.length} error{result.errors.length === 1 ? "" : "s"}
              </div>
              <ul className="list-disc list-inside text-xs text-destructive/90 space-y-0.5">
                {result.errors.slice(0, 10).map((e, i) => (
                  <li key={i} className="font-mono">{e}</li>
                ))}
              </ul>
            </div>
          )}
        </section>
        <div className="flex items-center gap-2">
          <Button onClick={() => navigate({ to: "/files/backups/$id", params: { id } })}>
            Back to backup
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              setResult(null);
              setStep(1);
            }}
          >
            Run another restore
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6 max-w-3xl">
      <Link
        to="/files/backups/$id"
        params={{ id }}
        className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
      >
        ← Back to backup
      </Link>

      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Restore from snapshot</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Step {step} of 3 — {STEP_TITLES[step - 1]}
        </p>
      </header>

      {/* Progress bar */}
      <div className="flex items-center gap-2">
        {[1, 2, 3].map((n) => (
          <div
            key={n}
            className={`h-1.5 flex-1 rounded-full ${n <= step ? "bg-primary" : "bg-muted"}`}
          />
        ))}
      </div>

      {step === 1 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Pick a snapshot</h2>
          {snapshots.length === 0 ? (
            <p className="text-sm text-destructive">
              No snapshots on disk yet — run the backup at least once before restoring.
            </p>
          ) : (
            <>
              <label className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${snapshotChoice === "latest" ? "border-ring" : ""}`}>
                <input
                  type="radio"
                  name="snapshotChoice"
                  value="latest"
                  checked={snapshotChoice === "latest"}
                  onChange={() => setSnapshotChoice("latest")}
                  className="mt-1"
                />
                <div>
                  <div className="font-medium text-sm">Latest snapshot</div>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    Resolved at request time. Currently:{" "}
                    <span className="font-mono">
                      {snapshots[0] ? new Date(snapshots[0].timestamp).toLocaleString() : "(none)"}
                    </span>
                    {" "}— {snapshots[0]?.objects ?? 0} objects, {formatBytes(snapshots[0]?.bytes ?? 0)}.
                  </p>
                </div>
              </label>
              <label className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${snapshotChoice === "explicit" ? "border-ring" : ""}`}>
                <input
                  type="radio"
                  name="snapshotChoice"
                  value="explicit"
                  checked={snapshotChoice === "explicit"}
                  onChange={() => setSnapshotChoice("explicit")}
                  className="mt-1"
                />
                <div className="flex-1">
                  <div className="font-medium text-sm">Specific snapshot</div>
                  <p className="text-xs text-muted-foreground mt-0.5 mb-2">
                    Pick one from the {snapshots.length} most recent snapshot{snapshots.length === 1 ? "" : "s"}.
                  </p>
                  <select
                    value={explicitTimestamp}
                    onChange={(e) => {
                      setExplicitTimestamp(e.target.value);
                      setSnapshotChoice("explicit");
                    }}
                    disabled={snapshotChoice !== "explicit"}
                    className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
                  >
                    <option value="">— pick a snapshot —</option>
                    {snapshots.map((s) => (
                      <SnapshotOption key={s.prefix} s={s} />
                    ))}
                  </select>
                </div>
              </label>
            </>
          )}
        </section>
      )}

      {step === 2 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Pick a destination</h2>
          <p className="text-xs text-muted-foreground">
            Defaults to the backup's original source ({backup.srcBucket}). Change it if you'd
            rather restore into a different region or bucket — useful for "spin up a copy and
            poke around" workflows.
          </p>
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
                {regions.map((r) => (
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
                {dstBuckets?.buckets.map((b) => (
                  <option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0, 12)}</option>
                ))}
              </select>
            </div>
            <div className="grid gap-2 sm:col-span-2">
              <Label htmlFor="dstPrefix">Prefix (optional)</Label>
              <Input
                id="dstPrefix"
                value={dstPrefix}
                onChange={(e) => setDstPrefix(e.target.value)}
                placeholder="e.g. restored/"
              />
              <p className="text-xs text-muted-foreground">
                Empty restores into the bucket root. Use a prefix to keep restored files
                separate from existing data.
              </p>
            </div>
          </div>
          <div className="rounded-md border bg-muted/20 p-4 text-xs space-y-1">
            <div className="text-muted-foreground">Estimated size</div>
            <div className="font-mono">
              {objectsForChoice} objects, {formatBytes(sizeBytesForChoice)}
            </div>
          </div>
        </section>
      )}

      {step === 3 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Confirm &amp; run</h2>

          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">Snapshot</dt>
            <dd className="font-mono">
              {snapshotChoice === "latest" ? "latest" : explicitTimestamp}
            </dd>
            <dt className="text-muted-foreground">Destination region</dt>
            <dd className="font-mono">
              {regions.find((r) => r.id === dstRegionId)?.alias || dstRegionId}
            </dd>
            <dt className="text-muted-foreground">Destination bucket</dt>
            <dd className="font-mono">
              {dstBucket}{dstPrefix ? `/${dstPrefix.replace(/^\/+/, "")}` : ""}
            </dd>
            <dt className="text-muted-foreground">Estimated</dt>
            <dd className="font-mono">
              {objectsForChoice} objects, {formatBytes(sizeBytesForChoice)}
            </dd>
          </dl>

          <label className="flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30">
            <Checkbox
              checked={overwriteExisting}
              onCheckedChange={(v) => setOverwriteExisting(Boolean(v))}
              className="mt-0.5"
            />
            <div>
              <div className="font-medium text-sm">Overwrite existing files</div>
              <p className="text-xs text-muted-foreground mt-0.5">
                When off (recommended for the post-disaster "fill in the gaps" workflow), files
                whose keys already exist at the destination are skipped. Turn on to replace every
                object in place.
              </p>
            </div>
          </label>

          {overwriteExisting && restoringInPlace && (
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
              <div className="font-medium">This will REPLACE the current contents of {dstBucket}.</div>
              <p className="mt-1 text-xs">
                Existing files with matching keys will be overwritten with the snapshot's
                versions. There is no undo.
              </p>
            </div>
          )}

          {restoreMutation.error && (
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {String((restoreMutation.error as Error).message || "Failed to start restore")}
            </div>
          )}
        </section>
      )}

      <div className="flex items-center justify-between gap-2">
        <Button
          variant="outline"
          onClick={() => navigate({ to: "/files/backups/$id", params: { id } })}
        >
          Cancel
        </Button>
        <div className="flex items-center gap-2">
          {step > 1 && (
            <Button variant="ghost" onClick={() => setStep((s) => (s - 1) as Step)}>
              Back
            </Button>
          )}
          {step < 3 && (
            <Button
              onClick={() => setStep((s) => (s + 1) as Step)}
              disabled={!canAdvance(step)}
            >
              Next
            </Button>
          )}
          {step === 3 && (
            <Button
              onClick={handleSubmit}
              disabled={restoreMutation.isPending}
            >
              {restoreMutation.isPending ? "Restoring…" : "Restore"}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

const STEP_TITLES = ["Pick snapshot", "Pick destination", "Confirm & run"];

function SnapshotOption({ s }: { s: BackupSnapshotEntry }) {
  const ts = isoToSnapshotTimestamp(s.timestamp);
  const when = new Date(s.timestamp).toLocaleString();
  return (
    <option value={ts}>
      {when} — {s.objects} objects, {formatBytes(s.bytes)}
    </option>
  );
}

// isoToSnapshotTimestamp converts an ISO-8601 timestamp into the
// on-disk layout the backend expects (YYYY-MM-DD_HH:MM:SS in UTC).
function isoToSnapshotTimestamp(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}_${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
}

function formatBytes(n: number): string {
  if (!n) return "0";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}
