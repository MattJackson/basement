import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  useUserBackup,
  useRunUserBackup,
  useDeleteUserBackup,
  useUpdateUserBackup,
  type BackupResult,
} from "@/shared/api/queries";
import { useQueryClient } from "@tanstack/react-query";

// /files/backups/$id — detail page. Shows the backup config, the
// most-recent run, and a history table (up to 10 runs). Edit is
// intentionally minimal in v1.5.0a: schedule + enabled toggle only.
// Renaming and re-pointing source/dest happens via Delete + recreate
// for now (v1.5.0b will broaden this).
export const Route = createFileRoute("/files/backups/$id")({
  component: BackupDetailPage,
});

function BackupDetailPage() {
  const { id } = Route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: backup, isLoading, error } = useUserBackup(id);
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
        body: {
          name: backup.name,
          srcRegionId: backup.srcRegionId,
          srcBucket: backup.srcBucket,
          srcPrefix: backup.srcPrefix ?? undefined,
          dstRegionId: backup.dstRegionId,
          dstBucket: backup.dstBucket,
          dstPrefix: backup.dstPrefix ?? undefined,
          schedule: backup.schedule,
          disabled: !backup.disabled,
        },
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
        body: {
          name: backup.name,
          srcRegionId: backup.srcRegionId,
          srcBucket: backup.srcBucket,
          srcPrefix: backup.srcPrefix ?? undefined,
          dstRegionId: backup.dstRegionId,
          dstBucket: backup.dstBucket,
          dstPrefix: backup.dstPrefix ?? undefined,
          schedule: next,
          disabled: backup.disabled ?? false,
        },
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
          <dd className="font-mono">{backup.dstBucket}{backup.dstPrefix ? ` (${backup.dstPrefix})` : ""}</dd>
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
                  <th className="py-2 pr-4">Errors</th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {backup.history.map((r, idx) => <HistoryRow key={idx} r={r} />)}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}

function HistoryRow({ r }: { r: BackupResult }) {
  const started = r.startedAt ? new Date(r.startedAt).toLocaleString() : "—";
  return (
    <tr>
      <td className="py-2 pr-4 whitespace-nowrap">{started}</td>
      <td className={`py-2 pr-4 font-medium ${r.success ? "text-green-600" : "text-red-600"}`}>
        {r.success ? "success" : "failed"}
      </td>
      <td className="py-2 pr-4 text-right">{r.objectsCopied}</td>
      <td className="py-2 pr-4 text-right">{formatBytes(r.bytesCopied)}</td>
      <td className="py-2 pr-4 text-xs text-muted-foreground">
        {r.errors && r.errors.length > 0 ? r.errors[0] : "—"}
      </td>
    </tr>
  );
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
