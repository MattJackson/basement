import { createFileRoute } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { HealthPill } from "@/shared/ui/HealthPill";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes } from "@/shared/lib/format";
import { useNodes, useLayout, useCapabilities } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/cluster/")({
  component: adminPage(ClusterOverview),
});

/**
 * ClusterOverview is the secondary "ops" surface — previously the
 * `/admin` Dashboard. Now lives at `/admin/cluster` (reachable via the
 * admin menu), keeping `/admin` reserved for the primary files-first
 * bucket list.
 */
function ClusterOverview() {
  const queryClient = useQueryClient();
  const { data: nodes, isLoading: loadingNodes, error: nodesError } = useNodes();
  const { data: layout, isLoading: loadingLayout } = useLayout();
  const capsResult = useCapabilities();

  const totalNodes = nodes?.length ?? 0;
  const healthyNodes = nodes?.filter(n => n.address).length ?? 0;
  const totalCapacity = nodes?.reduce((sum, node) => sum + (node.capacity ?? 0), 0) ?? 0;

  let healthStatus: "healthy" | "degraded" | "unavailable" = "unavailable";
  if (!loadingNodes && !loadingLayout) {
    if (totalNodes === 0) {
      healthStatus = "unavailable";
    } else if (healthyNodes === totalNodes) {
      healthStatus = "healthy";
    } else {
      healthStatus = "degraded";
    }
  }

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "nodes"] });
    queryClient.invalidateQueries({ queryKey: ["admin", "layout"] });
    queryClient.invalidateQueries({ queryKey: ["capabilities"] });
  };

  return (
    <div className="space-y-6">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Cluster</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Backend health, nodes, and capabilities.
          </p>
        </div>
        <Button variant="outline" onClick={handleRefresh}>Refresh</Button>
      </header>

      {nodesError ? (
        <ErrorBanner
          message={
            nodesError instanceof Error ? nodesError.message : String(nodesError)
          }
        />
      ) : loadingNodes || loadingLayout ? (
        <div className="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-4">
          {[...Array(4)].map((_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-4 w-32" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-8 w-full" />
              </CardContent>
            </Card>
          ))}
        </div>
      ) : totalNodes === 0 ? (
        <div className="rounded-lg border bg-muted/50 p-12 text-center">
          <p className="text-lg font-medium">Cluster has no nodes yet</p>
          <p className="mt-2 text-sm opacity-60">
            Configure your cluster layout to get started.
          </p>
        </div>
      ) : (
        <>
          <div className="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-4">
            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm font-medium opacity-60">Status</CardTitle>
              </CardHeader>
              <CardContent>
                <HealthPill status={healthStatus} />
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm font-medium opacity-60">Storage Nodes</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{healthyNodes}/{totalNodes}</p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm font-medium opacity-60">Layout Version</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{layout?.version ?? 0}</p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="pb-3">
                <CardTitle className="text-sm font-medium opacity-60">Total Capacity</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold">{humanizeBytes(totalCapacity)}</p>
              </CardContent>
            </Card>
          </div>

          <Card>
            <CardHeader>
              <CardTitle>Cluster Summary</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid sm:grid-cols-2 gap-4">
                <div>
                  <p className="text-sm opacity-60">Layout Version</p>
                  <p className="font-medium">{layout?.version}</p>
                </div>
                {capsResult.data && (
                  <div>
                    <p className="text-sm opacity-60">Driver</p>
                    <p className="font-medium">{capsResult.data.driver ?? "-"}</p>
                  </div>
                )}
              </div>

              {capsResult.data && (
                <div>
                  <p className="text-sm font-medium mb-2">Features</p>
                  <div className="flex flex-wrap gap-2">
                    {capsResult.data.presign && (
                      <span className="px-2 py-1 bg-muted rounded text-xs">presigned URLs</span>
                    )}
                    {capsResult.data.multipart && (
                      <span className="px-2 py-1 bg-muted rounded text-xs">multipart uploads</span>
                    )}
                    {capsResult.data.quotas && (
                      <span className="px-2 py-1 bg-muted rounded text-xs">bucket quotas</span>
                    )}
                    {capsResult.data.bucket_aliases && (
                      <span className="px-2 py-1 bg-muted rounded text-xs">bucket aliases</span>
                    )}
                  </div>
                </div>
              )}

              <div className="flex flex-wrap gap-2 pt-2">
                <Button variant="outline" render={<a href="/admin/cluster/nodes">View nodes</a>} />
                <Button variant="outline" render={<a href="/admin/cluster/layout">Edit layout</a>} />
              </div>

              {layout?.nodes.length ? (
                <div>
                  <p className="text-sm font-medium mb-2">Nodes</p>
                  <ul className="space-y-1 text-sm">
                    {layout.nodes.map((node) => (
                      <li key={node.id} className="flex justify-between opacity-80">
                        <span className="font-mono">{node.id.slice(0, 12)}</span>
                        <span className="opacity-60">{node.zone}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
