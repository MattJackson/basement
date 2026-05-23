import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useState, useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { Dialog, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { DeleteClusterConfirm } from "@/shared/ui/DeleteClusterConfirm";
import { DangerZone } from "@/shared/ui/DangerZone";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime, humanizeBytes } from "@/shared/lib/format";
import {
  useGetCluster,
  useNodes,
  useCapabilities,
  useTestClusterQuery,
  useClusterBuckets,
  useClusterKeys,
  useBucket,
  useKey,
  useClusterAdmins,
  useAssignRole,
  useUnassignRole,
  usePolicies,
  type ClusterAdminAssignment,
  type PolicyRole,
} from "@/shared/api/queries";
import { client } from "@/shared/api/client";
import { useDeleteCluster } from "@/shared/api/mutations";
import { adminPage } from "@/shared/layout/adminPage";
import type { components } from "@/shared/api/types.gen";
import { DriverBadge } from "@/components/clusters/DriverBadge";
import { useElevationGuard } from "@/shared/auth/elevation";

export const Route = createFileRoute("/admin/clusters/$cid/")({
  component: adminPage(ClusterDetailScreen),
});

function ClusterDetailScreen() {
  const { cid } = Route.useParams();
  const navigate = useNavigate();

  const { data: cluster, isLoading, error } = useGetCluster(cid);
  const { data: nodes } = useNodes(cid);
  const { data: capabilities } = useCapabilities();
  const { data: buckets, isLoading: bucketsLoading } = useClusterBuckets(cid);
  const { data: keys, isLoading: keysLoading } = useClusterKeys(cid);
  const deleteCluster = useDeleteCluster();
  // ADR-0003 v1.2.0b: cluster:delete is ELEVATED-min — wrap the
  // click handler so a 403 ELEVATION_REQUIRED triggers the password
  // modal and retries the delete on success.
  const runWithElevation = useElevationGuard();

  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Test cluster state — auto-fires on mount + caches 60s so the
  // header HealthPill reflects reality, not a default 'Unavailable'.
  const testQuery = useTestClusterQuery(cid, { auto: true });
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

  const status: "healthy" | "degraded" | "unavailable" | "checking" =
    testResult ? getStatusFromResult(testResult)
    : (testQuery.isFetching || testQuery.isPending) ? "checking"
    : "unavailable";

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
          <div className="flex-1" />
          <Button
            variant="outline"
            size="sm"
            onClick={() => navigate({ to: "/admin/migrate", search: { srcCid: cid } })}
            title="Bulk-copy every bucket from this cluster to another cluster"
          >
            Migrate this cluster
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => navigate({ to: "/admin/clusters/$cid/edit", params: { cid } })}
            title="Edit label, color, admin token, S3 endpoint…"
          >
            Edit cluster
          </Button>
        </div>
      </div>

      {/* Stats card */}
      <Card>
        <CardContent className="pt-6">
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <div className="text-center p-4 rounded-lg bg-muted/50">
              <div className="text-xl font-semibold tabular-nums">
                {bucketsLoading ? <Skeleton className="h-6 w-10 mx-auto" /> : (buckets?.length ?? 0)}
              </div>
              <div className="text-xs text-muted-foreground mt-1">Buckets</div>
            </div>
            <div className="text-center p-4 rounded-lg bg-muted/50">
              <div className="text-xl font-semibold tabular-nums">
                {keysLoading ? <Skeleton className="h-6 w-10 mx-auto" /> : (keys?.length ?? 0)}
              </div>
              <div className="text-xs text-muted-foreground mt-1">Keys</div>
            </div>
            <div className="text-center p-4 rounded-lg bg-muted/50">
              <div className="text-xl font-semibold tabular-nums">{nodes?.length ?? 0}</div>
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
          ) : (testQuery.isFetching || testQuery.isPending) ? (
            <p className="text-sm text-muted-foreground">Checking connection…</p>
          ) : (
            <p className="text-sm text-muted-foreground">Click "Test connection" to verify the cluster is reachable.</p>
          )}
        </CardContent>
      </Card>

      {/* Cluster admins — persona-level info, shown above Buckets
          because "who runs this cluster" is the operator's first
          question when they hit a cluster detail page. v1.3.0e. */}
      <ClusterAdminsSection cid={cid} />

      {/* Buckets section — admin-grade columns */}
      <section className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h2 className="text-sm font-medium text-muted-foreground">
            Buckets
            {buckets ? <span className="ml-1.5 text-muted-foreground/60">({buckets.length})</span> : null}
          </h2>
        </div>
        {bucketsLoading ? (
          <Skeleton className="h-24 w-full rounded-lg" />
        ) : !buckets || buckets.length === 0 ? (
          <div className="rounded-lg border bg-card p-6">
            <EmptyState
              icon="database"
              title="No buckets yet"
              description="Buckets in this cluster will appear here."
            />
          </div>
        ) : (
          <div className="rounded-lg border bg-card overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead className="text-right w-[140px]">Size</TableHead>
                  <TableHead className="text-right w-[120px]">Objects</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {buckets.slice(0, 8).map((b) => (
                  <ClusterBucketRow key={b.id} cid={cid} bucketId={b.id} fallbackAlias={b.aliases?.[0]} />
                ))}
              </TableBody>
            </Table>
            {buckets.length > 8 && (
              <div className="px-4 py-2 text-xs text-muted-foreground border-t">
                + {buckets.length - 8} more in this cluster
              </div>
            )}
          </div>
        )}
      </section>

      {/* Keys section — admin-grade columns */}
      <section className="space-y-3">
        <div className="flex items-baseline justify-between">
          <h2 className="text-sm font-medium text-muted-foreground">
            Keys
            {keys ? <span className="ml-1.5 text-muted-foreground/60">({keys.length})</span> : null}
          </h2>
          {/* v1.11.0.15: no global "View all →" — keys are per-cluster
              by design and this section already lists every key on
              this cluster. The old cross-cluster /admin/keys
              aggregate was removed (orphan route per the
              per-cluster route model). */}
        </div>
        {keysLoading ? (
          <Skeleton className="h-24 w-full rounded-lg" />
        ) : !keys || keys.length === 0 ? (
          <div className="rounded-lg border bg-card p-6">
            <EmptyState
              icon="key"
              title="No keys yet"
              description="Access keys for this cluster will appear here."
            />
          </div>
        ) : (
          <div className="rounded-lg border bg-card overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Access Key ID</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.slice(0, 8).map((k) => (
                  <ClusterKeyRow key={k.id} cid={cid} keyId={k.id} fallbackName={k.name} />
                ))}
              </TableBody>
            </Table>
            {keys.length > 8 && (
              <div className="px-4 py-2 text-xs text-muted-foreground border-t">
                {/* v1.11.0.15: no link target — the global aggregate
                    page was removed and keys are inherently
                    per-cluster. Tail count only. */}
                + {keys.length - 8} more on this cluster
              </div>
            )}
          </div>
        )}
      </section>

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

      {/* Maintenance → Scrub link (v1.4.0c). Always rendered — the
          page itself surfaces "not supported" for AWS/MinIO drivers
          rather than hiding the link, so an operator can always learn
          why scrub isn't available on a backend instead of finding
          it missing without explanation. */}
      <Link to="/admin/clusters/$cid/scrub" params={{ cid }}>
        <Card className="cursor-pointer hover:bg-muted/50 transition-colors">
          <CardContent className="pt-6">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="font-medium">Maintenance — Scrub</h3>
                <p className="text-sm text-muted-foreground mt-1">
                  Inspect block-scrub state and kick off a durability scan.
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
      </Link>

      {/* Layout section - gated by capability. Route under cluster
          scope lands with CLUSTER.LAYOUT-EDITOR; for now anchor. */}
      {capabilities?.layout !== "readonly" ? (
        <Link to="/admin/clusters/$cid/layout" params={{ cid }}>
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
        </Link>
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
        onConfirm={async () => {
          setDeleteDialogOpen(false);
          try {
            await runWithElevation(() => deleteCluster.mutateAsync(cid));
          } catch {
            // ELEVATION_CANCELLED / network errors surface via the
            // mutation's existing error toast / banner.
          }
        }}
        onCancel={() => setDeleteDialogOpen(false)}
      />
    </div>
  );
}

/** Cluster-detail bucket row. Fires its own useBucket() so we get
 *  size/objects/created (the cluster-scoped list endpoint only
 *  returns id + aliases on Garage v1). Eight max per cluster-detail
 *  page so the parallel fetch fan-out is bounded. */
function ClusterBucketRow({ cid, bucketId, fallbackAlias }: { cid: string; bucketId: string; fallbackAlias?: string }) {
  const { data: detail } = useBucket(cid, bucketId);
  const name = detail?.aliases?.[0] ?? fallbackAlias ?? bucketId.slice(0, 12);
  return (
    <TableRow
      className="cursor-pointer hover:bg-muted/40"
      onClick={(e) => {
        const navTarget = (e.currentTarget.querySelector("a[data-row-link]") as HTMLAnchorElement | null);
        if (navTarget) navTarget.click();
      }}
    >
      <TableCell className="font-medium">
        <Link
          to="/admin/clusters/$cid/buckets/$id"
          params={{ cid, id: bucketId }}
          data-row-link
          className="hover:underline"
        >
          {name}
        </Link>
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {detail ? humanizeBytes(detail.bytes) : <Skeleton className="h-3 w-12 ml-auto" />}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {detail ? (detail.objects ?? 0).toLocaleString() : <Skeleton className="h-3 w-8 ml-auto" />}
      </TableCell>
    </TableRow>
  );
}

/** Cluster-detail key row. Fires useKey() to pull the canonical
 *  access-key-ID (Garage stores ID-as-access-key but the field on
 *  the list response is just `id`; the detail response carries the
 *  separate accessKeyId field if it differs). */
function ClusterKeyRow({ cid, keyId, fallbackName }: { cid: string; keyId: string; fallbackName?: string }) {
  const { data: detail } = useKey(cid, keyId);
  const name = detail?.name || fallbackName || "Unnamed";
  const akid = detail?.accessKeyId ?? keyId;
  return (
    <TableRow
      className="cursor-pointer hover:bg-muted/40"
      onClick={(e) => {
        const navTarget = (e.currentTarget.querySelector("a[data-row-link]") as HTMLAnchorElement | null);
        if (navTarget) navTarget.click();
      }}
    >
      <TableCell className="font-medium">
        <Link
          to="/admin/clusters/$cid/keys/$id"
          params={{ cid, id: keyId }}
          data-row-link
          className="hover:underline"
        >
          {name}
        </Link>
      </TableCell>
      <TableCell className="font-mono text-xs text-muted-foreground">{akid}</TableCell>
    </TableRow>
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

function HealthPill({ status, message }: { status: "healthy" | "degraded" | "unavailable" | "checking"; message?: string }) {
  const variants = {
    healthy: "bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20",
    degraded: "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400 border-yellow-500/20",
    unavailable: "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/20",
    checking: "bg-muted/50 text-muted-foreground border-border",
  } as const;

  const labels = {
    healthy: "Healthy",
    degraded: "Degraded",
    unavailable: "Unavailable",
    checking: "Checking…",
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

// --- Cluster admins section (v1.3.0e CLUSTER.ADMINS) ----------------
//
// Contextual view of who has admin authority over THIS cluster.
// Reads from /admin/clusters/{cid}/admins which joins user display
// names server-side and marks wildcard-inherited rows. Writes go
// through the global assignment endpoints with scope cluster:{cid}.
//
// Inherited rows (cluster:* or * superuser) render an "inherited
// from global" badge and disable the Remove button — those have to
// be managed from /admin/policies because they affect more than this
// cluster.
function ClusterAdminsSection({ cid }: { cid: string }) {
  const { data, isLoading, error } = useClusterAdmins(cid);
  const [addOpen, setAddOpen] = useState(false);

  return (
    <section className="space-y-3">
      <div className="flex items-baseline justify-between">
        <h2 className="text-sm font-medium text-muted-foreground">
          Cluster admins
          {data?.assignments ? (
            <span className="ml-1.5 text-muted-foreground/60">
              ({data.assignments.length})
            </span>
          ) : null}
        </h2>
        <Button size="sm" variant="outline" onClick={() => setAddOpen(true)}>
          + Add cluster admin
        </Button>
      </div>

      {error ? (
        <ErrorBanner message="Couldn't load cluster admins." />
      ) : isLoading ? (
        <Skeleton className="h-24 w-full rounded-lg" />
      ) : !data || data.assignments.length === 0 ? (
        <div className="rounded-lg border bg-card p-6">
          <EmptyState
            icon="key"
            title="No cluster admins assigned"
            description="Anyone with cluster_admin@cluster:* (global) or host_admin still controls this cluster. Add a manual assignment to grant admin rights to a specific user on this cluster only."
          />
        </div>
      ) : (
        <div className="rounded-lg border bg-card overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead className="w-[140px]">Role</TableHead>
                <TableHead className="w-[180px]">Source</TableHead>
                <TableHead className="w-[120px] text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.assignments.map((a, i) => (
                <ClusterAdminRow
                  key={`${a.userId}|${a.roleId}|${a.scope}|${i}`}
                  cid={cid}
                  assignment={a}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {addOpen && (
        <AddClusterAdminDialog
          cid={cid}
          onClose={() => setAddOpen(false)}
        />
      )}
    </section>
  );
}

// Exported for unit tests in __tests__/cluster-admins-row.test.tsx.
export function ClusterAdminRow({
  cid,
  assignment,
}: {
  cid: string;
  assignment: ClusterAdminAssignment;
}) {
  const queryClient = useQueryClient();
  const unassign = useUnassignRole();
  const runWithElevation = useElevationGuard();

  const displayLabel = assignment.displayName?.trim()
    ? `${assignment.displayName} (${assignment.userId})`
    : assignment.userId;

  // Source column: combine the policy-source field (manual/oidc) with
  // the inherited flag. Inherited rows render the badge regardless of
  // whether they were originally manual or oidc — for the operator
  // looking at this view, "inherited from global" is the
  // load-bearing fact.
  let sourceLabel: string;
  let sourceBadgeClass = "text-muted-foreground border-muted-foreground/30";
  if (assignment.inherited) {
    sourceLabel = "inherited from global";
    sourceBadgeClass =
      "text-amber-700 dark:text-amber-400 border-amber-500/40 bg-amber-500/10";
  } else if (assignment.source === "oidc") {
    sourceLabel = "OIDC";
  } else {
    sourceLabel = "manual";
  }

  const handleRevoke = async () => {
    if (
      !confirm(
        `Revoke ${assignment.roleId} from ${assignment.userId} on this cluster?`,
      )
    ) {
      return;
    }
    try {
      await runWithElevation(() =>
        unassign.mutateAsync({
          userId: assignment.userId,
          roleId: assignment.roleId,
          scope: assignment.scope,
        }),
      );
      queryClient.invalidateQueries({
        queryKey: ["admin", "clusters", cid, "admins"],
      });
      // Also refresh /admin/policies cache so the global matrix
      // sees the change without a full reload.
      queryClient.invalidateQueries({ queryKey: ["admin", "policies"] });
      toast.success("Assignment revoked");
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  return (
    <TableRow>
      <TableCell className="font-medium">{displayLabel}</TableCell>
      <TableCell>
        <Badge variant="outline" className="font-mono text-[10px]">
          {assignment.roleId}
        </Badge>
      </TableCell>
      <TableCell>
        <Badge variant="outline" className={`font-mono text-[10px] ${sourceBadgeClass}`}>
          {sourceLabel}
        </Badge>
      </TableCell>
      <TableCell className="text-right">
        {assignment.inherited ? (
          <Tooltip>
            <TooltipTrigger asChild>
              <span>
                <Button variant="destructive" size="sm" disabled>
                  Remove
                </Button>
              </span>
            </TooltipTrigger>
            <TooltipContent className="max-w-xs">
              This assignment is inherited from a global scope (
              <code className="font-mono text-[10px]">{assignment.scope}</code>
              ) and affects more than this cluster. Manage it from{" "}
              <code className="font-mono text-[10px]">/admin/policies</code>.
            </TooltipContent>
          </Tooltip>
        ) : (
          <Button
            variant="destructive"
            size="sm"
            onClick={handleRevoke}
            disabled={unassign.isPending}
          >
            Remove
          </Button>
        )}
      </TableCell>
    </TableRow>
  );
}

// AddClusterAdminDialog — modal with two fields (user + role) per
// the popups-max-2-fields doctrine. Scope is implicit from the
// route param so it doesn't count as a field. User is a dropdown
// of all users from /admin/users (eagerly fetched on dialog open);
// role defaults to `cluster_admin` and offers any non-deprecated
// role from the policy matrix.
function AddClusterAdminDialog({
  cid,
  onClose,
}: {
  cid: string;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const assign = useAssignRole();
  const runWithElevation = useElevationGuard();
  const { data: policies } = usePolicies();

  // Eagerly load the users list so the dropdown has labels. The
  // /admin/users endpoint is uiAdmin-gated, which the operator
  // viewing this page already passes (cluster-detail is admin-only).
  const [users, setUsers] = useState<Array<{ username: string; name?: string }>>(
    [],
  );
  const [usersError, setUsersError] = useState<string | null>(null);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const { data, error } = await client.GET("/admin/users");
        if (cancelled) return;
        if (error || !data) {
          setUsersError("Couldn't load users");
          return;
        }
        setUsers(
          (data as Array<{ username: string; name?: string }>).map((u) => ({
            username: u.username,
            name: u.name,
          })),
        );
      } catch {
        if (!cancelled) setUsersError("Couldn't load users");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const availableRoles: PolicyRole[] = (policies?.roles ?? []).filter(
    (r) => !r.deprecated,
  );
  const defaultRoleId =
    availableRoles.find((r) => r.id === "cluster_admin")?.id ??
    availableRoles[0]?.id ??
    "";

  const [userId, setUserId] = useState("");
  const [roleId, setRoleId] = useState(defaultRoleId);

  // Re-sync the role default once the policies query lands (it's
  // empty on first render).
  useEffect(() => {
    if (!roleId && defaultRoleId) setRoleId(defaultRoleId);
  }, [defaultRoleId, roleId]);

  const handleAssign = async () => {
    if (!userId.trim() || !roleId) {
      toast.error("Pick a user and a role");
      return;
    }
    try {
      await runWithElevation(() =>
        assign.mutateAsync({
          userId: userId.trim(),
          roleId,
          scope: `cluster:${cid}`,
        }),
      );
      queryClient.invalidateQueries({
        queryKey: ["admin", "clusters", cid, "admins"],
      });
      queryClient.invalidateQueries({ queryKey: ["admin", "policies"] });
      toast.success(`Assigned ${roleId} to ${userId} on this cluster`);
      onClose();
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogHeader>
        <DialogTitle>Add cluster admin</DialogTitle>
        <p className="text-sm text-muted-foreground">
          Grant a user admin rights on this cluster only. Scope:{" "}
          <code className="font-mono text-xs">cluster:{cid}</code>.
        </p>
      </DialogHeader>

      <div className="space-y-3">
        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">
            User
          </label>
          {usersError ? (
            <div className="space-y-1">
              <Input
                value={userId}
                onChange={(e) => setUserId(e.target.value)}
                placeholder="Enter username"
              />
              <p className="text-xs text-destructive">{usersError} — enter manually.</p>
            </div>
          ) : users.length === 0 ? (
            <Skeleton className="h-8 w-full" />
          ) : (
            <select
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
              className="h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm"
            >
              <option value="">— pick a user —</option>
              {users.map((u) => (
                <option key={u.username} value={u.username}>
                  {u.name ? `${u.name} (${u.username})` : u.username}
                </option>
              ))}
            </select>
          )}
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-muted-foreground">
            Role
          </label>
          <select
            value={roleId}
            onChange={(e) => setRoleId(e.target.value)}
            className="h-8 w-full rounded-lg border border-input bg-transparent px-2 text-sm"
          >
            {availableRoles.map((r) => (
              <option key={r.id} value={r.id}>
                {r.label || r.id}
              </option>
            ))}
          </select>
          <p className="text-xs text-muted-foreground">
            Most operators want <code className="font-mono">cluster_admin</code>{" "}
            here. Other roles assignable at cluster scope are available too.
          </p>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onClose} disabled={assign.isPending}>
          Cancel
        </Button>
        <Button onClick={handleAssign} disabled={assign.isPending}>
          {assign.isPending ? "Assigning…" : "Assign"}
        </Button>
      </DialogFooter>
    </Dialog>
  );
}
