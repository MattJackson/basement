// OBS.USAGE v0.9.0k — /admin/usage storage overview dashboard.
//
// The "where is my storage going?" page operators want at 2am. Five
// big-number cards at the top, a per-cluster table in the middle, and
// two side-by-side top-buckets panels at the bottom.
//
// v1.0.0d: inline trend charts. A "Trend" toggle in the header
// switches the top-buckets rows from "click to navigate" to
// "click to expand an inline sparkline". The sparkline pulls
// historical samples from /admin/usage/series — hourly
// snapshots written by the background scheduler.
//
// v1.4.0c: storage analytics polish.
//   - per-cluster Growth % column (7d change vs current bytes)
//   - top-growing-buckets panel (sorted by 7d growth %)
//   - anomaly banner when any bucket grew >100% in 7d
//   - time-range selector (7d / 30d / 90d) feeds the inline trend chart

import { useEffect, useMemo, useState } from "react";
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
  useUsageSeries,
  type UsagePerCluster,
  type UsageTopBucket,
  type UsageSeriesPoint,
} from "@/shared/api/queries";

export const Route = createFileRoute("/admin/usage")({
  component: adminPage(UsagePage),
});

// rangeDaysOptions is the FE-side list of windows the time-range
// selector exposes. The backend already accepts the full RFC3339
// range via ?from=&to= (clamped server-side at 90d), so this is
// pure FE work.
const RANGE_OPTIONS: { label: string; days: number }[] = [
  { label: "7d", days: 7 },
  { label: "30d", days: 30 },
  { label: "90d", days: 90 },
];

function UsagePage() {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useUsageOverview();
  // v1.0.0d: when ON, top-bucket rows expand into a sparkline
  // instead of navigating to the bucket detail page.
  const [trendMode, setTrendMode] = useState(false);
  // v1.4.0c: time-range selector — feeds inline trend charts + the
  // growth-rate / anomaly computations below.
  const [rangeDays, setRangeDays] = useState<number>(7);

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
    <UsagePageBody
      totals={totals}
      perCluster={perCluster}
      topBucketsByBytes={topBucketsByBytes}
      topBucketsByObjects={topBucketsByObjects}
      unhealthyCount={unhealthyCount}
      trendMode={trendMode}
      setTrendMode={setTrendMode}
      rangeDays={rangeDays}
      setRangeDays={setRangeDays}
      handleRefresh={handleRefresh}
    />
  );
}

// UsagePageBody is the rendered shell; pulled out so growth-rate state
// can hook into the page mid-render without nesting the whole thing
// under another conditional.
type UsagePageBodyProps = {
  totals: ReturnType<typeof useUsageOverview>["data"] extends infer R
    ? R extends { totals: infer T }
      ? T
      : never
    : never;
  perCluster: UsagePerCluster[];
  topBucketsByBytes: UsageTopBucket[];
  topBucketsByObjects: UsageTopBucket[];
  unhealthyCount: number;
  trendMode: boolean;
  setTrendMode: React.Dispatch<React.SetStateAction<boolean>>;
  rangeDays: number;
  setRangeDays: React.Dispatch<React.SetStateAction<number>>;
  handleRefresh: () => void;
};

function UsagePageBody({
  totals,
  perCluster,
  topBucketsByBytes,
  topBucketsByObjects,
  unhealthyCount,
  trendMode,
  setTrendMode,
  rangeDays,
  setRangeDays,
  handleRefresh,
}: UsagePageBodyProps) {
  // v1.4.0c: growth-rate registry — for every top bucket (across both
  // top-N panels) we fetch a {rangeDays}-window series and compute the
  // bucket's growth %. The registry is keyed by cluster+bucket so the
  // per-cluster table can sum across each cluster's contributing
  // top-buckets to surface a rolled-up cluster growth %.
  //
  // We dedupe across the two panels so a bucket that appears in both
  // by-bytes + by-objects only fetches once.
  const trackedBuckets = useMemo(() => {
    const seen = new Set<string>();
    const out: { clusterId: string; bucketId: string; clusterLabel: string; bucketAlias: string }[] = [];
    for (const r of [...topBucketsByBytes, ...topBucketsByObjects]) {
      const k = r.clusterId + "|" + r.bucketId;
      if (seen.has(k)) continue;
      seen.add(k);
      out.push({
        clusterId: r.clusterId,
        bucketId: r.bucketId,
        clusterLabel: r.clusterLabel,
        bucketAlias: r.bucketAlias,
      });
    }
    return out;
  }, [topBucketsByBytes, topBucketsByObjects]);

  const growth = useBucketGrowthRegistry(trackedBuckets, rangeDays);

  // Per-cluster growth: weighted-by-current-bytes average of the
  // tracked buckets in each cluster. Falls back to undefined when no
  // tracked bucket has a series yet — UI renders an em-dash.
  const perClusterGrowth = useMemo(() => {
    const byCluster = new Map<string, { sumWeighted: number; sumWeight: number }>();
    for (const r of topBucketsByBytes) {
      const g = growth.get(r.clusterId + "|" + r.bucketId);
      if (g === undefined || !Number.isFinite(g)) continue;
      const cur = byCluster.get(r.clusterId) ?? { sumWeighted: 0, sumWeight: 0 };
      const weight = Math.max(1, r.bytes);
      cur.sumWeighted += g * weight;
      cur.sumWeight += weight;
      byCluster.set(r.clusterId, cur);
    }
    const out = new Map<string, number>();
    for (const [cid, agg] of byCluster) {
      if (agg.sumWeight > 0) out.set(cid, agg.sumWeighted / agg.sumWeight);
    }
    return out;
  }, [topBucketsByBytes, growth]);

  // Top-growing buckets — re-sort topBucketsByBytes by growth %, drop
  // entries with no growth data, top-10.
  const topGrowingBuckets = useMemo(() => {
    const items = topBucketsByBytes
      .map((r) => ({ row: r, growth: growth.get(r.clusterId + "|" + r.bucketId) }))
      .filter((x): x is { row: UsageTopBucket; growth: number } =>
        x.growth !== undefined && Number.isFinite(x.growth),
      )
      .sort((a, b) => b.growth - a.growth)
      .slice(0, 10);
    return items;
  }, [topBucketsByBytes, growth]);

  // Anomaly: any tracked bucket with growth >100% within the window.
  const anomalies = useMemo(() => {
    const out: { row: UsageTopBucket; growth: number }[] = [];
    for (const r of topBucketsByBytes) {
      const g = growth.get(r.clusterId + "|" + r.bucketId);
      if (g !== undefined && Number.isFinite(g) && g > 100) {
        out.push({ row: r, growth: g });
      }
    }
    return out.sort((a, b) => b.growth - a.growth);
  }, [topBucketsByBytes, growth]);

  return (
    <div className="space-y-8">
      <PageHeader
        title="Usage Overview"
        description="Snapshot of storage usage across every cluster you can see. Numbers reflect the last successful per-cluster fetch."
        actions={
          <div className="flex items-center gap-2">
            <RangeSelector value={rangeDays} onChange={setRangeDays} />
            <Button
              variant={trendMode ? "default" : "outline"}
              onClick={() => setTrendMode((m) => !m)}
              title="Toggle inline time-series charts on bucket rows (hourly samples)"
            >
              {trendMode ? "Trend: on" : "Trend: off"}
            </Button>
            <Button variant="outline" onClick={handleRefresh}>Refresh</Button>
          </div>
        }
      />

      {/* v1.4.0c: anomaly banner. Surfaces buckets that more than
          doubled within the selected window. Operators see this at the
          top of the page so the "what changed?" question lands before
          they scroll. */}
      {anomalies.length > 0 && (
        <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3 text-sm text-amber-900 dark:text-amber-200 space-y-1">
          <div className="font-medium">
            {anomalies.length} bucket{anomalies.length > 1 ? "s" : ""} grew &gt;100%
            in the last {rangeDays}d
          </div>
          <ul className="text-xs space-y-0.5">
            {anomalies.slice(0, 5).map(({ row, growth: g }) => (
              <li key={row.clusterId + "|" + row.bucketId} className="flex items-center gap-2">
                <span className="font-mono">{row.bucketAlias || row.bucketId.slice(0, 8)}</span>
                <span className="text-amber-700 dark:text-amber-300">
                  +{Math.round(g)}%
                </span>
                <span className="text-muted-foreground">on {row.clusterLabel}</span>
                <Link
                  to="/admin/clusters/$cid/buckets/$id"
                  params={{ cid: row.clusterId, id: row.bucketId }}
                  className="ml-auto text-xs font-medium text-primary hover:underline"
                >
                  Investigate?
                </Link>
              </li>
            ))}
          </ul>
        </div>
      )}

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
                  <TableHead className="text-right w-[110px]" title={`% change vs ${rangeDays}d ago, weighted by current bytes`}>
                    Growth ({rangeDays}d)
                  </TableHead>
                  <TableHead className="w-[120px]">Status</TableHead>
                  <TableHead className="w-[120px] text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {perCluster.map((row) => (
                  <PerClusterRow
                    key={row.id}
                    row={row}
                    growthPercent={perClusterGrowth.get(row.id)}
                  />
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </section>

      {/* v1.4.0c: top-growing buckets panel. Sorted by growth %; only
          buckets the operator already cares about (currently in
          top-bytes) appear here, which keeps the fan-out bounded. */}
      {topGrowingBuckets.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-lg font-semibold tracking-tight">
            Buckets growing fastest ({rangeDays}d)
          </h2>
          <div className="rounded-lg border bg-card overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Bucket</TableHead>
                  <TableHead>Cluster</TableHead>
                  <TableHead className="text-right w-[120px]">Current</TableHead>
                  <TableHead className="text-right w-[100px]">Growth</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {topGrowingBuckets.map(({ row, growth: g }) => (
                  <TableRow key={row.clusterId + "|" + row.bucketId}>
                    <TableCell className="font-medium">
                      <Link
                        to="/admin/clusters/$cid/buckets/$id"
                        params={{ cid: row.clusterId, id: row.bucketId }}
                        className="hover:underline"
                      >
                        {row.bucketAlias || row.bucketId.slice(0, 8)}
                      </Link>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{row.clusterLabel}</TableCell>
                    <TableCell className="text-right tabular-nums">{humanizeBytes(row.bytes)}</TableCell>
                    <TableCell className="text-right tabular-nums">
                      <GrowthLabel growth={g} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </section>
      )}

      {/* Bottom: side-by-side top-N panels. */}
      <section className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <TopBucketsPanel
          title="Top buckets by size"
          rows={topBucketsByBytes}
          metricLabel="Bytes"
          metricFormat={humanizeBytes}
          trendMode={trendMode}
          rangeDays={rangeDays}
        />
        <TopBucketsPanel
          title="Top buckets by object count"
          rows={topBucketsByObjects}
          metricLabel="Objects"
          metricFormat={(n) => n.toLocaleString()}
          trendMode={trendMode}
          rangeDays={rangeDays}
        />
      </section>
    </div>
  );
}

// useBucketGrowthRegistry fans out one /admin/usage/series fetch per
// tracked bucket and returns a Map from "cid|bid" → growth %.
//
// Growth % is computed as ((last - first) / max(1, first)) * 100 on
// the bytes axis. Returns +Infinity for buckets that grew from a
// truly-zero baseline (treated as 100% on the consumer side; we'd
// rather call out a brand-new bucket than divide by zero).
//
// The fan-out caps at the top-N (≤20 distinct buckets across the two
// panels) so the page can render without flooding the backend.
function useBucketGrowthRegistry(
  buckets: { clusterId: string; bucketId: string }[],
  rangeDays: number,
): Map<string, number> {
  const [registry, setRegistry] = useState<Map<string, number>>(() => new Map());

  // Reset when the range changes — old percentages aren't valid for
  // the new window.
  useEffect(() => {
    setRegistry(new Map());
  }, [rangeDays]);

  return useBucketGrowthRegistryInner(buckets, rangeDays, registry, setRegistry);
}

function useBucketGrowthRegistryInner(
  buckets: { clusterId: string; bucketId: string }[],
  rangeDays: number,
  registry: Map<string, number>,
  setRegistry: React.Dispatch<React.SetStateAction<Map<string, number>>>,
) {
  // Use a stable serialization for the dependency array so React doesn't
  // re-render every time we receive a new array reference with the same
  // contents.
  const key = buckets.map((b) => b.clusterId + "|" + b.bucketId).join(",");
  // Render-time effect: kick off lazy series fetches for every bucket
  // we haven't yet hydrated.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const updates: [string, number][] = [];
      for (const b of buckets) {
        const k = b.clusterId + "|" + b.bucketId;
        if (registry.has(k)) continue;
        try {
          const to = new Date();
          const from = new Date(to.getTime() - rangeDays * 24 * 60 * 60 * 1000);
          const params = new URLSearchParams({
            cid: b.clusterId,
            bid: b.bucketId,
            from: from.toISOString(),
            to: to.toISOString(),
          });
          const res = await fetch("/api/v1/admin/usage/series?" + params.toString(), {
            credentials: "include",
          });
          if (!res.ok) continue;
          const body = (await res.json()) as { snapshots?: UsageSeriesPoint[] };
          const pts = body.snapshots ?? [];
          if (pts.length < 2) continue;
          const first = pts[0]!.bytes;
          const last = pts[pts.length - 1]!.bytes;
          const pct = first <= 0 ? (last > 0 ? Number.POSITIVE_INFINITY : 0) : ((last - first) / first) * 100;
          updates.push([k, pct]);
        } catch {
          // Network errors don't block the page — the bucket simply
          // shows "—" in the growth columns.
        }
      }
      if (!cancelled && updates.length > 0) {
        setRegistry((prev) => {
          const next = new Map(prev);
          for (const [k, v] of updates) next.set(k, v);
          return next;
        });
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, rangeDays]);

  return registry;
}

function RangeSelector({
  value,
  onChange,
}: {
  value: number;
  onChange: React.Dispatch<React.SetStateAction<number>>;
}) {
  return (
    <div className="inline-flex items-center rounded-md border bg-background overflow-hidden">
      {RANGE_OPTIONS.map((opt) => (
        <button
          key={opt.days}
          type="button"
          onClick={() => onChange(opt.days)}
          className={
            "px-2.5 py-1 text-xs font-medium transition-colors " +
            (value === opt.days
              ? "bg-primary text-primary-foreground"
              : "hover:bg-muted")
          }
          title={`Show trend over the last ${opt.label}`}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}

function GrowthLabel({ growth }: { growth: number }) {
  if (!Number.isFinite(growth)) {
    return <span className="text-amber-600 dark:text-amber-500">new</span>;
  }
  const sign = growth > 0 ? "+" : "";
  const tone =
    growth < 0
      ? "text-emerald-600 dark:text-emerald-400"
      : growth > 50
        ? "text-destructive"
        : "text-muted-foreground";
  return <span className={tone}>{sign}{Math.round(growth)}%</span>;
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

function PerClusterRow({
  row,
  growthPercent,
}: {
  row: UsagePerCluster;
  growthPercent?: number;
}) {
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
      <TableCell className="text-right tabular-nums">
        {row.healthy && growthPercent !== undefined ? (
          <GrowthLabel growth={growthPercent} />
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
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
  trendMode,
  rangeDays,
}: {
  title: string;
  rows: UsageTopBucket[];
  metricLabel: string;
  metricFormat: (n: number) => string;
  trendMode: boolean;
  rangeDays: number;
}) {
  // Track which (clusterId|bucketId) row is currently expanded.
  // Single-row expansion at a time keeps the dashboard compact.
  const [expanded, setExpanded] = useState<string | null>(null);

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
                const key = row.clusterId + "|" + row.bucketId;
                return (
                  <TopBucketRow
                    key={key}
                    row={row}
                    metricLabel={metricLabel}
                    metricFormat={metricFormat}
                    trendMode={trendMode}
                    rangeDays={rangeDays}
                    isExpanded={expanded === key}
                    onToggle={() =>
                      setExpanded((curr) => (curr === key ? null : key))
                    }
                  />
                );
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

function TopBucketRow({
  row,
  metricLabel,
  metricFormat,
  trendMode,
  rangeDays,
  isExpanded,
  onToggle,
}: {
  row: UsageTopBucket;
  metricLabel: string;
  metricFormat: (n: number) => string;
  trendMode: boolean;
  rangeDays: number;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const navigate = useNavigate();
  const display =
    metricLabel === "Bytes" ? metricFormat(row.bytes) : metricFormat(row.objects);

  const onClick = () => {
    if (trendMode) {
      onToggle();
    } else {
      navigate({
        to: "/admin/clusters/$cid/buckets/$id",
        params: { cid: row.clusterId, id: row.bucketId },
      });
    }
  };

  return (
    <>
      <TableRow
        className="cursor-pointer hover:bg-muted/50"
        onClick={onClick}
        title={
          trendMode
            ? "Click to expand/collapse the inline trend chart"
            : "Open bucket detail"
        }
      >
        <TableCell className="font-medium">
          {row.bucketAlias || row.bucketId.slice(0, 8)}
        </TableCell>
        <TableCell className="text-muted-foreground">{row.clusterLabel}</TableCell>
        <TableCell className="text-right tabular-nums">{display}</TableCell>
      </TableRow>
      {trendMode && isExpanded && (
        <TableRow className="bg-muted/20">
          <TableCell colSpan={3} className="p-3">
            <TrendChart
              cid={row.clusterId}
              bid={row.bucketId}
              metricLabel={metricLabel}
              rangeDays={rangeDays}
            />
          </TableCell>
        </TableRow>
      )}
    </>
  );
}

// TrendChart fetches the /admin/usage/series payload for one bucket
// and renders a tiny inline SVG line chart. No external dep — the
// data set is ~168 points (7d hourly) so a single-pass polyline is
// plenty.
//
// X-axis is time, Y-axis is the metric (bytes or objects). The line
// is normalised to fit the SVG viewBox; min/max labels on the right
// hand side give the operator the actual scale.
function TrendChart({
  cid,
  bid,
  metricLabel,
  rangeDays,
}: {
  cid: string;
  bid: string;
  metricLabel: string;
  rangeDays: number;
}) {
  const { data, isLoading, error } = useUsageSeries(cid, bid, { rangeDays });

  if (isLoading) {
    return (
      <div className="text-xs text-muted-foreground text-center py-4">
        Loading trend…
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-xs text-destructive text-center py-4">
        Failed to load trend: {(error as Error).message}
      </div>
    );
  }

  if (!data || data.snapshots.length === 0) {
    return (
      <div className="text-xs text-muted-foreground text-center py-4">
        Recording started — check back in an hour for the first data point.
      </div>
    );
  }

  return <Sparkline points={data.snapshots} metricLabel={metricLabel} />;
}

// Sparkline draws a single-line chart inside a 600x80 SVG. Inputs
// are already sorted oldest-first by the backend. Zero-variance
// series flatten to a horizontal line at vertical centre.
function Sparkline({
  points,
  metricLabel,
}: {
  points: UsageSeriesPoint[];
  metricLabel: string;
}) {
  const width = 600;
  const height = 80;
  const padX = 4;
  const padY = 6;

  const values = points.map((p) =>
    metricLabel === "Bytes" ? p.bytes : p.objects,
  );
  const min = Math.min(...values);
  const max = Math.max(...values);
  const range = max - min || 1; // avoid /0 on flat series

  const innerW = width - padX * 2;
  const innerH = height - padY * 2;

  const xs = (i: number) =>
    points.length === 1 ? padX + innerW / 2 : padX + (i / (points.length - 1)) * innerW;
  const ys = (v: number) => padY + innerH - ((v - min) / range) * innerH;

  const path = values.map((v, i) => (i === 0 ? "M" : "L") + xs(i).toFixed(1) + "," + ys(v).toFixed(1)).join(" ");

  const fmt = (n: number) =>
    metricLabel === "Bytes" ? humanizeBytes(n) : n.toLocaleString();

  const first = points[0]!;
  const last = points[points.length - 1]!;
  const span =
    points.length > 1
      ? "Last " + describeSpan(first.time, last.time) + ", " + points.length + " samples"
      : "1 sample";

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>{span}</span>
        <span className="tabular-nums">
          min {fmt(min)} &middot; max {fmt(max)}
        </span>
      </div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="w-full h-20 rounded border bg-background"
        preserveAspectRatio="none"
        aria-label={`${metricLabel} trend over time`}
      >
        <path
          d={path}
          fill="none"
          stroke="currentColor"
          strokeWidth={1.5}
          className="text-primary"
        />
        {points.length === 1 && (
          <circle
            cx={xs(0)}
            cy={ys(values[0]!)}
            r={2}
            className="fill-primary"
          />
        )}
      </svg>
    </div>
  );
}

// describeSpan turns the (first, last) ISO timestamps into a
// human-friendly window label without depending on date-fns. Coarse
// — "Nd" if >24h, "Nh" otherwise, "Nm" if <1h. The chart envelope
// already says "7 days" in the backend `range` field; this is just
// the human read-out for the actual seeded data.
function describeSpan(firstISO: string, lastISO: string): string {
  const ms = new Date(lastISO).getTime() - new Date(firstISO).getTime();
  if (ms <= 0) return "0m";
  const mins = Math.round(ms / 60_000);
  if (mins >= 24 * 60) return Math.round(mins / 60 / 24) + "d";
  if (mins >= 60) return Math.round(mins / 60) + "h";
  return mins + "m";
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
