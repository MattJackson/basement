import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes } from "@/shared/lib/format";
import { useUserRegion, useUserRegionBuckets, useUserRegions } from "@/shared/api/queries";

// Bucket list for a single UserRegion (ADR-0002, v1.1.0c). Replaces
// /files/$cid/index. Calls ListBuckets via the user's region key —
// the backend returns whatever buckets that key can see, so the UI
// stops asking "which buckets does this user have a grant on?"; it
// just renders what the backend returns.
//
// NOTE: do NOT wrap in userPage() — the parent layout
// (routes/files/$regionId.tsx) already does that.
export const Route = createFileRoute("/files/$regionId/")({
  component: RegionBuckets,
});

function RegionBuckets() {
  const { regionId } = Route.useParams();
  const navigate = useNavigate();

  const { data: regions, error: regionsError } = useUserRegions();
  const { data: region } = useUserRegion(regionId);
  const { data: bucketsData, isLoading, error: bucketsError } = useUserRegionBuckets(regionId);

  const noRegions = regions !== undefined && regions.length === 0;
  if (noRegions) {
    navigate({ to: "/files", replace: true });
  }

  const activeRegion = region ?? regions?.find((r) => r.id === regionId);
  const buckets = bucketsData ?? [];

  // Bucket-list errors (e.g. expired key, endpoint unreachable) get
  // surfaced inline instead of forcing the user back to /files — they
  // might just need to retry, and we don't want to lose their context.
  if (regionsError) {
    return (
      <div className="space-y-6">
        <PageHeader
          title={activeRegion?.alias ?? "Buckets"}
          description="Storage you have access to"
          actions={<Button variant="outline" onClick={() => window.location.reload()}>Retry</Button>}
        />
        <ErrorBanner message="Couldn't load regions. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={activeRegion?.alias ?? "Buckets"}
        description={
          activeRegion
            ? `Buckets your key can see at ${hostnameOf(activeRegion.endpoint)}`
            : "Loading region..."
        }
        actions={
          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Button variant="outline" onClick={() => navigate({ to: "/files" })}>
              Back
            </Button>
          </div>
        }
      />

      {bucketsError ? (
        <EmptyState
          icon="alert-circle"
          title="Can't list buckets"
          description={`Backend returned an error: ${String(bucketsError)}`}
        />
      ) : isLoading ? (
        <BucketListSkeleton />
      ) : buckets.length === 0 ? (
        <EmptyState
          icon="database"
          title="No buckets yet"
          description="Your S3 key can't see any buckets at this endpoint yet. Ask your cluster admin to grant the key access to a bucket."
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Bucket</TableHead>
                <TableHead className="text-right w-[140px]">Size</TableHead>
                <TableHead className="text-right w-[120px]">Objects</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {buckets.map((bucket) => (
                <BucketRow
                  key={`${regionId}:${bucket.id}`}
                  regionId={regionId}
                  bucketId={bucket.id}
                  aliases={bucket.aliases ?? []}
                  bytes={bucket.bytes ?? 0}
                  objects={bucket.objects ?? 0}
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
  regionId,
  bucketId,
  aliases,
  bytes,
  objects,
}: {
  regionId: string;
  bucketId: string;
  aliases: string[];
  bytes: number;
  objects: number;
}) {
  const primaryAlias = aliases[0];
  const onNavigate = () => {
    window.location.href = `/files/${regionId}/b/${bucketId}`;
  };

  // ADR-0002 deliberately drops the per-bucket "stat me" hydration
  // pattern (the old /files/$cid/index relied on a useUserBucket()
  // per row to fill in bytes/objects, because Garage's ListBuckets
  // returns zeros). At the region tier we don't have a per-bucket
  // detail endpoint yet — the user's key is the grant, not a
  // basement record. Until the backend gains a region-tier
  // GetBucket, the table just shows whatever ListBuckets returned.
  return (
    <TableRow className="cursor-pointer hover:bg-muted/50" onClick={onNavigate}>
      <TableCell>
        {primaryAlias ? (
          <span className="font-medium">{primaryAlias}</span>
        ) : (
          <span className="text-sm italic text-muted-foreground">{bucketId}</span>
        )}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {bytes > 0 ? humanizeBytes(bytes) : <span className="text-muted-foreground">{"—"}</span>}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {objects > 0 ? objects.toLocaleString() : <span className="text-muted-foreground">{"—"}</span>}
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
          </TableRow>
        </TableHeader>
        <TableBody>
          {[...Array(5)].map((_, i) => (
            <TableRow key={i}>
              <TableCell><Skeleton className="h-4 w-48" /></TableCell>
              <TableCell><Skeleton className="h-4 w-16 ml-auto" /></TableCell>
              <TableCell><Skeleton className="h-4 w-12 ml-auto" /></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

// hostnameOf strips the scheme + path off an endpoint URL so the
// description line stays compact ("Buckets your key can see at
// s3.example.com"). Falls back to the raw endpoint if parsing fails.
function hostnameOf(endpoint: string): string {
  try {
    return new URL(endpoint).host;
  } catch {
    return endpoint;
  }
}
