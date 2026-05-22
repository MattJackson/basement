import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes } from "@/shared/lib/format";
import {
  useDeleteUserRegion,
  useUserRegion,
  useUserRegionBuckets,
  useUserRegions,
} from "@/shared/api/queries";

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
  const deleteRegion = useDeleteUserRegion();

  const noRegions = regions !== undefined && regions.length === 0;
  if (noRegions) {
    navigate({ to: "/files", replace: true });
  }

  const activeRegion = region ?? regions?.find((r) => r.id === regionId);
  // v1.4.0a: the bucket-list hook now returns an envelope carrying
  // the list + a per-driver capability flag. When the driver can't
  // surface Objects + Bytes (Garage v1 today), the table hides
  // those columns entirely rather than render em-dash rows.
  const buckets = bucketsData?.buckets ?? [];
  const statsAvailable = bucketsData?.perBucketStatsAvailable ?? false;

  // v1.3.0a.1: the backend now returns 401 USER_KEY_REJECTED (with the
  // region detail in the error payload) when the access key the region
  // is bound to was revoked / rotated on the backend. The query hook's
  // `apiError` augments the Error with a `.code` field, so we can
  // branch off that to render an actionable alert instead of the
  // generic "Can't list buckets — internal error" fallthrough.
  const bucketsErrCode =
    bucketsError && (bucketsError as Error & { code?: string }).code;
  const isKeyRejected = bucketsErrCode === "USER_KEY_REJECTED";

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

      {isKeyRejected ? (
        <KeyRejectedAlert
          onDelete={async () => {
            await deleteRegion.mutateAsync(regionId);
            navigate({ to: "/files/keys", replace: true });
          }}
          isDeleting={deleteRegion.isPending}
          deleteError={deleteRegion.error?.message}
        />
      ) : bucketsError ? (
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
                {statsAvailable && (
                  <>
                    <TableHead className="text-right w-[140px]">Size</TableHead>
                    <TableHead className="text-right w-[120px]">Objects</TableHead>
                  </>
                )}
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
                  statsAvailable={statsAvailable}
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
  statsAvailable,
}: {
  regionId: string;
  bucketId: string;
  aliases: string[];
  bytes: number;
  objects: number;
  statsAvailable: boolean;
}) {
  const primaryAlias = aliases[0];
  const onNavigate = () => {
    window.location.href = `/files/${regionId}/b/${bucketId}`;
  };

  // ADR-0002 deliberately drops the per-bucket "stat me" hydration
  // pattern (the old /files/$cid/index relied on a useUserBucket()
  // per row to fill in bytes/objects, because Garage's ListBuckets
  // returns zeros). v1.4.0a takes the next honest step: if the driver
  // has told us PerBucketStatsAvailable is false, we don't render
  // Size + Objects columns at all — a row of em-dashes was confusing
  // operators ("is this bucket empty, or does basement not know?").
  return (
    <TableRow className="cursor-pointer hover:bg-muted/50" onClick={onNavigate}>
      <TableCell>
        {primaryAlias ? (
          <span className="font-medium">{primaryAlias}</span>
        ) : (
          <span className="text-sm italic text-muted-foreground">{bucketId}</span>
        )}
      </TableCell>
      {statsAvailable && (
        <>
          <TableCell className="text-right tabular-nums">
            {bytes > 0 ? humanizeBytes(bytes) : <span className="text-muted-foreground">{"—"}</span>}
          </TableCell>
          <TableCell className="text-right tabular-nums">
            {objects > 0 ? objects.toLocaleString() : <span className="text-muted-foreground">{"—"}</span>}
          </TableCell>
        </>
      )}
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

// KeyRejectedAlert is the v1.3.0a.1 inline UI shown when the region
// endpoints return USER_KEY_REJECTED (401). It tells the user the key
// stored in basement is no longer valid at the backend and gives
// them two ways out: delete the bad region in-place, or jump to the
// keychain to add a fresh key. Renders red but is not a toast — the
// user lands on this region's URL expecting buckets, so a full panel
// is the right weight.
function KeyRejectedAlert({
  onDelete,
  isDeleting,
  deleteError,
}: {
  onDelete: () => void | Promise<void>;
  isDeleting: boolean;
  deleteError?: string;
}) {
  return (
    <div
      className="rounded-lg border border-destructive/40 bg-destructive/5 p-6 space-y-4"
      data-testid="user-key-rejected-alert"
      role="alert"
    >
      <div className="space-y-2">
        <h2 className="text-lg font-semibold text-destructive">
          Your access key was rejected
        </h2>
        <p className="text-sm text-foreground">
          The backend no longer accepts the access key stored for this
          region. The key was probably revoked, rotated, or never existed
          on this cluster. basement can&apos;t list any buckets until
          you swap in a working key.
        </p>
      </div>

      {deleteError ? (
        <p className="text-sm text-destructive">{deleteError}</p>
      ) : null}

      <div className="flex flex-col sm:flex-row gap-2">
        <Button
          variant="destructive"
          onClick={onDelete}
          disabled={isDeleting}
          data-testid="delete-rejected-region-button"
        >
          {isDeleting ? "Deleting..." : "Delete this region"}
        </Button>
        <Link to="/files/keys/new">
          <Button variant="outline" data-testid="add-new-key-link">
            Add a fresh key
          </Button>
        </Link>
      </div>
    </div>
  );
}

// BucketListSkeleton always renders the three-column skeleton: at
// load time we don't yet know whether the driver supports stats, and
// showing a wider skeleton avoids the table flickering narrower once
// the real data arrives on a no-stats driver. The Bucket column on
// its own is the dominant width regardless.
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
