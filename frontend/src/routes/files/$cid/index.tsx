import { createFileRoute } from "@tanstack/react-router";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes } from "@/shared/lib/format";
import { useUserClusterBuckets, useUserClusters } from "@/shared/api/queries";
// NOTE: do NOT wrap in userPage() — the parent layout
// (routes/files/$cid.tsx) already wraps Outlet in userPage().
// Wrapping again here would double-render UserShell (caught in
// v0.8.0d.7 — operator saw two headers stacked).
export const Route = createFileRoute("/files/$cid/")({
  component: ClusterBuckets,
});

function ClusterBuckets() {
  const { cid } = Route.useParams();
  const navigate = useNavigate();

  const { data: clusters, error: clustersError } = useUserClusters();
  const { data: bucketsData, isLoading } = useUserClusterBuckets(cid);

  const noClusters = clusters !== undefined && clusters.length === 0;
  if (noClusters) {
    navigate({ to: "/files", replace: true });
  }
  const activeCluster = clusters?.find((c) => c.id === cid);
  
  const buckets = bucketsData ?? [];

  if (clustersError) {
    return (
      <div className="space-y-6">
        <PageHeader
          title={activeCluster?.label ?? "Buckets"}
          description="Storage you have access to"
          actions={<Button variant="outline" onClick={() => window.location.reload()}>Retry</Button>}
        />
        <ErrorBanner message="Couldn't connect to backend. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={activeCluster?.label ?? "Buckets"}
        description="Buckets you can access in this cluster"
        actions={
          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Button
              variant="outline"
              onClick={() => navigate({ to: "/files" })}
            >
              Back
            </Button>
          </div>
        }
      />

      {isLoading ? (
        <BucketListSkeleton />
      ) : buckets.length === 0 ? (
        <EmptyState
          icon="database"
          title="No buckets yet"
          description="No buckets accessible in this cluster yet. Contact your administrator."
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Bucket</TableHead>
                <TableHead className="text-right w-[140px]">Size</TableHead>
                <TableHead className="text-right w-[120px]">Objects</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {buckets.map((bucket) => (
                <BucketRow
                  key={`${cid}:${bucket.id}`}
                  cid={cid}
                  bucketId={bucket.id}
                  fallbackAliases={bucket.aliases ?? []}
                  onNavigate={() => {
                    window.location.href = `/files/${cid}/b/${bucket.id}`;
                  }}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function BucketRow({
  cid,
  bucketId,
  fallbackAliases,
  onNavigate,
}: {
  cid: string;
  bucketId: string;
  fallbackAliases: string[];
  onNavigate: () => void;
}) {
  const { data: detail } = useUserClusterBuckets(cid);
  const bucketDetail = detail?.find((b) => b.id === bucketId);
  
  // Fallback to fetching detail if not in list response (hydration pattern)
  const actualDetail = bucketDetail; 
  const aliases = actualDetail?.aliases ?? fallbackAliases;
  const primaryAlias = aliases[0];

  return (
    <TableRow className="cursor-pointer hover:bg-muted/50" onClick={onNavigate}>
      <TableCell>
        {primaryAlias ? (
          <span className="font-medium">{primaryAlias}</span>
        ) : (
          <span className="text-sm italic text-muted-foreground">(no alias)</span>
        )}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {actualDetail ? humanizeBytes(actualDetail.bytes) : <Skeleton className="h-3 w-12 ml-auto" />}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {actualDetail ? (actualDetail.objects ?? 0).toLocaleString() : <Skeleton className="h-3 w-8 ml-auto" />}
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
            <DropdownMenuItem
              onClick={(e) => {
                e.stopPropagation();
                onNavigate();
              }}
            >
              Browse files
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: React.ReactNode;
}) {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-muted-foreground mt-1">{description}</p>}
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
            <TableHead>Bucket</TableHead>
            <TableHead className="text-right w-[140px]">Size</TableHead>
            <TableHead className="text-right w-[120px]">Objects</TableHead>
            <TableHead className="w-16">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {[...Array(5)].map((_, i) => (
            <TableRow key={i}>
              <TableCell><Skeleton className="h-4 w-48" /></TableCell>
              <TableCell><Skeleton className="h-4 w-16 ml-auto" /></TableCell>
              <TableCell><Skeleton className="h-4 w-12 ml-auto" /></TableCell>
              <TableCell><Skeleton className="h-8 w-8 rounded" /></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
