import { createFileRoute } from "@tanstack/react-router";
import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import { useKey } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";
import { useUpdateKeyPermissions, useDeleteKey } from "@/shared/api/mutations";
import { useState } from "react";
import type { components } from "@/shared/api/types.gen";
import { DangerZone } from "@/shared/ui/DangerZone";

export const Route = createFileRoute("/admin/clusters/$cid/keys/$id")({
  component: adminPage(KeyDetailScreen),
});

function BackLink() {
  return (
    <Link to="/admin/keys" className="text-sm text-muted-foreground hover:text-foreground inline-flex items-center gap-1">
      ← Keys
    </Link>
  );
}

import { PermissionChips } from "@/shared/ui/PermissionChips";
import { DeleteKeyConfirm } from "@/shared/ui/DeleteKeyConfirm";

function BucketName({ globalAliases, localAliases, bucketId }: { globalAliases?: string[]; localAliases?: string[]; bucketId: string }) {
  const primaryAlias = globalAliases?.[0] ?? (localAliases && localAliases.length > 0 ? localAliases[0] : null);
  
  if (primaryAlias) {
    return <span>{primaryAlias}</span>;
  }
  
  return (
    <div className="flex items-center gap-2">
      <span className="font-mono text-xs">{bucketId.slice(0, 12)}...</span>
    </div>
  );
}

function KeyDetailScreen() {
  const { cid, id } = Route.useParams();
  const { data: key, isLoading, error } = useKey(cid, id);
  
  const updatePermissions = useUpdateKeyPermissions();
  const deleteKey = useDeleteKey();
  
  // Edit mode state
  const [isEditing, setIsEditing] = useState(false);
  const [editPermissions, setEditPermissions] = useState<components["schemas"]["BucketPermission"][]>([]);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink />
        <ErrorBanner message="Couldn't load key details." />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <BackLink />
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-4 w-64" />
        <Card>
          <CardContent className="pt-6">
            <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-md">
              <div>
                <dt className="text-sm text-muted-foreground">Access Key ID</dt>
                <dd><Skeleton className="h-4 w-full mt-1" /></dd>
              </div>
              <div>
                <dt className="text-sm text-muted-foreground">Created</dt>
                <dd><Skeleton className="h-4 w-32 mt-1" /></dd>
              </div>
              <div>
                <dt className="text-sm text-muted-foreground">Allow create bucket</dt>
                <dd><Skeleton className="h-4 w-16 mt-1" /></dd>
              </div>
            </dl>
          </CardContent>
        </Card>
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Bucket</TableHead>
                <TableHead className="w-48">Bucket ID</TableHead>
                <TableHead className="w-40">Permissions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(5)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-24" /></TableCell>
                  <TableCell><Skeleton className="h-6 w-16" /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      </div>
    );
  }

  if (!key) {
    return (
      <div className="space-y-6">
        <BackLink />
        <EmptyState
          icon="key"
          title="Key not found"
          description="It may have been deleted."
        />
      </div>
    );
  }

  const handleEditToggle = () => {
    if (!isEditing) {
      setEditPermissions(key.buckets?.map(b => ({
        bucketId: b.bucketId,
        read: b.read,
        write: b.write,
        owner: b.owner,
      })) ?? []);
    }
    setIsEditing(!isEditing);
  };

  const handleSave = () => {
    updatePermissions.mutate({ cid, id, permissions: editPermissions });
    setIsEditing(false);
  };

  const handleCancel = () => {
    setEditPermissions([]);
    setIsEditing(false);
  };

  const handlePermissionChange = (bucketId: string, field: "read" | "write" | "owner") => {
    setEditPermissions(prev => 
      prev.map(p => {
        if (p.bucketId !== bucketId) return p;
        // If owner is true, clear read/write
        const newOwner = !p.owner && field === "owner";
        const newRead = field === "read" ? !p.read : (newOwner ? false : p.read);
        const newWrite = field === "write" ? !p.write : (newOwner ? false : p.write);
        return { ...p, read: newRead, write: newWrite, owner: newOwner };
      })
    );
  };

  return (
    <div className="space-y-6">
      <BackLink />

      {/* Header */}
      <div className="space-y-1.5">
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{key.name ?? "Unnamed key"}</h1>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <span className="uppercase tracking-wide text-[10px] font-medium opacity-70">
            Access Key ID
          </span>
          <span className="font-mono text-xs">{key.accessKeyId ?? key.id}</span>
          <button
            type="button"
            onClick={() => navigator.clipboard.writeText(key.accessKeyId ?? key.id)}
            className="rounded-md p-1 hover:bg-muted opacity-60 hover:opacity-100 transition-opacity"
            aria-label="Copy Access Key ID"
            title="Copy Access Key ID"
          >
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
          </button>
        </div>
        <p className="text-xs text-muted-foreground/70 max-w-prose">
          This is the public credential — pair it with the secret access key (shown
          once at creation) in your S3 client (mc, aws-cli, Cyberduck, etc.).
        </p>
      </div>

      {/* Metadata card */}
      <Card>
        <CardContent className="pt-6">
          <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-md">
            <div>
              <dt className="text-sm text-muted-foreground">Access Key ID</dt>
              <dd className="font-mono text-sm mt-1">{key.accessKeyId}</dd>
            </div>
            {(() => {
              const human = humanizeTime(key.created);
              return human === "—" ? null : (
                <div>
                  <dt className="text-sm text-muted-foreground">Created</dt>
                  <dd className="mt-1">{human}</dd>
                </div>
              );
            })()}
            <div>
              <dt className="text-sm text-muted-foreground">Allow create bucket</dt>
              <dd className="mt-1">{key.allowCreateBucket ? "Yes" : "No"}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      {/* Bucket access table */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <span>Bucket access</span>
          {!isEditing && key.buckets && key.buckets.length > 0 && (
            <Button variant="outline" size="sm" onClick={handleEditToggle}>
              Edit permissions
            </Button>
          )}
        </CardHeader>
        <CardContent className="pt-6">
          {key.buckets && key.buckets.length > 0 ? (
            <>
              {isEditing ? (
                // Edit mode with checkboxes
                <div className="space-y-4">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>Bucket</TableHead>
                        <TableHead className="w-48">Bucket ID</TableHead>
                        <TableHead className="w-40 text-center">Permissions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {editPermissions.map((perm) => (
                        <TableRow key={perm.bucketId}>
                          <TableCell>
                            <BucketName 
                              globalAliases={key.buckets?.find(b => b.bucketId === perm.bucketId)?.globalAliases}
                              localAliases={key.buckets?.find(b => b.bucketId === perm.bucketId)?.localAliases}
                              bucketId={perm.bucketId}
                            />
                          </TableCell>
                          <TableCell className="font-mono text-xs">
                            {perm.bucketId.slice(0, 12)}...{perm.bucketId.slice(-4)}
                          </TableCell>
                          <TableCell className="text-center space-x-2">
                            <div className="flex items-center gap-2 justify-center">
                              <Checkbox
                                id={`read-${perm.bucketId}`}
                                checked={perm.read}
                                onCheckedChange={() => handlePermissionChange(perm.bucketId, "read")}
                                disabled={updatePermissions.isPending}
                              />
                              <label htmlFor={`read-${perm.bucketId}`} className="text-sm">Read</label>
                            </div>
                            <div className="flex items-center gap-2 justify-center mt-1">
                              <Checkbox
                                id={`write-${perm.bucketId}`}
                                checked={perm.write}
                                onCheckedChange={() => handlePermissionChange(perm.bucketId, "write")}
                                disabled={updatePermissions.isPending}
                              />
                              <label htmlFor={`write-${perm.bucketId}`} className="text-sm">Write</label>
                            </div>
                            <div className="flex items-center gap-2 justify-center mt-1">
                              <Checkbox
                                id={`owner-${perm.bucketId}`}
                                checked={perm.owner}
                                onCheckedChange={() => handlePermissionChange(perm.bucketId, "owner")}
                                disabled={updatePermissions.isPending}
                              />
                              <label htmlFor={`owner-${perm.bucketId}`} className="text-sm">Owner</label>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  <div className="flex gap-2 pt-4">
                    <Button onClick={handleSave} disabled={updatePermissions.isPending}>
                      {updatePermissions.isPending ? "Saving…" : "Save"}
                    </Button>
                    <Button variant="outline" onClick={handleCancel} disabled={updatePermissions.isPending}>
                      Cancel
                    </Button>
                  </div>
                </div>
              ) : (
                // Read mode with PermissionChips
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Bucket</TableHead>
                      <TableHead className="w-48">Bucket ID</TableHead>
                      <TableHead className="w-40">Permissions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {key.buckets.map((bucket) => (
                      <TableRow key={bucket.bucketId}>
                        <TableCell>
                          <BucketName 
                            globalAliases={bucket.globalAliases} 
                            localAliases={bucket.localAliases} 
                            bucketId={bucket.bucketId} 
                          />
                        </TableCell>
                        <TableCell className="font-mono text-xs">
                          {bucket.bucketId.slice(0, 12)}...{bucket.bucketId.slice(-4)}
                        </TableCell>
                        <TableCell>
                          <PermissionChips read={bucket.read} write={bucket.write} owner={bucket.owner} />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </>
          ) : (
            <EmptyState
              icon="database"
              title="No bucket access"
              description="This key has no permissions on any bucket."
            />
          )}
        </CardContent>
      </Card>

      <DangerZone description="Deleting this key revokes all its S3 access immediately. Buckets it was granted on are unaffected.">
        <Button
          variant="destructive"
          onClick={() => setDeleteDialogOpen(true)}
        >
          Delete key
        </Button>
      </DangerZone>

      <DeleteKeyConfirm
        open={deleteDialogOpen}
        keyName={key.name}
        isDeleting={deleteKey.isPending}
        onConfirm={() => {
          deleteKey.mutate({ cid, id });
          setDeleteDialogOpen(false);
        }}
        onCancel={() => setDeleteDialogOpen(false)}
      />

      {/* Show error if permission update fails */}
      {updatePermissions.isError && (
        <div className="rounded bg-destructive/10 p-4 text-destructive">
          Failed to save permissions. Please try again.
        </div>
      )}
    </div>
  );
}
