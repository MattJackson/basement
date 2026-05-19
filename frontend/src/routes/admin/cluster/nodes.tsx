import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes } from "@/shared/lib/format";
import { useNodes } from "@/shared/api/queries";

export const Route = createFileRoute("/admin/cluster/nodes")({
  component: NodesScreen,
});

function NodesScreen() {
  const queryClient = useQueryClient();
  const navigate = useNavigate({ from: "/admin/cluster/nodes" });
  const { data: nodes, isLoading, error } = useNodes();

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "nodes"] });
  };

  if (error) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <h1 className="text-3xl font-bold tracking-tight">Nodes</h1>
          <button
            onClick={handleRefresh}
            className="px-4 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
          >
            Refresh
          </button>
        </div>
        <ErrorBanner message="Couldn't connect to cluster. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold tracking-tight">Nodes</h1>
        <button
          onClick={handleRefresh}
          className="px-4 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
        >
          Refresh
        </button>
      </div>

      {isLoading ? (
        <div className="rounded-lg border bg-card">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Hostname</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Zone</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Capacity</TableHead>
                <TableHead>Tags</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(5)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-48" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell><Skeleton className="h-6 w-24 rounded-full" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  <TableCell><Skeleton className="h-3 w-3 rounded-full" /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : nodes && nodes.length === 0 ? (
        <EmptyState
          icon="server"
          title="No nodes configured"
          description="Configure your cluster layout to add storage and gateway nodes."
          action={
            <button
              onClick={() => navigate({ to: "/admin/cluster/layout" })}
              className="px-4 py-2 text-sm font-medium rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors"
            >
              Go to Layout Editor
            </button>
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Hostname</TableHead>
                <TableHead>Address</TableHead>
                <TableHead>Zone</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Capacity</TableHead>
                <TableHead>Tags</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {nodes && nodes.map((node) => (
                <TableRow key={node.id}>
                  <TableCell className="font-mono text-sm">
                    {node.id.slice(0, 8)}
                  </TableCell>
                  <TableCell>-</TableCell>
                  <TableCell>{node.address ?? "-"}</TableCell>
                  <TableCell>{node.zone ?? "-"}</TableCell>
                  <TableCell>
                    {node.role ? (
                      <Badge variant="secondary" className="capitalize">
                        {node.role}
                      </Badge>
                    ) : (
                      <span className="opacity-60">Unassigned</span>
                    )}
                  </TableCell>
                  <TableCell>
                    {node.capacity ? humanizeBytes(node.capacity) : "-"}
                  </TableCell>
                  <TableCell>
                    {node.tags && Object.keys(node.tags).length > 0 ? (
                      <div className="flex flex-wrap gap-1">
                        {Object.entries(node.tags).map(([key, value]) => (
                          <span
                            key={key}
                            className="px-2 py-0.5 bg-muted rounded text-xs font-mono"
                          >
                            {key}={value}
                          </span>
                        ))}
                      </div>
                    ) : (
                      "-"
                    )}
                  </TableCell>
                  <TableCell>
                    {node.address ? (
                      <span className="flex items-center gap-1.5">
                        <span className="h-2 w-2 rounded-full bg-green-500" />
                        <span className="text-sm opacity-80">Connected</span>
                      </span>
                    ) : (
                      <span className="flex items-center gap-1.5">
                        <span className="h-2 w-2 rounded-full bg-red-500" />
                        <span className="text-sm opacity-60">Unreachable</span>
                      </span>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}
