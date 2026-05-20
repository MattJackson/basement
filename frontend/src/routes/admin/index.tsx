import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DeleteBucketConfirm } from "@/shared/ui/DeleteBucketConfirm";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import { useBuckets, useListClusters } from "@/shared/api/queries";
import { useCreateBucket, useDeleteBucket } from "@/shared/api/mutations";
import { adminPage } from "@/shared/layout/adminPage";
import { ClusterBadge } from "@/components/ClusterBadge";
import { ClusterFilter } from "@/components/ClusterFilter";

const _createBucketSchema = z.object({
  alias: z.string().min(1, "Alias is required"),
});

type CreateBucketFormValues = z.infer<typeof _createBucketSchema>;

export const Route = createFileRoute("/admin/")({
  component: adminPage(MyBuckets),
});

function MyBuckets() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteDialogId, setDeleteDialogId] = useState<string | null>(null);
  const [clusterFilter, setClusterFilter] = useState<string | null>(null);
  const createMutation = useCreateBucket();
  const deleteMutation = useDeleteBucket();
  const { data: bucketsData, isLoading, error } = useBuckets();
  const { data: clusters, isLoading: clustersLoading } = useListClusters();

  // First-run gate: if the operator has no clusters configured (either
  // env auto-seed didn't fire OR they deleted them all), there's no
  // useful bucket list to show. Bounce to /admin/clusters which has
  // the "Welcome — add your first cluster" empty state.
  const noClusters = !clustersLoading && (clusters?.length ?? 0) === 0;
  if (noClusters) {
    navigate({ to: "/admin/clusters", replace: true });
  }

  const handleRefresh = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "buckets"] });
  };

  const handleCreateSubmit = async (values: CreateBucketFormValues) => {
    try {
      createMutation.mutate(
        { alias: values.alias },
        {
          onError: () => {
            // Error stays in dialog, don't close
          }
        }
      );
    } catch {
      // Handled by mutation error state
    }
  };

  const handleDeleteClick = (bucketId: string) => {
    setDeleteDialogId(bucketId);
  };

  const confirmDelete = () => {
    if (deleteDialogId) {
      deleteMutation.mutate(deleteDialogId);
      setDeleteDialogId(null);
    }
  };

  const buckets = bucketsData?.buckets ?? [];
  const errors = bucketsData?.errors ?? [];

  const filteredBuckets = buckets.filter((bucket) => {
    if (clusterFilter && bucket.connectionId !== clusterFilter) return false;
    if (!search) return true;
    const needle = search.toLowerCase();
    return (bucket.aliases ?? []).some((a: string) => a.toLowerCase().includes(needle));
  });

  const errorBanner = errors.length > 0 ? (
    <div className="rounded-lg border bg-destructive/10 p-4">
      <p className="text-sm text-destructive font-medium">
        Couldn&apos;t load buckets from {errors.length} cluster{errors.length > 1 ? "s" : ""}:{" "}
        {errors.map((e) => e.connectionId.slice(0, 8)).join(", ")}
      </p>
      <details className="mt-2 text-xs">
        <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
          View details
        </summary>
        <ul className="mt-2 space-y-1">
          {errors.map((e, idx) => (
            <li key={idx} className="text-destructive">
              {e.connectionId.slice(0, 8)}: {e.message}
            </li>
          ))}
        </ul>
      </details>
    </div>
  ) : null;

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader
          title="My Buckets"
          description="Storage buckets you own or have access to."
          actions={<Button variant="outline" onClick={handleRefresh}>Refresh</Button>}
        />
        {errorBanner}
        <ErrorBanner message="Couldn&apos;t connect to cluster. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="My Buckets"
        description="Storage buckets you own or have access to."
        actions={
          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Input
              placeholder="Search buckets..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="flex-1 sm:w-64"
            />
            <ClusterFilter selectedClusterId={clusterFilter} onFilterChange={setClusterFilter} />
            <Button 
              variant="outline" 
              onClick={() => setCreateDialogOpen(true)}
            >
              New
            </Button>
          </div>
        }
      />

      {errorBanner}

      {isLoading ? (
        <BucketListSkeleton />
      ) : filteredBuckets?.length === 0 ? (
        <EmptyState
          icon="database"
          title={search ? "No buckets match your search" : "No buckets yet"}
          description={
            search
              ? "Try a different search term."
              : "Create your first bucket to start storing files."
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-48">Cluster</TableHead>
                <TableHead>Bucket Name</TableHead>
                <TableHead>Aliases</TableHead>
                <TableHead>Created</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredBuckets?.map((bucket) => {
                const aliases = bucket.aliases ?? [];
                const [primary, ...rest] = aliases;
                const name = primary ?? bucket.id.slice(0, 8);

                return (
                  <TableRow
                    key={bucket.id}
                    className="cursor-pointer hover:bg-muted/50"
                    onClick={() => {
                      // PLANNED: /admin/clusters/{cid}/buckets/{id} route (CLUSTER.RESOURCE-DETAIL)
                      // @ts-expect-error route not yet defined
                      navigate({ to: "/admin/clusters/$cid/buckets/$id", params: { cid: bucket.connectionId, id: bucket.id } });
                    }}
                  >
                    <TableCell>
                      <ClusterBadge connectionId={bucket.connectionId} />
                    </TableCell>
                    <TableCell className="font-medium">{name}</TableCell>
                    <TableCell>
                      {rest.length > 0 ? rest.join(", ") : "—"}
                    </TableCell>
                    <TableCell>{bucket.created ? humanizeTime(bucket.created) : "—"}</TableCell>
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
                          <DropdownMenuItem
                             onClick={(e) => {
                                e.stopPropagation();
                                // PLANNED: /admin/clusters/{cid}/buckets/{id} route (CLUSTER.RESOURCE-DETAIL)
                                // @ts-expect-error route not yet defined
                                navigate({ to: "/admin/clusters/$cid/buckets/$id", params: { cid: bucket.connectionId, id: bucket.id } });
                              }}
                          >
                            View
                          </DropdownMenuItem>
                          <DropdownMenuItem 
                            variant="destructive" 
                            onClick={(e) => {
                              e.stopPropagation();
                              handleDeleteClick(bucket.id);
                            }}
                          >
                            Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      )}

      <Dialog open={createDialogOpen} onOpenChange={(open) => {
        if (!open) {
          setCreateDialogOpen(false);
          createMutation.reset();
        }
      }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Create new bucket</DialogTitle>
            <DialogDescription>
              Enter a unique alias for your new bucket. The alias must be
              lowercase, 3-63 characters, and contain only letters, numbers,
              and hyphens.
            </DialogDescription>
          </DialogHeader>

          {createMutation.isError && (
            <div className="text-sm text-destructive">
              {String(createMutation.error?.message ?? "Failed to create bucket")}
            </div>
          )}

          <form onSubmit={(e) => {
            e.preventDefault();
            const formData = new FormData(e.currentTarget);
            const alias = String(formData.get("alias") || "");
            handleCreateSubmit({ alias });
          }}>
            <div className="grid gap-4 py-4">
              <div className="grid gap-2">
                <label htmlFor="alias" className="text-sm font-medium">
                  Bucket alias
                </label>
                <Input
                  id="alias"
                  name="alias"
                  placeholder="my-bucket"
                  autoComplete="off"
                />
              </div>
            </div>

            <DialogFooter>
              <button 
                type="button"
                onClick={() => setCreateDialogOpen(false)}
                className="inline-flex items-center justify-center rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 border border-input bg-transparent hover:bg-accent hover:text-accent-foreground"
              >
                Cancel
              </button>
              <Button 
                type="submit" 
                disabled={createMutation.isPending}
              >
                {createMutation.isPending ? "Creating..." : "Create"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <DeleteBucketConfirm
        open={deleteDialogId !== null}
        bucketAlias={(() => {
          if (!deleteDialogId) return "";
          const bucket = buckets.find((b) => b.id === deleteDialogId);
          return bucket?.aliases?.[0] ?? deleteDialogId.slice(0, 12);
        })()}
        isDeleting={deleteMutation.isPending}
        onConfirm={confirmDelete}
        onCancel={() => setDeleteDialogId(null)}
      />
    </div>
  );
}

interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: React.ReactNode;
}

function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          {title}
        </h1>
        {description && (
          <p className="text-sm text-muted-foreground mt-1">{description}</p>
        )}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </header>
  );
}

function BucketListSkeleton() {
  return (
    <div className="rounded-lg border bg-card overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-48">Cluster</TableHead>
            <TableHead>Bucket Name</TableHead>
            <TableHead>Aliases</TableHead>
            <TableHead>Created</TableHead>
            <TableHead className="w-16">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {[...Array(5)].map((_, i) => (
            <TableRow key={i}>
              <TableCell><Skeleton className="h-4 w-32" /></TableCell>
              <TableCell><Skeleton className="h-4 w-48" /></TableCell>
              <TableCell><Skeleton className="h-4 w-32" /></TableCell>
              <TableCell><Skeleton className="h-4 w-32" /></TableCell>
              <TableCell><Skeleton className="h-8 w-8 rounded" /></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
