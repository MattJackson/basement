// v1.4.0c SCRUB.MAINT — /admin/clusters/{cid}/scrub
//
// Block-scrub maintenance surface for Garage. Drivers that don't
// support scrub (AWS S3, MinIO) render an explanation card with the
// reason from the capability flag. The supported branch shows the
// live state (running/idle, progress %, blocks scanned/corrupt, last
// completion timestamp) and a Run button that POSTs to the kick
// endpoint.
//
// While Running=true the page polls every 5 seconds (handled in the
// useClusterScrub hook) so the operator sees progress tick without
// manual refresh.

import { createFileRoute, Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Badge } from "@/components/ui/badge";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import {
  useGetCluster,
  useClusterScrub,
  useStartClusterScrub,
} from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/clusters/$cid/scrub")({
  component: adminPage(ClusterScrubScreen),
});

function ClusterScrubScreen() {
  const { cid } = Route.useParams();
  const { data: cluster } = useGetCluster(cid);
  const { data, isLoading, error } = useClusterScrub(cid);
  const startScrub = useStartClusterScrub(cid);
  const queryClient = useQueryClient();

  const handleRun = async () => {
    try {
      await startScrub.mutateAsync();
      toast.success("Scrub started");
      // Force an immediate refetch so the page flips into the running
      // state without waiting for the 5s poll boundary.
      await queryClient.invalidateQueries({
        queryKey: ["admin", "clusters", cid, "scrub"],
      });
    } catch (e) {
      toast.error(`Couldn't start scrub: ${(e as Error).message}`);
    }
  };

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink cid={cid} clusterLabel={cluster?.label} />
        <ErrorBanner message="Couldn't load scrub status." />
      </div>
    );
  }

  if (isLoading || !data) {
    return (
      <div className="space-y-6">
        <BackLink cid={cid} clusterLabel={cluster?.label} />
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-40 w-full rounded-lg" />
      </div>
    );
  }

  const { capabilities, state } = data;

  return (
    <div className="space-y-6">
      <BackLink cid={cid} clusterLabel={cluster?.label} />

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
            Block scrub
            {cluster?.label ? (
              <span className="text-muted-foreground font-normal"> · {cluster.label}</span>
            ) : null}
          </h1>
          <p className="text-sm text-muted-foreground mt-1 max-w-3xl">
            Walks every block in the cluster and verifies its on-disk
            checksum against the recorded value. Repairs blocks that
            diverge from quorum. Safe to run while the cluster is
            serving traffic.
          </p>
        </div>
      </header>

      {!capabilities.supported ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Not supported</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-sm text-muted-foreground max-w-prose">
              {capabilities.reason ||
                "This driver doesn't expose a block-scrub operation."}
            </p>
          </CardContent>
        </Card>
      ) : (
        <SupportedView
          state={state}
          onRun={handleRun}
          isStarting={startScrub.isPending}
        />
      )}
    </div>
  );
}

function SupportedView({
  state,
  onRun,
  isStarting,
}: {
  state: ReturnType<typeof useClusterScrub>["data"] extends infer R
    ? R extends { state: infer S }
      ? S
      : never
    : never;
  onRun: () => void;
  isStarting: boolean;
}) {
  // Narrowed view — state is always defined when we reach here.
  const s = state as {
    running: boolean;
    lastCompleted?: string;
    blocksScanned?: number;
    blocksCorrupt?: number;
    progressPercent?: number;
    message?: string;
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <div className="flex items-center gap-2">
          <CardTitle className="text-base">Status</CardTitle>
          {s.running ? (
            <Badge className="bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20">
              Running
            </Badge>
          ) : (
            <Badge variant="secondary">Idle</Badge>
          )}
        </div>
        <Button
          onClick={onRun}
          disabled={s.running || isStarting}
          size="sm"
          title={
            s.running
              ? "A scrub is already running on this cluster"
              : "Kick off a block scrub"
          }
        >
          {isStarting ? "Starting…" : s.running ? "Running…" : "Run scrub"}
        </Button>
      </CardHeader>
      <CardContent className="space-y-4">
        {s.running && (s.progressPercent ?? 0) > 0 && (
          <div className="space-y-1">
            <div className="text-xs text-muted-foreground tabular-nums">
              {s.progressPercent}% complete
            </div>
            <div className="h-2 w-full rounded-full bg-muted overflow-hidden">
              <div
                className="h-full bg-primary transition-all"
                style={{ width: `${Math.min(100, Math.max(0, s.progressPercent ?? 0))}%` }}
              />
            </div>
          </div>
        )}

        <dl className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-sm">
          <Stat label="Blocks scanned" value={(s.blocksScanned ?? 0).toLocaleString()} />
          <Stat
            label="Corrupt blocks"
            value={(s.blocksCorrupt ?? 0).toLocaleString()}
            tone={(s.blocksCorrupt ?? 0) > 0 ? "warn" : "default"}
          />
          <Stat
            label="Progress"
            value={s.progressPercent ? `${s.progressPercent}%` : "—"}
          />
          <Stat
            label="Last completed"
            value={s.lastCompleted ? humanizeTime(s.lastCompleted) : "Never"}
          />
        </dl>

        {s.message && (
          <p className="text-xs text-muted-foreground border-l-2 border-muted pl-3 italic">
            {s.message}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function Stat({
  label,
  value,
  tone = "default",
}: {
  label: string;
  value: string;
  tone?: "default" | "warn";
}) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wider text-muted-foreground">
        {label}
      </dt>
      <dd
        className={
          "mt-1 text-base font-semibold tabular-nums " +
          (tone === "warn" ? "text-amber-600 dark:text-amber-500" : "")
        }
      >
        {value}
      </dd>
    </div>
  );
}

function BackLink({ cid, clusterLabel }: { cid: string; clusterLabel?: string }) {
  return (
    <Link
      to="/admin/clusters/$cid"
      params={{ cid }}
      className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
    >
      ← {clusterLabel ?? "Back to cluster"}
    </Link>
  );
}
