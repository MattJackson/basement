import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  useFederation,
  useFailoverFederation,
  useResyncFederation,
  useDeleteFederation,
  useUserRegions,
  type FederatedBucket,
  type FederationHealth,
  type FederationReplicaTarget,
} from "@/shared/api/queries";
import { useQueryClient } from "@tanstack/react-query";

// /files/federated-buckets/$id — federation detail page.
//
// Three sections:
//   - Configuration: primary, policy summary, created/updated stamps
//   - Replicas: table with per-target health + lag + "Promote to primary"
//   - Actions header: Resync / Delete / (Edit lands in v1.6.0e+)
//
// 5s auto-refresh inherited from useFederation() so per-replica health
// pills react quickly when an operator clicks Resync or Promote — the
// engine recomputes lag on every tick, so the FE seeing the recomputed
// state within one tick keeps the operator's mental model honest.
//
// Promote-to-primary opens a confirmation Dialog (popups-max-2-fields
// rule: it's a confirmation, not a form — two read-only fields, old +
// new primary, plus a confirm button).
export const Route = createFileRoute("/files/federated-buckets/$id/")({
  component: FederationDetailPage,
});

function FederationDetailPage() {
  const { id } = Route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: federation, isLoading, error } = useFederation(id);
  const { data: regions = [] } = useUserRegions();
  const failoverMutation = useFailoverFederation();
  const resyncMutation = useResyncFederation();
  const deleteMutation = useDeleteFederation();

  const [promoteTarget, setPromoteTarget] =
    useState<FederationReplicaTarget | null>(null);

  if (isLoading) return <div className="space-y-6">Loading…</div>;
  if (error || !federation) {
    return (
      <div className="space-y-6">
        <Link
          to="/files/federated-buckets"
          className="text-sm text-muted-foreground hover:underline"
        >
          ← Back to federations
        </Link>
        <p className="text-sm text-destructive">
          Federation not found or you don't have access.
        </p>
      </div>
    );
  }

  const regionAlias = (rid: string): string => {
    const r = regions.find((rr) => rr.id === rid);
    return r?.alias || r?.endpoint || "(unknown region)";
  };

  const handleResync = async () => {
    try {
      await resyncMutation.mutateAsync(federation.id);
      qc.invalidateQueries({ queryKey: ["user", "federations", federation.id] });
    } catch (e) {
      console.error("resync failed", e);
    }
  };

  const handleDelete = async () => {
    const msg =
      `Delete federation "${federation.name}"? Replica data on each backend is preserved — ` +
      `only the basement replication record is removed. This cannot be undone.`;
    if (!confirm(msg)) return;
    try {
      await deleteMutation.mutateAsync(federation.id);
      navigate({ to: "/files/federated-buckets" });
    } catch (e) {
      console.error("delete failed", e);
    }
  };

  const handleConfirmPromote = async () => {
    if (!promoteTarget) return;
    try {
      await failoverMutation.mutateAsync({
        id: federation.id,
        body: {
          newPrimaryRegionId: promoteTarget.regionId,
          newPrimaryBucket: promoteTarget.bucket,
        },
      });
      setPromoteTarget(null);
      qc.invalidateQueries({ queryKey: ["user", "federations", federation.id] });
      qc.invalidateQueries({ queryKey: ["user", "federations"] });
    } catch (e) {
      console.error("failover failed", e);
    }
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <Link
        to="/files/federated-buckets"
        className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
      >
        ← Back to federations
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
            {federation.name}
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            {regionAlias(federation.primary.regionId)} /{" "}
            <span className="font-mono">{federation.primary.bucket}</span>{" "}
            → {federation.replicas.length} replica
            {federation.replicas.length === 1 ? "" : "s"}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {federation.policy.autoFailover && (
            <AutoFailoverArmedBadge
              autoFailoverSec={federation.policy.autoFailoverSec ?? 0}
            />
          )}
          <StatusBadge health={federation.computedHealth || "in-sync"} />
        </div>
      </header>

      <section className="rounded-lg border bg-card p-6 space-y-4">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-medium text-muted-foreground">
            Configuration
          </h2>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleResync}
              disabled={resyncMutation.isPending}
              data-testid="resync-button"
            >
              {resyncMutation.isPending ? "Queuing…" : "Resync now"}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
              data-testid="delete-button"
            >
              Delete
            </Button>
          </div>
        </div>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="text-muted-foreground">Primary region</dt>
          <dd className="font-mono">{regionAlias(federation.primary.regionId)}</dd>
          <dt className="text-muted-foreground">Primary bucket</dt>
          <dd className="font-mono">{federation.primary.bucket}</dd>
          <dt className="text-muted-foreground">Replicas</dt>
          <dd>{federation.replicas.length}</dd>
          <dt className="text-muted-foreground">Sync mode</dt>
          <dd className="font-mono">
            {federation.policy.syncMode}
            {federation.policy.syncMode === "scheduled" && federation.policy.schedule
              ? ` (${federation.policy.schedule})`
              : ""}
          </dd>
          <dt className="text-muted-foreground">Lag alert</dt>
          <dd className="font-mono">{federation.policy.lagAlertSec}s</dd>
          <dt className="text-muted-foreground">Write quorum</dt>
          <dd className="font-mono">
            {federation.policy.writeQuorum} of {1 + federation.replicas.length}
          </dd>
          <dt className="text-muted-foreground">Auto-failover</dt>
          <dd className="font-mono">
            {federation.policy.autoFailover
              ? `on (${federation.policy.autoFailoverSec ?? 0}s)`
              : "off"}
          </dd>
          <dt className="text-muted-foreground">Created</dt>
          <dd>{new Date(federation.createdAt).toLocaleString()}</dd>
          <dt className="text-muted-foreground">Updated</dt>
          <dd>{new Date(federation.updatedAt).toLocaleString()}</dd>
        </dl>
        {(failoverMutation.error || resyncMutation.error || deleteMutation.error) && (
          <div
            className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
            data-testid="detail-error"
          >
            {String(
              ((failoverMutation.error || resyncMutation.error || deleteMutation.error) as Error)
                .message || "Action failed",
            )}
          </div>
        )}
      </section>

      <section className="rounded-lg border bg-card p-6 space-y-4">
        <h2 className="text-sm font-medium text-muted-foreground">Replicas</h2>
        {federation.replicas.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No replicas configured. Edit the federation to add at least one.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Region</TableHead>
                  <TableHead>Bucket</TableHead>
                  <TableHead>Health</TableHead>
                  <TableHead>Lag (objects / bytes)</TableHead>
                  <TableHead>Last sync</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {federation.replicas.map((rep, idx) => (
                  <ReplicaTableRow
                    key={`${rep.regionId}|${rep.bucket}|${idx}`}
                    rep={rep}
                    regionAlias={regionAlias(rep.regionId)}
                    onPromote={() => setPromoteTarget(rep)}
                    disabled={failoverMutation.isPending}
                  />
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      <Dialog open={!!promoteTarget} onOpenChange={(o) => !o && setPromoteTarget(null)}>
        <DialogHeader>
          <DialogTitle>Promote replica to primary?</DialogTitle>
          <DialogDescription>
            Swaps the current primary with the selected replica. The
            replication queue drains first so in-flight writes flush before the
            swap. Future writes route to the new primary.
          </DialogDescription>
        </DialogHeader>
        <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
          <dt className="text-muted-foreground">Old primary</dt>
          <dd className="font-mono">
            {regionAlias(federation.primary.regionId)} / {federation.primary.bucket}
          </dd>
          <dt className="text-muted-foreground">New primary</dt>
          <dd className="font-mono" data-testid="promote-new-primary">
            {promoteTarget
              ? `${regionAlias(promoteTarget.regionId)} / ${promoteTarget.bucket}`
              : ""}
          </dd>
        </dl>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => setPromoteTarget(null)}
            disabled={failoverMutation.isPending}
          >
            Cancel
          </Button>
          <Button
            onClick={handleConfirmPromote}
            disabled={failoverMutation.isPending}
            data-testid="confirm-promote"
          >
            {failoverMutation.isPending ? "Promoting…" : "Promote to primary"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}

function ReplicaTableRow({
  rep,
  regionAlias,
  onPromote,
  disabled,
}: {
  rep: FederationReplicaTarget;
  regionAlias: string;
  onPromote: () => void;
  disabled: boolean;
}) {
  const health = (rep.health || "in-sync") as FederationHealth;
  return (
    <TableRow>
      <TableCell className="font-mono text-xs">{regionAlias}</TableCell>
      <TableCell className="font-mono">{rep.bucket}</TableCell>
      <TableCell>
        <HealthPill health={health} />
      </TableCell>
      <TableCell className="font-mono text-xs">
        {rep.lagObjects ?? 0} / {formatBytes(rep.lagBytes ?? 0)}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
        {rep.lastSync ? new Date(rep.lastSync).toLocaleString() : "never"}
      </TableCell>
      <TableCell className="text-right whitespace-nowrap">
        <Button
          variant="outline"
          size="sm"
          onClick={onPromote}
          disabled={disabled}
          data-testid={`promote-${rep.regionId}-${rep.bucket}`}
        >
          Promote to primary
        </Button>
      </TableCell>
    </TableRow>
  );
}

function HealthPill({ health }: { health: FederationHealth }) {
  const label = health || "in-sync";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs font-medium ${healthClass(health)}`}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current opacity-80" />
      {label}
    </span>
  );
}

// AutoFailoverArmedBadge surfaces the v1.6.0f auto-failover policy in
// the header — a subtle blue pill next to the StatusBadge so operators
// know the watchdog will promote a replica if the primary stays
// unreachable for autoFailoverSec. Tooltip gives the precise window.
//
// Visible only when Policy.AutoFailover === true; never rendered for
// federations using the v1.6.0 default of manual failover only.
function AutoFailoverArmedBadge({ autoFailoverSec }: { autoFailoverSec: number }) {
  const human =
    autoFailoverSec >= 60
      ? `${Math.round(autoFailoverSec / 60)}m`
      : `${autoFailoverSec}s`;
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-md border border-blue-500/40 bg-blue-500/10 px-2 py-1 text-xs font-medium text-blue-700 dark:text-blue-400"
      data-testid="auto-failover-armed-badge"
      title={`Watchdog will promote the healthiest replica after ${autoFailoverSec}s of primary unreachability.`}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      Auto-failover armed ({human})
    </span>
  );
}

function StatusBadge({ health }: { health: FederationHealth }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium ${healthClass(health)}`}
      data-testid="federation-status-badge"
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {health || "in-sync"}
    </span>
  );
}

function healthClass(health: FederationHealth): string {
  switch (health) {
    case "broken":
      return "border-red-500/40 bg-red-500/10 text-red-700 dark:text-red-400";
    case "stale":
      return "border-amber-600/40 bg-amber-600/10 text-amber-700 dark:text-amber-400";
    case "lagging":
      return "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400";
    default:
      return "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400";
  }
}

// formatBytes — kept inline to match the backup detail page's helper;
// each route owns its own copy because the union "do we need this
// hard enough to share" question only resolves once we have 3+
// callers (k.i.s.s.).
function formatBytes(n: number): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`;
}

// Re-exports so the test file can import the typed shape without
// pulling /shared/api/queries directly when mocking the hook.
export type { FederatedBucket };
