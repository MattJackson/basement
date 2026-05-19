import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
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

export const Route = createFileRoute("/admin/buckets/$id")({
  component: adminPage(AdminBucketDetail),
});

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (copied) {
      const timer = setTimeout(() => setCopied(false), 2000);
      return () => clearTimeout(timer);
    }
  }, [copied]);

  return (
    <button
      type="button"
      onClick={() => {
        navigator.clipboard.writeText(text);
        setCopied(true);
      }}
      className="rounded-md p-1.5 hover:bg-muted opacity-60 hover:opacity-100 transition-opacity"
      aria-label="Copy to clipboard"
    >
      {copied ? (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="h-3 w-3 text-green-600"
        >
          <polyline points="20 6 9 17 4 12" />
        </svg>
      ) : (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="h-3 w-3"
        >
          <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
          <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2v1" />
        </svg>
      )}
    </button>
  );
}

function AdminBucketDetail() {
  const { id } = Route.useParams();
  const updateMutation = useUpdateBucket();
  const deleteMutation = useDeleteBucket();
  const [isEditingAlias, setIsEditingAlias] = useState(false);
  const [aliasInput, setAliasInput] = useState("");
  const [quotaDialogOpen, setQuotaDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const { data: bucket, isLoading, error } = useBucket(id);

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
      id: bucket.id,
      update: { quotas },
    });
    setQuotaDialogOpen(false);
  };

  const handleDelete = () => {
    deleteMutation.mutate(bucket.id);
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
                <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground">
                  <span>{bucket.id}</span>
                  <CopyButton text={bucket.id} />
                  {bucket.aliases && bucket.aliases.length > 1 ? (
                    bucket.aliases.slice(1).map((alias) => (
                      <Badge key={alias} variant="secondary" className="text-xs">
                        {alias}
                      </Badge>
                    ))
                  ) : null}
                </div>
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
                    {bucket.unfinishedUploads && bucket.unfinishedUploads > 0
                      ? bucket.unfinishedUploads.toLocaleString()
                      : "—"}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">Unfinished uploads</div>
                </div>
              </div>
            </CardContent>
          </Card>

         {/* Quotas card */}
           <Card>
             <CardHeader className="flex flex-row items-center justify-between">
               <span>Quotas</span>
               <Button 
                 size="sm" 
                 variant="outline"
                 onClick={() => setQuotaDialogOpen(true)}
               >
                 Edit quotas
               </Button>
             </CardHeader>
             <CardContent className="pt-6">
               <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-md">
                 <div>
                   <dt className="text-sm text-muted-foreground">Max size</dt>
                   <dd className="font-medium tabular-nums mt-1">
                     {bucket.quotas?.maxSize != null ? (
                       humanizeBytes(bucket.quotas.maxSize)
                     ) : bucket.quotas == null ? (
                       "Unlimited"
                     ) : (
                       "—"
                     )}
                   </dd>
                 </div>
                 <div>
                   <dt className="text-sm text-muted-foreground">Max objects</dt>
                   <dd className="font-medium tabular-nums mt-1">
                     {bucket.quotas?.maxObjects != null ? (
                       bucket.quotas.maxObjects.toLocaleString()
                     ) : bucket.quotas == null ? (
                       "Unlimited"
                     ) : (
                       "—"
                     )}
                   </dd>
                 </div>
               </dl>
             </CardContent>
           </Card>

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

          {/* Attached keys table */}
          <Card>
            <CardHeader>Attached keys</CardHeader>
            <CardContent className="pt-6">
              {bucket.keys == null || bucket.keys.length === 0 ? (
                <EmptyState
                  icon="key"
                  title="No keys attached"
                  description="Grant a key access to this bucket from the Keys page."
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-1/3">Key ID</TableHead>
                      <TableHead className="w-1/3">Name</TableHead>
                      <TableHead className="w-1/3">Permissions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {bucket.keys.map((keyAccess) => (
                      <TableRow key={keyAccess.keyId}>
                        <TableCell>
                          <span className="font-mono text-xs">
                            {keyAccess.keyId.slice(0, 12)}…{keyAccess.keyId.slice(-4)}
                          </span>
                        </TableCell>
                        <TableCell>{keyAccess.name ?? "—"}</TableCell>
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
              )}
            </CardContent>
          </Card>

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
      to="/admin"
      className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
    >
      ← Buckets
    </Link>
  );
}
