import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/shared/ui/EmptyState";
import {
  useUserBackups,
  useRunUserBackup,
  useDeleteUserBackup,
  type Backup,
} from "@/shared/api/queries";
import { useQueryClient } from "@tanstack/react-query";

// /files/backups — list of the caller's scheduled backups. Sibling
// of /files/backups/new (wizard) and /files/backups/$id (detail), so
// the parent file (backups.tsx) is auto-derived as the layout — this
// is the index that renders the list.
export const Route = createFileRoute("/files/backups/")({
  component: BackupsListPage,
});

function BackupsListPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: backups = [], isLoading } = useUserBackups();
  const runMutation = useRunUserBackup();
  const deleteMutation = useDeleteUserBackup();

  const handleRun = async (id: string) => {
    try {
      await runMutation.mutateAsync(id);
      qc.invalidateQueries({ queryKey: ["user", "backups"] });
    } catch (e) {
      // The mutation surfaces the error; nothing more to do here.
      console.error("run backup failed", e);
    }
  };

  const handleDelete = async (b: Backup) => {
    if (!confirm(`Delete backup "${b.name}"? This cannot be undone.`)) return;
    try {
      await deleteMutation.mutateAsync(b.id);
      qc.invalidateQueries({ queryKey: ["user", "backups"] });
    } catch (e) {
      console.error("delete backup failed", e);
    }
  };

  if (isLoading) {
    return <div className="space-y-6">Loading...</div>;
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Backups</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Scheduled, named bucket-to-bucket copy jobs. Pick a source, a destination, and a cadence.
          </p>
        </div>
        <Button onClick={() => navigate({ to: "/files/backups/new" })}>+ New backup</Button>
      </header>

      {backups.length === 0 ? (
        <EmptyState
          icon="sync"
          title="No backups yet"
          description="Set up a scheduled copy from one bucket to another. Daily, weekly, monthly — or just an on-demand button."
          action={
            <Button onClick={() => navigate({ to: "/files/backups/new" })}>+ New backup</Button>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {backups.map((b) => (
            <Card key={b.id}>
              <CardHeader>
                <CardTitle className="text-base flex items-start justify-between gap-2">
                  <Link
                    to="/files/backups/$id"
                    params={{ id: b.id }}
                    className="hover:underline truncate"
                    title={b.name}
                  >
                    {b.name}
                  </Link>
                  <span className={`text-xs font-normal ${statusColor(b)}`}>
                    {statusLabel(b)}
                  </span>
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 text-sm">
                <div className="text-muted-foreground">
                  <div className="truncate" title={`${b.srcBucket} → ${b.dstBucket}`}>
                    <span className="font-medium text-foreground">{b.srcBucket}</span>
                    {" → "}
                    <span className="font-medium text-foreground">{b.dstBucket}</span>
                  </div>
                </div>
                <div className="flex items-center gap-2 text-xs">
                  <span className="rounded-md border bg-muted/30 px-1.5 py-0.5 font-mono">
                    {b.schedule}
                  </span>
                  {b.disabled && (
                    <span className="rounded-md border border-yellow-500/40 bg-yellow-500/10 px-1.5 py-0.5 text-yellow-700">
                      disabled
                    </span>
                  )}
                </div>
                <LastRun b={b} />
                <div className="flex items-center gap-2 pt-2">
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => handleRun(b.id)}
                    disabled={b.disabled || runMutation.isPending}
                  >
                    Run now
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    onClick={() => handleDelete(b)}
                    disabled={deleteMutation.isPending}
                  >
                    Delete
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}

// LastRun renders a one-line summary of the most recent BackupResult.
// Keeps the card dense; the detail page has the full history table.
function LastRun({ b }: { b: Backup }) {
  if (!b.lastResult) {
    return <p className="text-xs text-muted-foreground">No runs yet</p>;
  }
  const r = b.lastResult;
  const when = r.startedAt ? new Date(r.startedAt).toLocaleString() : "—";
  return (
    <p className="text-xs text-muted-foreground">
      Last run: {when} — {r.success ? "success" : "failed"} ({r.objectsCopied} objects)
    </p>
  );
}

function statusLabel(b: Backup): string {
  if (b.disabled) return "disabled";
  if (b.schedule === "manual") return "manual";
  return "scheduled";
}

function statusColor(b: Backup): string {
  if (b.disabled) return "text-yellow-600";
  if (b.lastResult && !b.lastResult.success) return "text-red-600";
  return "text-muted-foreground";
}
