import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  useUserBackup,
  useRunUserBackup,
  useDeleteUserBackup,
  useUpdateUserBackup,
  useUserBackupSnapshots,
  type Backup,
  type BackupResult,
  type BackupSnapshotEntry,
} from "@/shared/api/queries";
import { useQueryClient } from "@tanstack/react-query";

// /files/backups/$id — detail page. Shows the backup config, the
// most-recent run, run history, and (for snapshot-mode backups) the
// list of snapshots currently on disk with "browse" links.
//
// Edit on this page is intentionally minimal: schedule + enabled
// toggle. Renaming and re-pointing source/dest happens via Delete +
// recreate; flipping mode/retention is also a wizard-only action
// (the destination layout changes so a mid-life flip is a config
// migration, not a single-field edit).
export const Route = createFileRoute("/files/backups/$id/")({
  component: BackupDetailPage,
});

function BackupDetailPage() {
  const { id } = Route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: backup, isLoading, error } = useUserBackup(id);
  const isSnapshot = backup?.mode === "snapshot";
  const { data: snapshots = [] } = useUserBackupSnapshots(id, !!backup && isSnapshot);
  const runMutation = useRunUserBackup();
  const deleteMutation = useDeleteUserBackup();
  const updateMutation = useUpdateUserBackup();
  const [editingSchedule, setEditingSchedule] = useState(false);
  const [scheduleDraft, setScheduleDraft] = useState("");

  if (isLoading) return <div className="space-y-6">Loading…</div>;
  if (error || !backup) {
    return (
      <div className="space-y-6">
        <Link to="/files/backups" className="text-sm text-muted-foreground hover:underline">← Back to backups</Link>
        <p className="text-sm text-destructive">Backup not found or you don't have access.</p>
      </div>
    );
  }

  const handleRun = async () => {
    try {
      await runMutation.mutateAsync(backup.id);
      qc.invalidateQueries({ queryKey: ["user", "backups", backup.id] });
      qc.invalidateQueries({ queryKey: ["user", "backups", backup.id, "snapshots"] });
    } catch (e) {
      console.error("run failed", e);
    }
  };

  const handleDelete = async () => {
    if (!confirm(`Delete backup "${backup.name}"? This cannot be undone.`)) return;
    try {
      await deleteMutation.mutateAsync(backup.id);
      navigate({ to: "/files/backups" });
    } catch (e) {
      console.error("delete failed", e);
    }
  };

  const handleToggleDisabled = async () => {
    try {
      await updateMutation.mutateAsync({
        id: backup.id,
        body: passThroughBody(backup, { disabled: !backup.disabled }),
      });
      qc.invalidateQueries({ queryKey: ["user", "backups", backup.id] });
      qc.invalidateQueries({ queryKey: ["user", "backups"] });
    } catch (e) {
      console.error("update failed", e);
    }
  };

  const handleSaveSchedule = async () => {
    const next = scheduleDraft.trim();
    if (!next) return;
    try {
      await updateMutation.mutateAsync({
        id: backup.id,
        body: passThroughBody(backup, { schedule: next }),
      });
      setEditingSchedule(false);
      qc.invalidateQueries({ queryKey: ["user", "backups", backup.id] });
      qc.invalidateQueries({ queryKey: ["user", "backups"] });
    } catch (e) {
      console.error("schedule update failed", e);
    }
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <Link to="/files/backups" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        ← Back to backups
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{backup.name}</h1>
          <p className="text-sm text-muted-foreground mt-1">
            {backup.srcBucket} → {backup.dstBucket}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            onClick={handleRun}
            disabled={backup.disabled || runMutation.isPending}
          >
            {runMutation.isPending ? "Running…" : "Run now"}
          </Button>
          <Button variant="outline" onClick={handleToggleDisabled} disabled={updateMutation.isPending}>
            {backup.disabled ? "Enable" : "Disable"}
          </Button>
          <Button variant="ghost" onClick={handleDelete} disabled={deleteMutation.isPending}>
            Delete
          </Button>
        </div>
      </header>

      <section className="rounded-lg border bg-card p-6 space-y-4">
        <h2 className="text-sm font-medium text-muted-foreground">Configuration</h2>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="text-muted-foreground">Source bucket</dt>
          <dd className="font-mono">{backup.srcBucket}{backup.srcPrefix ? ` (${backup.srcPrefix})` : ""}</dd>
          <dt className="text-muted-foreground">Destination bucket</dt>
          <dd className="font-mono">
            {backup.dstBucket}
            {!isSnapshot && backup.dstPrefix ? ` (${backup.dstPrefix})` : ""}
          </dd>
          <dt className="text-muted-foreground">Mode</dt>
          <dd className="font-mono">{backup.mode ?? "mirror"}</dd>
          {isSnapshot && (
            <>
              <dt className="text-muted-foreground">Retention</dt>
              <dd className="font-mono">
                {formatRetention(backup)}
              </dd>
            </>
          )}
          <dt className="text-muted-foreground">Schedule</dt>
          <dd className="flex items-center gap-2">
            {!editingSchedule ? (
              <>
                <span className="font-mono">{backup.schedule}</span>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => { setScheduleDraft(backup.schedule); setEditingSchedule(true); }}
                >
                  Edit
                </Button>
              </>
            ) : (
              <>
                <input
                  className="rounded-md border bg-background px-2 py-1 text-sm font-mono"
                  value={scheduleDraft}
                  onChange={(e) => setScheduleDraft(e.target.value)}
                  placeholder="0 3 * * * or manual"
                />
                <Button size="sm" onClick={handleSaveSchedule} disabled={updateMutation.isPending}>Save</Button>
                <Button size="sm" variant="ghost" onClick={() => setEditingSchedule(false)}>Cancel</Button>
              </>
            )}
          </dd>
          <dt className="text-muted-foreground">Disabled</dt>
          <dd>{backup.disabled ? "yes" : "no"}</dd>
          <dt className="text-muted-foreground">Created</dt>
          <dd>{new Date(backup.createdAt).toLocaleString()}</dd>
        </dl>
        {updateMutation.error && (
          <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {String((updateMutation.error as Error).message || "Failed to save")}
          </div>
        )}
      </section>

      {isSnapshot && (
        <section className="rounded-lg border bg-card p-6 space-y-4">
          <h2 className="text-sm font-medium text-muted-foreground">Snapshots on disk</h2>
          {snapshots.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No snapshots yet. Click "Run now" — the first run lands at <span className="font-mono">{`${backup.dstBucket}/{name}/{timestamp}/`}</span>.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-left text-xs text-muted-foreground border-b">
                  <tr>
                    <th className="py-2 pr-4">Timestamp</th>
                    <th className="py-2 pr-4 text-right">Objects</th>
                    <th className="py-2 pr-4 text-right">Size</th>
                    <th className="py-2 pr-4 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y">
                  {snapshots.map((s) => (
                    <SnapshotRow key={s.prefix} s={s} backup={backup} />
                  ))}
                </tbody>
              </table>
              <p className="text-xs text-muted-foreground mt-3">
                Showing the {snapshots.length} most recent snapshot{snapshots.length === 1 ? "" : "s"}. Older
                snapshots are pruned automatically by the retention policy.
              </p>
              <div className="mt-4 flex items-center gap-2">
                <Link
                  to="/files/backups/$id/restore"
                  params={{ id: backup.id }}
                  search={{ ts: undefined }}
                  className="inline-flex items-center justify-center rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
                >
                  Restore from snapshot…
                </Link>
                <span className="text-xs text-muted-foreground">
                  Copies a chosen snapshot back to a destination of your choice.
                </span>
              </div>
            </div>
          )}
        </section>
      )}

      <section className="rounded-lg border bg-card p-6 space-y-4">
        <h2 className="text-sm font-medium text-muted-foreground">Recent runs</h2>
        {(!backup.history || backup.history.length === 0) ? (
          <p className="text-sm text-muted-foreground">No runs yet. Click "Run now" to kick one off.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs text-muted-foreground border-b">
                <tr>
                  <th className="py-2 pr-4">Started</th>
                  <th className="py-2 pr-4">Result</th>
                  <th className="py-2 pr-4 text-right">Objects</th>
                  <th className="py-2 pr-4 text-right">Bytes</th>
                  {isSnapshot && <th className="py-2 pr-4 text-right">Pruned</th>}
                  <th className="py-2 pr-4">Errors</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {backup.history.map((r, idx) => <HistoryRow key={idx} r={r} showPruned={isSnapshot} />)}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

// passThroughBody clones the current Backup back into a create-shaped
// body so the update mutation only changes the fields the caller
// explicitly overrides. Keeps every edit handler from re-typing the
// pass-through field list.
function passThroughBody(
  b: Backup,
  patch: Partial<{
    schedule: string;
    disabled: boolean;
  }>,
) {
  return {
    name: b.name,
    srcRegionId: b.srcRegionId,
    srcBucket: b.srcBucket,
    srcPrefix: b.srcPrefix ?? undefined,
    dstRegionId: b.dstRegionId,
    dstBucket: b.dstBucket,
    dstPrefix: b.dstPrefix ?? undefined,
    schedule: patch.schedule ?? b.schedule,
    disabled: patch.disabled ?? b.disabled ?? false,
    mode: b.mode,
    retention: b.retention,
  };
}

function HistoryRow({ r, showPruned }: { r: BackupResult; showPruned: boolean }) {
  const started = r.startedAt ? new Date(r.startedAt).toLocaleString() : "—";
  return (
    <tr>
      <td className="py-2 pr-4 whitespace-nowrap">{started}</td>
      <td className={`py-2 pr-4 font-medium ${r.success ? "text-green-600" : "text-red-600"}`}>
        {r.success ? "success" : "failed"}
      </td>
      <td className="py-2 pr-4 text-right">{r.objectsCopied}</td>
      <td className="py-2 pr-4 text-right">{formatBytes(r.bytesCopied)}</td>
      {showPruned && (
        <td className="py-2 pr-4 text-right">
          {r.snapshotsPruned ?? 0}
        </td>
      )}
      <td className="py-2 pr-4 text-xs text-muted-foreground">
        {r.errors && r.errors.length > 0 ? r.errors[0] : "—"}
      </td>
    </tr>
  );
}

function SnapshotRow({ s, backup }: { s: BackupSnapshotEntry; backup: Backup }) {
  const when = new Date(s.timestamp).toLocaleString();
  // Each row gets two actions: Browse (drills into the destination
  // bucket at this snapshot's prefix) and Restore (pre-fills the
  // restore wizard with this exact timestamp selected). The wizard
  // reads `?ts=...` to seed Step 1.
  const tsParam = isoToSnapshotTimestamp(s.timestamp);
  return (
    <tr>
      <td className="py-2 pr-4 whitespace-nowrap">
        <span className="font-mono">{when}</span>
      </td>
      <td className="py-2 pr-4 text-right">{s.objects}</td>
      <td className="py-2 pr-4 text-right">{formatBytes(s.bytes)}</td>
      <td className="py-2 pr-4 text-right">
        <Link
          to="/files/$regionId/b/$bid"
          params={{ regionId: backup.dstRegionId, bid: backup.dstBucket }}
          search={{ prefix: s.prefix, token: "" }}
          className="text-primary hover:underline mr-3"
        >
          Browse →
        </Link>
        <Link
          to="/files/backups/$id/restore"
          params={{ id: backup.id }}
          search={{ ts: tsParam }}
          className="text-primary hover:underline"
        >
          Restore →
        </Link>
      </td>
    </tr>
  );
}

// isoToSnapshotTimestamp converts an ISO-8601 string into the
// "YYYY-MM-DD_HH:MM:SS" layout the backend wants for the
// snapshotTimestamp field. We deliberately don't go through
// toLocaleString — the on-disk layout is UTC, and any locale shift
// would land the wizard on the wrong snapshot.
function isoToSnapshotTimestamp(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}_${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
}

function formatRetention(b: Backup): string {
  const r = b.retention;
  if (!r || (!r.keepDaily && !r.keepWeekly && !r.keepMonthly)) {
    return "default (7d / 4w / 12m)";
  }
  return `${r.keepDaily ?? 0}d / ${r.keepWeekly ?? 0}w / ${r.keepMonthly ?? 0}m`;
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
