import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Card, CardContent } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DeleteBucketConfirm } from "@/shared/ui/DeleteBucketConfirm";
import { DangerZone } from "@/shared/ui/DangerZone";
import { PermissionChips } from "@/shared/ui/PermissionChips";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes, humanizeTime } from "@/shared/lib/format";
import { useBucket } from "@/shared/api/queries";
import { useUpdateBucket, useDeleteBucket } from "@/shared/api/mutations";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/clusters/$cid/buckets/$id")({
  component: adminPage(AdminBucketDetail),
});

function AdminBucketDetail() {
  const { cid, id } = Route.useParams();
  const updateMutation = useUpdateBucket();
  const deleteMutation = useDeleteBucket();
  const [isEditingAlias, setIsEditingAlias] = useState(false);
  const [aliasInput, setAliasInput] = useState("");
  const [quotaDialogOpen, setQuotaDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const { data: bucket, isLoading, error } = useBucket(cid, id);

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink />
        <ErrorBanner message="Couldn't load bucket details." />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
          <BackLink />
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-4 w-96" />
          <Card>
            <CardContent className="pt-6">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="text-center p-4 rounded-lg bg-muted/50">
                    <Skeleton className="h-6 w-24 mx-auto mb-1" />
                    <Skeleton className="h-3 w-16 mx-auto" />
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="space-y-3 max-w-md">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-3/4" />
              </div>
            </CardContent>
          </Card>
          <Card>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-1/3">Key ID</TableHead>
                  <TableHead className="w-1/3">Name</TableHead>
                  <TableHead className="w-1/3">Permissions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {[...Array(5)].map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-full" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
      </div>
    );
  }

  if (!bucket) {
    return (
      <div className="space-y-6">
        <BackLink />
        <EmptyState
          icon="database"
          title="Bucket not found"
          description="It may have been deleted."
        />
      </div>
    );
  }

  const handleAliasSave = () => {
    if (!aliasInput.trim()) return;
    
    const aliases = bucket.aliases ?? [];
    const newAliases = [aliasInput, ...aliases.filter((a) => a !== aliases[0])];
    updateMutation.mutate({
      cid,
      id: bucket.id,
      update: { aliases: newAliases },
    });
    setIsEditingAlias(false);
  };

  const handleCancelEdit = () => {
    setAliasInput(bucket.aliases?.[0] ?? "");
    setIsEditingAlias(false);
  };

  const handleQuotaSubmit = (maxSizeGB: number | null, maxObjects: number | null) => {
    const quotas = {
      max_size: maxSizeGB !== null ? Math.round(maxSizeGB * 1024 ** 3) : null,
      max_objects: maxObjects !== null ? maxObjects : null,
    };

    updateMutation.mutate({
      cid,
      id: bucket.id,
      update: { quotas },
    });
    setQuotaDialogOpen(false);
  };

  const handleDelete = () => {
    deleteMutation.mutate({ cid, id: bucket.id });
    setDeleteDialogOpen(false);
  };

  return (
    <div className="space-y-6">
          <BackLink />

          {/* Header */}
          <div className="space-y-2">
            {isEditingAlias ? (
              <div className="flex items-center gap-2">
                <Input
                  value={aliasInput}
                  onChange={(e) => setAliasInput(e.target.value)}
                  onBlur={handleAliasSave}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") handleAliasSave();
                    if (e.key === "Escape") handleCancelEdit();
                  }}
                  className="flex-1"
                  autoFocus
                />
                <Button 
                  size="sm" 
                  variant="outline"
                  onClick={handleAliasSave}
                >
                  Save
                </Button>
                <Button 
                  size="sm" 
                  variant="ghost"
                  onClick={handleCancelEdit}
                >
                  Cancel
                </Button>
              </div>
            ) : (
              <>
                <button
                  onClick={() => {
                    setAliasInput(bucket.aliases?.[0] ?? "");
                    setIsEditingAlias(true);
                  }}
                  className="text-2xl sm:text-3xl font-semibold tracking-tight hover:underline underline-offset-4 text-left"
                >
                  {bucket.aliases?.[0] ?? bucket.id.slice(0, 12)}
                </button>
                {bucket.aliases && bucket.aliases.length > 1 ? (
                  <div className="flex items-center gap-1.5 flex-wrap">
                    {bucket.aliases.slice(1).map((alias) => (
                      <Badge key={alias} variant="secondary" className="text-xs">
                        {alias}
                      </Badge>
                    ))}
                  </div>
                ) : null}
              </>
            )}
          </div>

          {/* Stats card */}
          <Card>
            <CardContent className="pt-6">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                <div className="text-center p-4 rounded-lg bg-muted/50">
                  <div className="text-xl font-semibold tabular-nums">
                    {humanizeBytes(bucket.bytes)}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">Size</div>
                </div>
                <div className="text-center p-4 rounded-lg bg-muted/50">
                  <div className="text-xl font-semibold tabular-nums">
                    {(bucket.objects ?? 0).toLocaleString()}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">Objects</div>
                </div>
                <div className="text-center p-4 rounded-lg bg-muted/50">
                  <div className="text-xl font-semibold tabular-nums">
                    {(bucket.unfinishedUploads ?? 0).toLocaleString()}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">Unfinished uploads</div>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Quotas — compact single row, no oversized card */}
          <div className="flex items-center justify-between gap-4 rounded-lg border bg-card px-4 py-3">
            <div className="flex flex-wrap items-baseline gap-x-6 gap-y-1 text-sm">
              <span className="text-muted-foreground">Quotas</span>
              <span>
                <span className="text-xs text-muted-foreground mr-1.5">Max size</span>
                <span className="font-medium tabular-nums">
                  {bucket.quotas?.maxSize != null
                    ? humanizeBytes(bucket.quotas.maxSize)
                    : "Unlimited"}
                </span>
              </span>
              <span>
                <span className="text-xs text-muted-foreground mr-1.5">Max objects</span>
                <span className="font-medium tabular-nums">
                  {bucket.quotas?.maxObjects != null
                    ? bucket.quotas.maxObjects.toLocaleString()
                    : "Unlimited"}
                </span>
              </span>
            </div>
            <Button
              size="sm"
              variant="outline"
              onClick={() => setQuotaDialogOpen(true)}
            >
              Edit
            </Button>
          </div>

           <Dialog open={quotaDialogOpen} onOpenChange={(open) => {
             if (!open) setQuotaDialogOpen(false);
           }}>
             <DialogContent className="sm:max-w-md">
               <DialogHeader>
                 <DialogTitle>Edit quotas</DialogTitle>
                 <DialogDescription>
                   Set limits for bucket size and object count. Leave empty for unlimited.
                 </DialogDescription>
               </DialogHeader>

               {updateMutation.isError && (
                 <div className="text-sm text-destructive">
                   {String(updateMutation.error?.message ?? "Failed to update quotas")}
                 </div>
               )}

               <form onSubmit={(e) => {
                 e.preventDefault();
                 const formData = new FormData(e.currentTarget);
                 const maxSizeGBStr = String(formData.get("maxSizeGB") || "");
                 const maxObjectsStr = String(formData.get("maxObjects") || "");
                 
                 const maxSizeGB = maxSizeGBStr === "" ? null : parseFloat(maxSizeGBStr) || null;
                 const maxObjects = maxObjectsStr === "" ? null : parseInt(maxObjectsStr, 10) || null;
                 
                 handleQuotaSubmit(maxSizeGB, maxObjects);
               }}>
                 <div className="grid gap-4 py-4">
                   <div className="grid gap-2">
                     <label htmlFor="maxSizeGB" className="text-sm font-medium">
                       Max size (GB)
                     </label>
                     <Input 
                       id="maxSizeGB" 
                       name="maxSizeGB" 
                       type="number" 
                       min="0"
                       step="0.1"
                       placeholder="Unlimited"
                       defaultValue={bucket.quotas?.maxSize != null ? bucket.quotas.maxSize / (1024 ** 3) : ""}
                     />
                   </div>
                   <div className="grid gap-2">
                     <label htmlFor="maxObjects" className="text-sm font-medium">
                       Max objects
                     </label>
                     <Input 
                       id="maxObjects" 
                       name="maxObjects" 
                       type="number" 
                       min="0"
                       placeholder="Unlimited"
                       defaultValue={bucket.quotas?.maxObjects ?? ""}
                     />
                   </div>
                 </div>

               <DialogFooter>
                  <button 
                    type="button"
                    onClick={() => setQuotaDialogOpen(false)}
                    className="inline-flex items-center justify-center rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 border border-input bg-transparent hover:bg-accent hover:text-accent-foreground"
                  >
                    Cancel
                  </button>
                  <Button 
                    type="submit" 
                    disabled={updateMutation.isPending}
                  >
                    {updateMutation.isPending ? "Saving..." : "Save"}
                  </Button>
                </DialogFooter>
               </form>
             </DialogContent>
           </Dialog>

          {/* Attached keys */}
          <section className="space-y-3">
            <h2 className="text-sm font-medium text-muted-foreground">
              Attached keys
              {bucket.keys && bucket.keys.length > 0 ? (
                <span className="ml-1.5 text-muted-foreground/60">({bucket.keys.length})</span>
              ) : null}
            </h2>
            {bucket.keys == null || bucket.keys.length === 0 ? (
              <div className="rounded-lg border bg-card p-6">
                <EmptyState
                  icon="key"
                  title="No keys attached"
                  description="Grant a key access to this bucket from the Keys page."
                />
              </div>
            ) : (
              <div className="rounded-lg border bg-card overflow-hidden">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Name</TableHead>
                      <TableHead className="w-[260px]">Access Key ID</TableHead>
                      <TableHead className="w-[140px]">Permissions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {bucket.keys.map((keyAccess) => (
                      <TableRow key={keyAccess.keyId}>
                        <TableCell className="font-medium">{keyAccess.name ?? "—"}</TableCell>
                        <TableCell className="font-mono text-xs text-muted-foreground">
                          {keyAccess.keyId}
                        </TableCell>
                        <TableCell>
                          <PermissionChips
                            read={!!keyAccess.read}
                            write={!!keyAccess.write}
                            owner={!!keyAccess.owner}
                          />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </section>

        {/* Created */}
           {bucket.created && humanizeTime(bucket.created) !== "—" && (
             <p className="text-xs text-muted-foreground">Created {humanizeTime(bucket.created)}</p>
           )}

           <DangerZone description="Deleting this bucket removes all its objects, aliases (global + per-key), and revokes any key access. Cannot be undone.">
             <Button
               variant="destructive"
               onClick={() => setDeleteDialogOpen(true)}
               disabled={deleteMutation.isPending}
             >
               {deleteMutation.isPending ? "Deleting..." : "Delete bucket"}
             </Button>
           </DangerZone>

           <DeleteBucketConfirm
             open={deleteDialogOpen}
             bucketAlias={bucket.aliases?.[0] ?? bucket.id.slice(0, 12)}
             isDeleting={deleteMutation.isPending}
             onConfirm={handleDelete}
             onCancel={() => setDeleteDialogOpen(false)}
           />
     </div>
   );
 }

function BackLink() {
  return (
    <Link
      to="/"
      className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
    >
      ← Buckets
    </Link>
  );
}
