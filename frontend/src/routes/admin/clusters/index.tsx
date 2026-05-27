import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import type { components } from "@/shared/api/types.gen";
import { adminPage } from "@/shared/layout/adminPage";
import { useListClusters, useTestClusterQuery, useClusterBuckets, useClusterKeys } from "@/shared/api/queries";
import { useDeleteCluster } from "@/shared/api/mutations";
import { DeleteClusterConfirm } from "@/shared/ui/DeleteClusterConfirm";
import { DriverBadge } from "@/components/clusters/DriverBadge";
import { useElevationGuard } from "@/shared/auth/elevation";

export const Route = createFileRoute("/admin/clusters/")({
  component: adminPage(ClustersScreen),
});

function ClustersScreen() {
  const navigate = useNavigate();
  // v2.0.0-beta.4: useTranslation returns {t, i18n, ready} — to alias
  // 't' to 'tp' you destructure-rename, not just expect a 'tp' field.
  // The freshman's original `const { tp } = useTranslation("pages")`
  // got `tp = undefined`.
  const { t: tp } = useTranslation("pages");
  const { data: clustersData, isLoading, error } = useListClusters();
  const deleteCluster = useDeleteCluster();
  // ADR-0003 v1.2.0b: cluster:delete is an ELEVATED-min capability,
  // so a USER/ADMIN session 403s with ELEVATION_REQUIRED on first
  // try. Wrapping the click handler in runWithElevation pops the
  // password modal and retries automatically on success, giving the
  // operator a one-click experience instead of a 403 → modal → re-click.
  const runWithElevation = useElevationGuard();

  // Delete dialog state (Create + Edit now navigate to dedicated
  // routes — popups-max-2-fields rule from v0.8.0d.15).
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [clusterToDelete, setClusterToDelete] = useState<{ id: string; label: string } | null>(null);

  const clusters = clustersData ?? [];

  const handleEditClick = (cluster: components["schemas"]["Connection"]) => {
    navigate({ to: "/admin/clusters/$cid/edit", params: { cid: cluster.id } });
  };

  const handleDeleteClick = (id: string, label: string) => {
    setClusterToDelete({ id, label });
    setDeleteDialogOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!clusterToDelete) return;
    const target = clusterToDelete;
    setDeleteDialogOpen(false);
    setClusterToDelete(null);
    try {
      await runWithElevation(() => deleteCluster.mutateAsync(target.id));
    } catch {
      // Errors (including ELEVATION_CANCELLED) surface via the
      // mutation's existing error state / banner; nothing to do here.
    }
  };


const header = (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{tp("adminClustersList.pageTitle")}</h1>
        <p className="text-sm text-muted-foreground mt-1">
          {tp("adminClustersList.pageSubtitle")}
        </p>
      </div>
      <Button onClick={() => navigate({ to: "/admin/clusters/new" })}>
        {tp("adminClustersList.addClusterButton")}
      </Button>
    </header>
  );

if (error) {
    return (
      <div className="space-y-6">
        {header}
        <ErrorBanner message={tp("errors.connectionFailed")} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {header}

      {isLoading ? (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-12">
                  <span className="sr-only">Actions</span>
                </TableHead>
                <TableHead>{tp("adminClustersList.addColumnLabel")}</TableHead>
                <TableHead>{tp("adminClustersList.addColumnDriver")}</TableHead>
                <TableHead>{tp("adminClustersList.addColumnStatus")}</TableHead>
                <TableHead>{tp("adminClustersList.addColumnResources")}</TableHead>
                <TableHead className="w-16">{tp("adminClustersList.addColumnActions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(5)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-3 w-3 rounded-full" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell><Skeleton className="h-6 w-24" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                  <TableCell><Skeleton className="h-8 w-8 rounded" /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : clusters.length === 0 ? (
        <div className="rounded-lg border bg-card p-8 sm:p-12">
          <EmptyState
            icon="server"
            title={tp("adminClustersList.emptyStateTitle")}
            description={tp("adminClustersList.emptyStateDescription")}
            action={
              <Button onClick={() => navigate({ to: "/admin/clusters/new" })} size="lg">
                {tp("adminClustersList.firstClusterButton")}
              </Button>
            }
          />
        </div>
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-12">
                  <span className="sr-only">Actions</span>
                </TableHead>
                <TableHead>{tp("adminClustersList.addColumnLabel")}</TableHead>
                <TableHead>{tp("adminClustersList.addColumnDriver")}</TableHead>
                <TableHead>{tp("adminClustersList.addColumnStatus")}</TableHead>
                <TableHead>{tp("adminClustersList.addColumnResources")}</TableHead>
                <TableHead className="w-16">{tp("adminClustersList.addColumnActions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {clusters.map((cluster) => (
                <ClusterRow
                  key={cluster.id}
                  cluster={cluster}
                  onEdit={() => handleEditClick(cluster)}
                  onDelete={() => handleDeleteClick(cluster.id, cluster.label)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <DeleteClusterConfirm
        open={deleteDialogOpen}
        clusterLabel={clusterToDelete?.label ?? ""}
        isDeleting={deleteCluster.isPending}
        onConfirm={handleDeleteConfirm}
        onCancel={() => { setDeleteDialogOpen(false); setClusterToDelete(null); }}
      />
    </div>
  );
}

function colorDot(color?: string) {
  return (
    <span
      className="inline-block h-3 w-3 rounded-full border border-border"
      style={{ backgroundColor: color ?? "#C9874B" }}
      aria-hidden
    />
  );
}

function getStatusFromResult(result: components["schemas"]["ConnectionTestResult"] | undefined): "healthy" | "degraded" | "unavailable" {
  if (!result) return "unavailable";
  if (result.ok) return "healthy";
  return "unavailable";
}

/** Bucket + key counts for the clusters list row. Two queries per
 *  row but they hit /admin/clusters/{cid}/{buckets,keys} which are
 *  cached for 30s — and on a single-row deployment we're talking
 *  one extra fetch per resource type. */
function ClusterCounts({ cid }: { cid: string }) {
  const { t: tp } = useTranslation("pages");
  const { data: buckets, isLoading: loadingB } = useClusterBuckets(cid);
  const { data: keys, isLoading: loadingK } = useClusterKeys(cid);
  if (loadingB || loadingK) return <span className="text-xs text-muted-foreground">…</span>;
  const b = buckets?.length ?? 0;
  const k = keys?.length ?? 0;
  return (
    <span className="text-xs text-muted-foreground">
      <strong className="text-foreground tabular-nums">{b}</strong> {tp("adminClustersList.bucketCount")} · <strong className="text-foreground tabular-nums">{k}</strong> {tp("adminClustersList.keyCount")}
    </span>
  );
}

interface ClusterRowProps {
  cluster: components["schemas"]["Connection"];
  onEdit: () => void;
  onDelete: () => void;
}

function ClusterRow({ cluster, onEdit, onDelete }: ClusterRowProps) {
  const { t: tp } = useTranslation("pages");
  const navigate = useNavigate();
  // Manual test only — Garage /v1/health is 10-20s round-trip, so we
  // never auto-poll. User clicks "Test" on the detail page when they
  // want a fresh status.
  // Auto-test on mount + 60s staleTime — operator wants status
  // visible without clicking. React Query shares cache with the
  // detail page so visiting both doesn't double-fire.
  const { data: testResult, isFetching } = useTestClusterQuery(cluster.id, { auto: true });
  const status: "healthy" | "degraded" | "unavailable" | "checking" =
    testResult ? getStatusFromResult(testResult)
    : isFetching ? "checking"
    : "unavailable";

  return (
    <TableRow
      className="cursor-pointer hover:bg-muted/50"
      onClick={() => navigate({ to: "/admin/clusters/$cid", params: { cid: cluster.id } })}
    >
      <TableCell>{colorDot(cluster.color)}</TableCell>
      <TableCell className="font-medium">{cluster.label}</TableCell>
      <TableCell><DriverBadge driver={cluster.driver} /></TableCell>
      <TableCell>
        {status === "unavailable" && testResult?.message ? (
          <TooltipWrapper message={testResult.message}>
            <HealthPill status="unavailable" />
          </TooltipWrapper>
        ) : (
          <HealthPill status={status} />
        )}
      </TableCell>
      <TableCell>
        <ClusterCounts cid={cluster.id} />
      </TableCell>
      <TableCell>
        <DropdownMenu>
          <DropdownMenuTrigger onClick={(e) => e.stopPropagation()}>
            <Button variant="ghost" className="h-8 w-8 p-0">
              <span className="sr-only">Open menu</span>
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="h-4 w-4"
              >
                <circle cx="12" cy="12" r="1" />
                <circle cx="19" cy="12" r="1" />
                <circle cx="5" cy="12" r="1" />
              </svg>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={(e) => { e.stopPropagation(); onEdit(); }}>
              {tp("buttons.edit")}
            </DropdownMenuItem>
            <DropdownMenuItem 
              variant="destructive" 
              onClick={(e) => { e.stopPropagation(); onDelete(); }}
            >
              {tp("buttons.delete")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

function TooltipWrapper({ message, children }: { message: string; children: React.ReactNode }) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>{children}</TooltipTrigger>
        <TooltipContent>
          <p>{message}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}

function HealthPill({ status }: { status: "healthy" | "degraded" | "unavailable" | "checking" }) {
  const { t: tp } = useTranslation("pages");
  const variants = {
    healthy: "bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20",
    degraded: "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400 border-yellow-500/20",
    unavailable: "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/20",
    checking: "bg-muted/50 text-muted-foreground border-border",
  } as const;

  const labels = {
    healthy: tp("adminClustersList.healthy"),
    degraded: tp("adminClustersList.degraded"),
    unavailable: tp("adminClustersList.unavailable"),
    checking: tp("adminClustersList.checking"),
  } as const;

  return (
    <span className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold ${variants[status]}`}>
      {labels[status]}
    </span>
  );
}
