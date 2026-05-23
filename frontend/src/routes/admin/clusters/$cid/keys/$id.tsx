import { createFileRoute } from "@tanstack/react-router";
import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import { useKey, useClusterBuckets, useGetCluster } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";
import { useUpdateKeyPermissions, useDeleteKey } from "@/shared/api/mutations";
import { useMemo, useState } from "react";
import type { components } from "@/shared/api/types.gen";
import { DangerZone } from "@/shared/ui/DangerZone";
import { GrantBucketAccessDialog } from "@/shared/ui/GrantBucketAccessDialog";
import { RevokeAccessConfirm } from "@/shared/ui/RevokeAccessConfirm";
import { useElevationGuard } from "@/shared/auth/elevation";

// v1.4.0b: pagination + filter + "only granted" toggle on the key
// permissions edit screen. On a cluster with 1000+ buckets the flat
// scroll-everything-and-checkbox UI was unusable. The page-size
// constant keeps the DOM cost bounded per page; filter narrows the
// list client-side (server-side search lands when the all-buckets list
// endpoint adds q= support).
const PAGE_SIZE = 50;

export const Route = createFileRoute("/admin/clusters/$cid/keys/$id")({
  component: adminPage(KeyDetailScreen),
});

function BackLink({ cid }: { cid: string }) {
  // v1.11.0.15: cluster-scoped back-target. The global /admin/keys
  // route was removed; the cluster detail page is the canonical
  // "all keys for this cluster" view (renders the keys section the
  // operator came from). There's no standalone /admin/clusters/{cid}/keys/
  // index route — keys are inherently per-cluster and live on the
  // cluster detail.
  return (
    <Link
      to="/admin/clusters/$cid"
      params={{ cid }}
      className="text-sm text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
    >
      ← Cluster
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
  const { data: cluster } = useGetCluster(cid);
  const { data: clusterBuckets } = useClusterBuckets(cid);

  const updatePermissions = useUpdateKeyPermissions();
  const deleteKey = useDeleteKey();
  // v1.3.0a.3: key:edit_permissions and key:delete are ADMIN-tier on
  // the backend. Wrap every destructive / state-changing handler so a
  // 403 ELEVATION_REQUIRED pops the modal and the mutation re-fires
  // automatically on success (no second click required).
  const runWithElevation = useElevationGuard();

  // Edit mode state
  const [isEditing, setIsEditing] = useState(false);
  const [editPermissions, setEditPermissions] = useState<components["schemas"]["BucketPermission"][]>([]);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [grantOpen, setGrantOpen] = useState(false);
  const [revokeTarget, setRevokeTarget] = useState<{ bucketId: string; label: string } | null>(null);

  // v1.4.0b: filter + pagination + only-granted toggle. Each control
  // resets pagination so the operator never lands on an empty page
  // after narrowing.
  const [filter, setFilter] = useState("");
  const [page, setPage] = useState(0);
  const [onlyGranted, setOnlyGranted] = useState(false);

  // Bucket-label lookup keyed by bucket id. Built once per editPermissions
  // / clusterBuckets change so the filter input + rendered rows can
  // resolve a label in O(1). Garage returns the alias on Bucket.aliases
  // and on key.buckets[].globalAliases / localAliases; the per-row
  // BucketName component already handles the fallback to a truncated
  // hash, but the FILTER needs a single canonical string to substring
  // against, so we precompute the same fallback here.
  const bucketLabels = useMemo(() => {
    const out = new Map<string, string>();
    for (const b of clusterBuckets ?? []) {
      if (!b.id) continue;
      out.set(b.id, b.aliases?.[0] ?? `${b.id.slice(0, 12)}…`);
    }
    for (const b of key?.buckets ?? []) {
      if (out.has(b.bucketId)) continue;
      const label =
        b.globalAliases?.[0] ?? b.localAliases?.[0] ?? `${b.bucketId.slice(0, 12)}…`;
      out.set(b.bucketId, label);
    }
    return out;
  }, [clusterBuckets, key?.buckets]);

  // The filtered + ordered list of permission rows visible to the user
  // BEFORE pagination is applied. Filter matches the bucket label
  // substring (case-insensitive); onlyGranted hides rows where every
  // permission bit is false. This is recomputed cheaply on each render
  // because the list maxes at "every bucket in the cluster", which is
  // already bounded by the page-size below for scroll cost.
  const visiblePermissions = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    return editPermissions.filter((p) => {
      if (onlyGranted && !p.read && !p.write && !p.owner) return false;
      if (!needle) return true;
      const label = (bucketLabels.get(p.bucketId) ?? p.bucketId).toLowerCase();
      return label.includes(needle);
    });
  }, [editPermissions, filter, onlyGranted, bucketLabels]);

  const totalPages = Math.max(1, Math.ceil(visiblePermissions.length / PAGE_SIZE));
  // Clamp the current page in case a filter trimmed the list below it.
  // Avoids a "Page 5 of 2" footer / blank table when the user types
  // into the filter on a high page.
  const clampedPage = Math.min(page, totalPages - 1);
  const pageStart = clampedPage * PAGE_SIZE;
  const pageRows = visiblePermissions.slice(pageStart, pageStart + PAGE_SIZE);

  const grantedCount = useMemo(
    () => editPermissions.filter((p) => p.read || p.write || p.owner).length,
    [editPermissions],
  );

  // Capability gate: per-key R/W/O bucket permissions are the Garage key
  // model. AWS-S3 / MinIO use IAM; those drivers return ErrUnsupported
  // from UpdateKeyPermissions and the affordances would just 501 on
  // submit. We gate on the connection's driver field here; switching to
  // a per-connection capability fetch (keyModel === "garage") is the
  // doctrinally correct move once that endpoint lands.
  const supportsGrants =
    cluster?.driver === "garage" || cluster?.driver === "garage-v1";

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink cid={cid} />
        <ErrorBanner message="Couldn't load key details." />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <BackLink cid={cid} />
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-4 w-64" />
        <div className="rounded-lg border bg-card px-4 py-3 flex gap-8">
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-4 w-36" />
        </div>
        <Card>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Bucket</TableHead>
                <TableHead className="w-40">Permissions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(5)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
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
        <BackLink cid={cid} />
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
      // v1.4.0b: hydrate from the UNION of (every bucket in the cluster)
      // and (every bucket the key already touches). Grants stay as-is;
      // ungranted buckets render as unchecked rows the operator can
      // tick. This is the "edit at scale" model — pagination + filter
      // make it usable on 1000+ buckets, the "Only granted" toggle is
      // the escape hatch when the operator just wants to audit/edit
      // existing grants.
      const granted = new Map<string, components["schemas"]["BucketPermission"]>();
      for (const b of key.buckets ?? []) {
        granted.set(b.bucketId, {
          bucketId: b.bucketId,
          read: b.read,
          write: b.write,
          owner: b.owner,
        });
      }
      const merged: components["schemas"]["BucketPermission"][] = [];
      // Granted first so they're easy to find at the top when "Only granted"
      // is off too. Stable alphabetical sort below for the long tail.
      for (const b of key.buckets ?? []) {
        merged.push({
          bucketId: b.bucketId,
          read: b.read,
          write: b.write,
          owner: b.owner,
        });
      }
      const tail: components["schemas"]["BucketPermission"][] = [];
      for (const b of clusterBuckets ?? []) {
        if (!b.id || granted.has(b.id)) continue;
        tail.push({ bucketId: b.id, read: false, write: false, owner: false });
      }
      tail.sort((a, b) => {
        const la = (bucketLabels.get(a.bucketId) ?? a.bucketId).toLowerCase();
        const lb = (bucketLabels.get(b.bucketId) ?? b.bucketId).toLowerCase();
        return la.localeCompare(lb);
      });
      setEditPermissions([...merged, ...tail]);
      setFilter("");
      setPage(0);
      setOnlyGranted(false);
    }
    setIsEditing(!isEditing);
  };

  const handleSave = async () => {
    // Send the full list — backend treats UpdateKeyPermissions as a
    // wholesale replace, and rows with read=write=owner=false resolve
    // to a deny on Garage (no-op on already-ungranted buckets).
    const perms = editPermissions;
    setIsEditing(false);
    try {
      await runWithElevation(() =>
        updatePermissions.mutateAsync({ cid, id, permissions: perms }),
      );
    } catch {
      // ELEVATION_CANCELLED / network errors surface via the
      // mutation's existing isError banner below.
    }
  };

  const handleCancel = () => {
    setEditPermissions([]);
    setFilter("");
    setPage(0);
    setOnlyGranted(false);
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
      <BackLink cid={cid} />

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

      {/* Metadata — compact inline row to match Quotas pattern on bucket detail */}
      <div className="flex flex-wrap items-baseline gap-x-8 gap-y-2 text-sm rounded-lg border bg-card px-4 py-3">
        {(() => {
          const human = humanizeTime(key.created);
          return human === "—" ? null : (
            <span>
              <span className="text-xs text-muted-foreground mr-1.5">Created</span>
              <span className="font-medium">{human}</span>
            </span>
          );
        })()}
        <span>
          <span className="text-xs text-muted-foreground mr-1.5">Can create buckets</span>
          <span className="font-medium">{key.allowCreateBucket ? "Yes" : "No"}</span>
        </span>
      </div>

      {/* Bucket access table */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <span>
            Bucket access
            {key.buckets && key.buckets.length > 0 ? (
              <span className="ml-1.5 text-muted-foreground/60 text-sm font-normal">({key.buckets.length})</span>
            ) : null}
          </span>
          {!isEditing && (
            <div className="flex items-center gap-2">
              {supportsGrants && (
                <Button variant="outline" size="sm" onClick={() => setGrantOpen(true)}>
                  + Grant access
                </Button>
              )}
              {/* v1.4.0b: "Edit permissions" now hydrates the FULL cluster
                  bucket list (granted + ungranted) so the operator can
                  tick/untick at scale. Available whenever any bucket
                  exists in the cluster, even if this key has zero
                  grants today — that's the "first-time setup" entry. */}
              {((key.buckets && key.buckets.length > 0) ||
                (clusterBuckets && clusterBuckets.length > 0)) && (
                <Button variant="outline" size="sm" onClick={handleEditToggle}>
                  Edit permissions
                </Button>
              )}
            </div>
          )}
        </CardHeader>
        <CardContent className="pt-6">
          {isEditing ? (
            // v1.4.0b: edit mode with filter + pagination + only-granted
            // toggle + sticky save bar. The list is the FULL cluster
            // bucket list (granted + ungranted) hydrated by
            // handleEditToggle above; checkbox state survives pagination
            // because we mutate the single editPermissions array, not
            // per-page slices.
            <div className="space-y-4" data-testid="key-perms-editor">
              {/* Filter + toggle row. Both narrow the visible list;
                  changing either resets pagination so the operator
                  never lands on a now-empty page. */}
              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <Input
                  type="search"
                  placeholder="Filter buckets..."
                  value={filter}
                  onChange={(e) => {
                    setFilter(e.target.value);
                    setPage(0);
                  }}
                  className="sm:max-w-xs"
                  aria-label="Filter buckets"
                  data-testid="key-perms-filter"
                />
                <div className="flex items-center gap-2 text-sm">
                  <Checkbox
                    id="only-granted-toggle"
                    checked={onlyGranted}
                    onCheckedChange={(v) => {
                      setOnlyGranted(v === true);
                      setPage(0);
                    }}
                    data-testid="key-perms-only-granted"
                  />
                  <label htmlFor="only-granted-toggle" className="select-none">
                    Show only granted ({grantedCount})
                  </label>
                </div>
              </div>

              {pageRows.length === 0 ? (
                <div className="rounded-lg border bg-muted/20 px-4 py-6 text-center text-sm text-muted-foreground">
                  {filter || onlyGranted
                    ? "No buckets match the current filter."
                    : "This cluster has no buckets yet."}
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Bucket</TableHead>
                      <TableHead className="w-40 text-center">Permissions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {pageRows.map((perm) => {
                      // v1.4.0b: prefer the existing key.buckets entry's
                      // aliases (set by Garage on already-granted edges)
                      // when present, otherwise fall back to the cluster
                      // bucket's first alias from the precomputed label
                      // map. This makes ungranted rows show readable
                      // names instead of "{12-char-hash}...".
                      const grantedRow = key.buckets?.find((b) => b.bucketId === perm.bucketId);
                      const fallbackLabel = bucketLabels.get(perm.bucketId);
                      return (
                      <TableRow key={perm.bucketId}>
                        <TableCell>
                          {grantedRow ? (
                            <BucketName
                              globalAliases={grantedRow.globalAliases}
                              localAliases={grantedRow.localAliases}
                              bucketId={perm.bucketId}
                            />
                          ) : (
                            <BucketName
                              globalAliases={fallbackLabel ? [fallbackLabel] : undefined}
                              bucketId={perm.bucketId}
                            />
                          )}
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
                      );
                    })}
                  </TableBody>
                </Table>
              )}

              {/* Pagination footer. Always rendered (even on a single
                  page) so the operator has a stable "showing X-Y of Z"
                  signal and never wonders if the list is truncated. */}
              <div className="flex items-center justify-between gap-2 pt-2 text-sm text-muted-foreground">
                <span data-testid="key-perms-page-indicator">
                  {visiblePermissions.length === 0
                    ? "0 buckets"
                    : `Showing ${pageStart + 1}-${Math.min(pageStart + PAGE_SIZE, visiblePermissions.length)} of ${visiblePermissions.length}`}
                  {totalPages > 1 ? ` (page ${clampedPage + 1} of ${totalPages})` : null}
                </span>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage((p) => Math.max(0, p - 1))}
                    disabled={clampedPage === 0}
                    data-testid="key-perms-prev"
                  >
                    Previous
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                    disabled={clampedPage >= totalPages - 1}
                    data-testid="key-perms-next"
                  >
                    Next
                  </Button>
                </div>
              </div>

              {/* Sticky Save bar — pinned to the bottom of the viewport
                  while editing so the operator never has to scroll the
                  table to commit changes. position: sticky inside the
                  CardContent (not fixed) so it stays inside the page
                  flow and doesn't overlap the global nav on small
                  screens. The negative bottom inset cancels the
                  CardContent's own padding so the bar sits flush. */}
              <div
                className="sticky bottom-0 -mx-6 -mb-6 mt-4 flex items-center justify-end gap-2 border-t bg-background/95 px-6 py-3 backdrop-blur"
                data-testid="key-perms-sticky-save"
              >
                <span className="mr-auto text-xs text-muted-foreground">
                  {grantedCount} bucket{grantedCount === 1 ? "" : "s"} granted
                </span>
                <Button variant="outline" onClick={handleCancel} disabled={updatePermissions.isPending}>
                  Cancel
                </Button>
                <Button onClick={handleSave} disabled={updatePermissions.isPending}>
                  {updatePermissions.isPending ? "Saving…" : "Save changes"}
                </Button>
              </div>
            </div>
          ) : key.buckets && key.buckets.length > 0 ? (
            // Read mode with PermissionChips
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Bucket</TableHead>
                  <TableHead className="w-40">Permissions</TableHead>
                  {supportsGrants && <TableHead className="w-24 text-right">Actions</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {key.buckets.map((bucket) => {
                  const bucketLabel =
                    bucket.globalAliases?.[0] ??
                    bucket.localAliases?.[0] ??
                    `${bucket.bucketId.slice(0, 12)}…`;
                  return (
                    <TableRow key={bucket.bucketId}>
                      <TableCell>
                        <BucketName
                          globalAliases={bucket.globalAliases}
                          localAliases={bucket.localAliases}
                          bucketId={bucket.bucketId}
                        />
                      </TableCell>
                      <TableCell>
                        <PermissionChips read={bucket.read} write={bucket.write} owner={bucket.owner} />
                      </TableCell>
                      {supportsGrants && (
                        <TableCell className="text-right">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setRevokeTarget({ bucketId: bucket.bucketId, label: bucketLabel })}
                            disabled={updatePermissions.isPending}
                          >
                            Revoke
                          </Button>
                        </TableCell>
                      )}
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
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
        onConfirm={async () => {
          setDeleteDialogOpen(false);
          try {
            await runWithElevation(() =>
              deleteKey.mutateAsync({ cid, id }),
            );
          } catch {
            // ELEVATION_CANCELLED / network errors surface via the
            // mutation's existing isError state.
          }
        }}
        onCancel={() => setDeleteDialogOpen(false)}
      />

      {/* Grant access dialog — only mounted when supported. Candidates
          are cluster buckets minus those already in this key's grants
          (Garage can't have two edges to the same bucket-key pair). */}
      {supportsGrants && (
        <GrantBucketAccessDialog
          open={grantOpen}
          isSaving={updatePermissions.isPending}
          errorMessage={updatePermissions.isError ? "Couldn't grant access. Try again." : null}
          candidates={(() => {
            const owned = new Set((key.buckets ?? []).map((b) => b.bucketId));
            return (clusterBuckets ?? [])
              .filter((b) => b.id && !owned.has(b.id))
              .map((b) => ({
                bucketId: b.id as string,
                label: b.aliases?.[0] ?? `${(b.id as string).slice(0, 12)}…`,
              }));
          })()}
          onCancel={() => setGrantOpen(false)}
          onSubmit={async ({ bucketId, read, write, owner }) => {
            const existing: components["schemas"]["BucketPermission"][] = (key.buckets ?? []).map((b) => ({
              bucketId: b.bucketId,
              read: b.read,
              write: b.write,
              owner: b.owner,
            }));
            const next = [...existing, { bucketId, read, write, owner }];
            try {
              await runWithElevation(() =>
                updatePermissions.mutateAsync({ cid, id, permissions: next }),
              );
              setGrantOpen(false);
            } catch {
              // ELEVATION_CANCELLED / network errors surface via the
              // dialog's existing errorMessage prop.
            }
          }}
        />
      )}

      {/* Revoke confirmation. Submit translates to UpdateKeyPermissions
          with read=write=owner=false on the target bucket — Garage's
          /bucket/deny resolves the edge to nothing, which lists/details
          drop on next fetch. */}
      <RevokeAccessConfirm
        open={revokeTarget !== null}
        subject={key.name ?? "this key"}
        target={revokeTarget?.label ?? ""}
        isRevoking={updatePermissions.isPending}
        onCancel={() => setRevokeTarget(null)}
        onConfirm={async () => {
          if (!revokeTarget) return;
          const target = revokeTarget;
          try {
            await runWithElevation(() =>
              updatePermissions.mutateAsync({
                cid,
                id,
                permissions: [
                  {
                    bucketId: target.bucketId,
                    read: false,
                    write: false,
                    owner: false,
                  },
                ],
              }),
            );
            setRevokeTarget(null);
          } catch {
            // ELEVATION_CANCELLED / network errors surface via the
            // mutation's existing error state.
          }
        }}
      />

      {/* Show error if permission update fails (and no dialog is showing it) */}
      {updatePermissions.isError && !grantOpen && !revokeTarget && (
        <div className="rounded bg-destructive/10 p-4 text-destructive">
          Failed to save permissions. Please try again.
        </div>
      )}
    </div>
  );
}
