// OBS.USAGE v0.9.0k — /admin/usage storage overview dashboard.
//
// The "where is my storage going?" page operators want at 2am. Five
// big-number cards at the top, a per-cluster table in the middle, and
// two side-by-side top-buckets panels at the bottom. All from the
// existing per-cluster reads — no metrics persistence, no time series.

import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { adminPage } from "@/shared/layout/adminPage";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { humanizeBytes } from "@/shared/lib/format";
import {
  useUsageOverview,
  type UsagePerCluster,
  type UsageTopBucket,
} from "@/shared/api/queries";

export const Route = createFileRoute("/admin/usage")({
  component: adminPage(UsagePage),
});

function UsagePage() {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useUsageOverview();

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "usage", "overview"] });
  };

  if (isLoading) {
    return (
      <div className="space-y-6">
        <PageHeader title="Usage Overview" description="Storage usage across every cluster." />
        <div className="flex items-center justify-center min-h-[300px] text-muted-foreground">
          Loading usage data...
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader
          title="Usage Overview"
          description="Storage usage across every cluster."
          actions={<Button variant="outline" onClick={handleRefresh}>Refresh</Button>}
        />
        <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
          Failed to load usage overview: {(error as Error).message}
        </div>
      </div>
    );
  }

  if (!data) return null;

  const { totals, perCluster, topBucketsByBytes, topBucketsByObjects } = data;
  const unhealthyCount = perCluster.filter((c) => !c.healthy).length;

  return (
    <div className="space-y-8">
      <PageHeader
        title="Usage Overview"
        description="Snapshot of storage usage across every cluster you can see. Numbers reflect the last successful per-cluster fetch."
        actions={<Button variant="outline" onClick={handleRefresh}>Refresh</Button>}
      />

      {/* Top: big-number stat cards. */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-5 gap-3">
        <StatCard label="Clusters" value={totals.clusters.toLocaleString()} />
        <StatCard label="Buckets" value={totals.buckets.toLocaleString()} />
        <StatCard label="Keys" value={totals.keys.toLocaleString()} />
        <StatCard label="Bytes" value={humanizeBytes(totals.bytes)} />
        <StatCard label="Grants" value={totals.grants.toLocaleString()} />
      </div>

      {unhealthyCount > 0 && (
        <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
          {unhealthyCount} cluster{unhealthyCount > 1 ? "s" : ""} returned an error
          during the fan-out. Their rows below show the failure detail; totals
          reflect only the healthy clusters.
        </div>
      )}

      {/* Middle: per-cluster table. */}
      <section className="space-y-3">
        <h2 className="text-lg font-semibold tracking-tight">Per cluster</h2>
        {perCluster.length === 0 ? (
          <div className="rounded-lg border bg-muted/30 p-6 text-sm text-muted-foreground text-center">
            No clusters connected yet.{" "}
            <Link to="/admin/clusters" className="text-primary hover:underline">
              Add one
            </Link>{" "}
            to populate the dashboard.
          </div>
        ) : (
          <div className="rounded-lg border bg-card overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Cluster</TableHead>
                  <TableHead className="text-right w-[140px]">Bytes</TableHead>
                  <TableHead className="text-right w-[120px]">Objects</TableHead>
                  <TableHead className="text-right w-[100px]">Buckets</TableHead>
                  <TableHead className="text-right w-[100px]">Keys</TableHead>
                  <TableHead className="w-[120px]">Status</TableHead>
                  <TableHead className="w-[120px] text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {perCluster.map((row) => (
                  <PerClusterRow key={row.id} row={row} />
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      {/* Bottom: side-by-side top-N panels. */}
      <section className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <TopBucketsPanel
          title="Top buckets by size"
          rows={topBucketsByBytes}
          metricLabel="Bytes"
          metricFormat={humanizeBytes}
        />
        <TopBucketsPanel
          title="Top buckets by object count"
          rows={topBucketsByObjects}
          metricLabel="Objects"
          metricFormat={(n) => n.toLocaleString()}
        />
      </section>
    </div>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="pt-5">
        <div className="text-xs uppercase tracking-wider text-muted-foreground">
          {label}
        </div>
        <div className="text-2xl font-semibold tracking-tight tabular-nums mt-1">
          {value}
        </div>
      </CardContent>
    </Card>
  );
}

function PerClusterRow({ row }: { row: UsagePerCluster }) {
  const navigate = useNavigate();
  const onClick = () => {
    if (row.healthy) {
      navigate({ to: "/admin/clusters/$cid", params: { cid: row.id } });
    }
  };

  return (
    <TableRow
      className={row.healthy ? "cursor-pointer hover:bg-muted/50" : ""}
      onClick={onClick}
    >
      <TableCell>
        <div className="font-medium">{row.label || row.id.slice(0, 8)}</div>
        {!row.healthy && row.error && (
          <div className="text-xs text-destructive mt-1 truncate max-w-md" title={row.error}>
            {row.error}
          </div>
        )}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {row.healthy ? humanizeBytes(row.bytes) : "—"}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {row.healthy ? row.objects.toLocaleString() : "—"}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {row.healthy ? row.buckets.toLocaleString() : "—"}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {row.healthy ? row.keys.toLocaleString() : "—"}
      </TableCell>
      <TableCell>
        {row.healthy ? (
          <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:text-emerald-400">
            Healthy
          </span>
        ) : (
          <span className="inline-flex items-center rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive">
            Error
          </span>
        )}
      </TableCell>
      <TableCell className="text-right">
        {row.healthy ? (
          <Link
            to="/admin/migrate"
            search={{ srcCid: row.id }}
            onClick={(e) => e.stopPropagation()}
            className="text-xs font-medium text-primary hover:underline"
            title="Bulk-copy every bucket from this cluster to another cluster"
          >
            Migrate &rarr;
          </Link>
        ) : (
          <span className="text-xs text-muted-foreground">&mdash;</span>
        )}
      </TableCell>
    </TableRow>
  );
}

function TopBucketsPanel({
  title,
  rows,
  metricLabel,
  metricFormat,
}: {
  title: string;
  rows: UsageTopBucket[];
  metricLabel: string;
  metricFormat: (n: number) => string;
}) {
  const navigate = useNavigate();
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <div className="text-sm text-muted-foreground text-center py-6">
            No buckets to rank yet.
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Bucket</TableHead>
                <TableHead>Cluster</TableHead>
                <TableHead className="text-right w-[120px]">{metricLabel}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => {
                const display =
                  metricLabel === "Bytes" ? metricFormat(row.bytes) : metricFormat(row.objects);
                return (
                  <TableRow
                    key={`${row.clusterId}:${row.bucketId}`}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() =>
                      navigate({
                        to: "/admin/clusters/$cid/buckets/$id",
                        params: { cid: row.clusterId, id: row.bucketId },
                      })
                    }
                  >
                    <TableCell className="font-medium">
                      {row.bucketAlias || row.bucketId.slice(0, 8)}
                    </TableCell>
                    <TableCell className="text-muted-foreground">
                      {row.clusterLabel}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{display}</TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: React.ReactNode;
}) {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{title}</h1>
        {description && (
          <p className="text-sm text-muted-foreground mt-1 max-w-3xl">{description}</p>
        )}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </header>
  );
}
