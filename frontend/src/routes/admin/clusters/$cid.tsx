import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { DeleteClusterConfirm } from "@/shared/ui/DeleteClusterConfirm";
import { DangerZone } from "@/shared/ui/DangerZone";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import { useGetCluster, useNodes, useCapabilities, useTestClusterQuery } from "@/shared/api/queries";
import { useDeleteCluster } from "@/shared/api/mutations";
import { adminPage } from "@/shared/layout/adminPage";
import type { components } from "@/shared/api/types.gen";
import { EditClusterDialog } from "@/components/clusters/EditClusterDialog";
import { DriverBadge } from "@/components/clusters/DriverBadge";

export const Route = createFileRoute("/admin/clusters/$cid")({
  component: adminPage(ClusterDetailScreen),
});

function ClusterDetailScreen() {
  const { cid } = Route.useParams();

  const { data: cluster, isLoading, error } = useGetCluster(cid);
  const { data: nodes } = useNodes(cid);
  const { data: capabilities } = useCapabilities();
  const deleteCluster = useDeleteCluster();

  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Test cluster state
  const testQuery = useTestClusterQuery(cid);
  const testResult = testQuery.data ?? null;

  const handleTestCluster = () => {
    // useTestClusterQuery polls automatically; manual trigger is just
    // a refetch.
    testQuery.refetch();
  };

  const getStatusFromResult = (result?: components["schemas"]["ConnectionTestResult"]) => {
    if (!result?.ok) return "unavailable";
    if (result.message?.toLowerCase().includes("degraded")) return "degraded";
    return "healthy";
  };

  const status = testResult ? getStatusFromResult(testResult) : "unavailable";

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink />
        <ErrorBanner message="Couldn&apos;t load cluster details." />
      </div>
    );
  }

  if (isLoading || !cluster) {
    return (
      <div className="space-y-6">
        <BackLink />
        <Skeleton className="h-8 w-48" />
        <Card>
          <CardContent className="pt-6">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              {[...Array(3)].map((_, i) => (
                <Skeleton key={i} className="h-20 w-full rounded-lg" />
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <BackLink />

      {/* Header */}
      <div className="space-y-2">
        <div className="flex items-center gap-3">
          {colorDot(cluster.color)}
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{cluster.label}</h1>
          <DriverBadge driver={cluster.driver} />
          <HealthPill status={status} message={testResult?.message} />
        </div>
      </div>

      {/* Stats card */}
      <Card>
        <CardContent className="pt-6">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div className="text-center p-4 rounded-lg bg-muted/50">
              <div className="text-xl font-semibold tabular-nums">—</div>
              <div className="text-xs text-muted-foreground mt-1">Buckets</div>
            </div>
            <div className="text-center p-4 rounded-lg bg-muted/50">
              <div className="text-xl font-semibold tabular-nums">—</div>
              <div className="text-xs text-muted-foreground mt-1">Keys</div>
            </div>
            <div className="text-center p-4 rounded-lg bg-muted/50">
              <div className="text-xl font-semibold tabular-nums">{nodes?.length ?? "—"}</div>
              <div className="text-xs text-muted-foreground mt-1">Nodes</div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Test Connection */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <span>Connection test</span>
          <Button 
            size="sm" 
            variant="outline"
            onClick={handleTestCluster}
            disabled={testQuery.isPending}
          >
            {testQuery.isPending ? "Testing…" : "Test connection"}
          </Button>
        </CardHeader>
        <CardContent className="pt-6">
          {testResult ? (
            <div className={`text-sm ${testResult.ok ? "text-green-600" : "text-destructive"}`}>
              {testResult.ok ? "✓ Connection successful" : `✗ ${testResult.message}`}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">Click "Test connection" to verify the cluster is reachable.</p>
          )}
        </CardContent>
      </Card>

      {/* Buckets section */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <span>Buckets</span>
          {/* Routes /admin/clusters/$cid/buckets|keys|layout land in
              CLUSTER.RESOURCE-DETAIL + CLUSTER.LAYOUT-EDITOR cycles.
              Until then this is a plain anchor that goes to the
              cross-cluster lists pre-filtered by cluster. */}
          <a
            href={`/admin?cluster=${cid}`}
            className="text-sm font-medium hover:underline text-muted-foreground"
          >
            View all →
          </a>
        </CardHeader>
        <CardContent className="pt-6">
          <EmptyState
            icon="database"
            title="No buckets yet"
            description="Buckets in this cluster will appear here."
          />
        </CardContent>
      </Card>

      {/* Keys section */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <span>Keys</span>
          <a
            href={`/admin/keys?cluster=${cid}`}
            className="text-sm font-medium hover:underline text-muted-foreground"
          >
            View all →
          </a>
        </CardHeader>
        <CardContent className="pt-6">
          <EmptyState
            icon="key"
            title="No keys yet"
            description="Access keys for this cluster will appear here."
          />
        </CardContent>
      </Card>

      {/* Nodes section - gated by capability */}
      {capabilities?.layout !== "readonly" && (
        <Card>
          <CardHeader>Nodes</CardHeader>
          <CardContent className="pt-6">
            {!nodes || nodes.length === 0 ? (
              <EmptyState
                icon="server"
                title="No nodes configured"
                description="Add nodes via the Layout editor."
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-1/4">ID</TableHead>
                    <TableHead>Hostname</TableHead>
                    <TableHead>Role</TableHead>
                    <TableHead>Zone</TableHead>
                    <TableHead>Status</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {nodes.map((node) => (
                    <TableRow key={node.id}>
                      <TableCell className="font-mono text-xs">{node.id.slice(0, 12)}</TableCell>
                      <TableCell>{node.hostname ?? "—"}</TableCell>
                      <TableCell>{node.role ?? "unassigned"}</TableCell>
                      <TableCell>{node.zone ?? "—"}</TableCell>
                      <TableCell><NodeStatus status={node.status} /></TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      )}

      {/* Layout section - gated by capability. Route under cluster
          scope lands with CLUSTER.LAYOUT-EDITOR; for now anchor. */}
      {capabilities?.layout !== "readonly" ? (
        <a href={`/admin/clusters/${cid}/layout`}>
          <Card className="cursor-pointer hover:bg-muted/50 transition-colors">
            <CardContent className="pt-6">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-medium">Layout</h3>
                  <p className="text-sm text-muted-foreground mt-1">
                    Edit cluster topology and node assignments.
                  </p>
                </div>
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  className="h-5 w-5 text-muted-foreground"
                >
                  <path d="m9 18 6-6-6-6" />
                </svg>
              </div>
            </CardContent>
          </Card>
        </a>
      ) : (
        <Card>
          <CardContent className="pt-6">
            <h3 className="font-medium mb-2">Layout</h3>
            <p className="text-sm text-muted-foreground">
              Layout management is not supported by this backend.
            </p>
          </CardContent>
        </Card>
      )}

      {/* Created */}
      {cluster.createdAt && humanizeTime(cluster.createdAt) !== "—" && (
        <p className="text-xs text-muted-foreground">Created {humanizeTime(cluster.createdAt)}</p>
      )}

      {/* Edit Dialog */}
      <EditClusterDialog
        open={editDialogOpen}
        onOpenChange={(open) => !open && setEditDialogOpen(false)}
        cluster={cluster}
      />

      {/* Danger Zone */}
      <DangerZone description="Deleting this cluster removes the connection configuration. All buckets and keys remain but become inaccessible. Cannot be undone.">
        <Button
          variant="destructive"
          onClick={() => setDeleteDialogOpen(true)}
          disabled={testQuery.isPending}
        >
          Delete cluster
        </Button>
      </DangerZone>

      <DeleteClusterConfirm
        open={deleteDialogOpen}
        clusterLabel={cluster.label}
        isDeleting={deleteCluster.isPending}
        onConfirm={() => {
          deleteCluster.mutate(cid);
          setDeleteDialogOpen(false);
        }}
        onCancel={() => setDeleteDialogOpen(false)}
      />
    </div>
  );
}

function BackLink() {
  return (
    <Link
      to="/admin/clusters"
      className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
    >
      ← Back to Clusters
    </Link>
  );
}

function colorDot(color?: string) {
  return (
    <span
      className="inline-block h-3 w-3 rounded-full"
      style={{ backgroundColor: color ?? "#C9874B" }}
      aria-label={`Cluster color`}
    />
  );
}

function HealthPill({ status, message }: { status: "healthy" | "degraded" | "unavailable"; message?: string }) {
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
    <TooltipWrapper message={message}>
      <Badge variant="outline" className={`${variants[status]} px-3 py-1`}>
        {labels[status]}
      </Badge>
    </TooltipWrapper>
  );
}

function TooltipWrapper({ message, children }: { message?: string; children: React.ReactNode }) {
  if (!message) return <>{children}</>;
  
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

function NodeStatus({ status }: { status?: string }) {
  const isLive = status === "connected";
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium ${isLive ? "bg-green-100 text-green-800" : "bg-red-100 text-red-800"}`}>
      {status ?? "unknown"}
    </span>
  );
}

