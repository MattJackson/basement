import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/shared/ui/EmptyState";
import {
  useFederations,
  useDeleteFederation,
  useUserRegions,
  type FederatedBucket,
  type FederationHealth,
  type UserRegion,
} from "@/shared/api/queries";
import { useQueryClient } from "@tanstack/react-query";

// /files/federated-buckets — list of the caller's federated buckets.
// Mirror of /files/backups index, but rendered as a wide table instead
// of cards because the per-federation summary carries one extra column
// (per-replica health roll-up) that doesn't fit the card grid cleanly.
//
// 10s auto-refresh inherited from the useFederations() hook so a
// freshly-completed engine tick lands in the list without a manual
// refresh — the engine's default tick interval is also 10s so the FE
// is in lockstep with the source of truth.
export const Route = createFileRoute("/files/federated-buckets/")({
  component: FederatedBucketsListPage,
});

function FederatedBucketsListPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: federations = [], isLoading } = useFederations();
  const { data: regions = [] } = useUserRegions();
  const deleteMutation = useDeleteFederation();

  // Build a regionId -> alias lookup so the list renders human-readable
  // labels rather than UUID slugs. Falls back to "(unknown region)" when
  // a federation references a region the caller no longer owns (e.g. a
  // key was deleted but the federation outlived it).
  const regionAlias = (id: string): string => {
    const r = regions.find((rr) => rr.id === id);
    return r?.alias || r?.endpoint || "(unknown region)";
  };

  const handleDelete = async (fb: FederatedBucket) => {
    const msg =
      `Delete federation "${fb.name}"? Replica data on each backend is preserved — ` +
      `only the basement replication record is removed. This cannot be undone.`;
    if (!confirm(msg)) return;
    try {
      await deleteMutation.mutateAsync(fb.id);
      qc.invalidateQueries({ queryKey: ["user", "federations"] });
    } catch (e) {
      console.error("delete federation failed", e);
    }
  };

  if (isLoading) {
    return <div className="space-y-6">Loading…</div>;
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
            Federations
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            Multi-backend mirrored buckets. Pick a primary, add replicas, and
            basement keeps every backend in sync continuously.
          </p>
        </div>
        <Button onClick={() => navigate({ to: "/files/federated-buckets/new" })}>
          + New federation
        </Button>
      </header>

      {federations.length === 0 ? (
        <EmptyState
          icon="database"
          title="No federations yet"
          description="Create one to mirror a bucket across backends — e.g. home Garage + an off-site B2 copy that stays in lock-step automatically."
          action={
            <Button onClick={() => navigate({ to: "/files/federated-buckets/new" })}>
              + New federation
            </Button>
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Primary</TableHead>
                <TableHead className="text-right">Replicas</TableHead>
                <TableHead>Health</TableHead>
                <TableHead>Last activity</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {federations.map((fb) => (
                <FederationRow
                  key={fb.id}
                  fb={fb}
                  regionAlias={regionAlias}
                  onDelete={handleDelete}
                  isDeleting={deleteMutation.isPending}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

// FederationRow renders one row of the list. Pulled into its own
// component so the inline conditional rendering (health pill +
// last-activity + actions) stays readable.
function FederationRow({
  fb,
  regionAlias,
  onDelete,
  isDeleting,
}: {
  fb: FederatedBucket;
  regionAlias: (id: string) => string;
  onDelete: (fb: FederatedBucket) => void;
  isDeleting: boolean;
}) {
  const inSync = fb.replicas.filter(
    (r) => !r.health || r.health === "in-sync",
  ).length;
  const total = fb.replicas.length;
  const summary = `${inSync}/${total} in-sync`;
  const last = mostRecentReplicaSync(fb);
  return (
    <TableRow>
      <TableCell>
        <Link
          to="/files/federated-buckets/$id"
          params={{ id: fb.id }}
          className="font-medium hover:underline"
        >
          {fb.name}
        </Link>
      </TableCell>
      <TableCell className="font-mono text-xs text-muted-foreground">
        <div className="flex flex-col">
          <span>{regionAlias(fb.primary.regionId)}</span>
          <span className="text-foreground">{fb.primary.bucket}</span>
        </div>
      </TableCell>
      <TableCell className="text-right tabular-nums">{total}</TableCell>
      <TableCell>
        <HealthPill health={fb.computedHealth || "in-sync"} label={summary} />
      </TableCell>
      <TableCell className="text-xs text-muted-foreground whitespace-nowrap">
        {last ? new Date(last).toLocaleString() : "—"}
      </TableCell>
      <TableCell className="text-right whitespace-nowrap">
        <Link
          to="/files/federated-buckets/$id"
          params={{ id: fb.id }}
          className="text-sm text-primary hover:underline mr-3"
        >
          View
        </Link>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => onDelete(fb)}
          disabled={isDeleting}
        >
          Delete
        </Button>
      </TableCell>
    </TableRow>
  );
}

// mostRecentReplicaSync returns the ISO timestamp of the most-recently
// successful replicate across all replicas. Returns null when no
// replica has ever synced (fresh federation).
function mostRecentReplicaSync(fb: FederatedBucket): string | null {
  let best: string | null = null;
  for (const r of fb.replicas) {
    if (!r.lastSync) continue;
    if (!best || r.lastSync > best) best = r.lastSync;
  }
  return best;
}

// HealthPill colours a small badge to match the federation's overall
// computedHealth. Green for in-sync, amber for any kind of behind,
// red for broken. Same colour vocabulary as the rest of the v1.x
// status rendering (last-run badge on backups, scrub state on
// /admin/clusters/$cid).
function HealthPill({
  health,
  label,
}: {
  health: FederationHealth;
  label: string;
}) {
  const cls = healthClass(health);
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs font-medium ${cls}`}
      data-testid={`health-pill-${health || "in-sync"}`}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current opacity-80" />
      {label}
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

// Exported for unit tests that need to mirror the same lookup the
// route does. Not exposed via the route component itself.
export type { UserRegion };
