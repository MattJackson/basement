import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
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

export const Route = createFileRoute("/admin/clusters/")({
  component: adminPage(ClustersScreen),
});

function ClustersScreen() {
  const navigate = useNavigate();
  const { data: clustersData, isLoading, error } = useListClusters();
  const deleteCluster = useDeleteCluster();

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

  const handleDeleteConfirm = () => {
    if (!clusterToDelete) return;
    deleteCluster.mutate(clusterToDelete.id);
    setDeleteDialogOpen(false);
    setClusterToDelete(null);
  };


const header = (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Clusters</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Garage and S3-compatible storage backends.
        </p>
      </div>
      <Button onClick={() => navigate({ to: "/admin/clusters/new" })}>
        + Add cluster
      </Button>
    </header>
  );

if (error) {
    return (
      <div className="space-y-6">
        {header}
        <ErrorBanner message="Couldn&apos;t connect to backend. Retrying automatically..." />
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
                <TableHead className="w-12"></TableHead>
                <TableHead>Label</TableHead>
                <TableHead>Driver</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Resources</TableHead>
                <TableHead className="w-16">Actions</TableHead>
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
            title="Welcome to basement"
            description="One pane of glass for Garage, MinIO, OpenMaxIO, and AWS S3. Add your first cluster to get started — basement talks to its admin API and surfaces buckets, keys, and layout from here."
            action={
              <Button onClick={() => navigate({ to: "/admin/clusters/new" })} size="lg">
                + Add your first cluster
              </Button>
            }
          />
        </div>
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-12"></TableHead>
                <TableHead>Label</TableHead>
                <TableHead>Driver</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Resources</TableHead>
                <TableHead className="w-16">Actions</TableHead>
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
  const { data: buckets, isLoading: loadingB } = useClusterBuckets(cid);
  const { data: keys, isLoading: loadingK } = useClusterKeys(cid);
  if (loadingB || loadingK) return <span className="text-xs text-muted-foreground">…</span>;
  const b = buckets?.length ?? 0;
  const k = keys?.length ?? 0;
  return (
    <span className="text-xs text-muted-foreground">
      <strong className="text-foreground tabular-nums">{b}</strong> buckets · <strong className="text-foreground tabular-nums">{k}</strong> keys
    </span>
  );
}

interface ClusterRowProps {
  cluster: components["schemas"]["Connection"];
  onEdit: () => void;
  onDelete: () => void;
}

function ClusterRow({ cluster, onEdit, onDelete }: ClusterRowProps) {
  const navigate = useNavigate();
  // Manual test only — Garage /v1/health is 10-20s round-trip, so we
  // never auto-poll. User clicks "Test" on the detail page when they
  // want a fresh status.
  const { data: testResult, isFetching, refetch } = useTestClusterQuery(cluster.id);
  const status = testResult ? getStatusFromResult(testResult) : "unknown";

  return (
    <TableRow
      className="cursor-pointer hover:bg-muted/50"
      onClick={() => navigate({ to: "/admin/clusters/$cid", params: { cid: cluster.id } })}
    >
      <TableCell>{colorDot(cluster.color)}</TableCell>
      <TableCell className="font-medium">{cluster.label}</TableCell>
      <TableCell><DriverBadge driver={cluster.driver} /></TableCell>
      <TableCell>
        {isFetching ? (
          <span className="text-xs text-muted-foreground">Testing…</span>
        ) : !testResult ? (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-xs"
            onClick={(e) => {
              e.stopPropagation();
              refetch();
            }}
          >
            Test
          </Button>
        ) : status === "unavailable" && testResult?.message ? (
          <TooltipWrapper message={testResult.message}>
            <span className="text-xs text-destructive">Unavailable</span>
          </TooltipWrapper>
        ) : (
          <HealthPill status={status as "healthy" | "degraded" | "unavailable"} />
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
              Edit
            </DropdownMenuItem>
            <DropdownMenuItem 
              variant="destructive" 
              onClick={(e) => { e.stopPropagation(); onDelete(); }}
            >
              Delete
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

function HealthPill({ status }: { status: "healthy" | "degraded" | "unavailable" }) {
  const variants = {
    healthy: "bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20",
    degraded: "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400 border-yellow-500/20",
    unavailable: "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/20",
  } as const;

  const labels = {
    healthy: "Healthy",
    degraded: "Degraded",
    unavailable: "Unavailable",
  } as const;

  return (
    <span className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold ${variants[status]}`}>
      {labels[status]}
    </span>
  );
}
