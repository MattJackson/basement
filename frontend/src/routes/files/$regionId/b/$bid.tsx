import { createFileRoute, useNavigate, useLocation } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import {
  useUserRegionBuckets,
  useUserRegionObjects,
  useUserRegionPresignGet,
} from "@/shared/api/queries";
import { ObjectRow } from "@/components/objects/ObjectRow";
import { UploadDialog } from "@/components/upload/UploadDialog";

// Object browser for buckets reached via a UserRegion (ADR-0002, v1.1.0c).
// Same shell + table layout as the old /files/$cid/b/$bid route — the
// only material difference is that every backend call goes through
// /api/v1/user/regions/{regionId}/buckets/{bid}/* and is signed with
// the region's S3 key. We deliberately do NOT call a per-bucket
// useUserBucket() — there is no per-bucket basement record at the
// region tier; ListBuckets via the user's key is authoritative.
export const Route = createFileRoute("/files/$regionId/b/$bid")({
  component: UserRegionBucketObjects,
});

function UserRegionBucketObjects() {
  const { regionId, bid } = Route.useParams();
  const navigate = useNavigate();

  // Read search params via useLocation() so the component re-renders
  // when navigate() updates the URL with a new prefix (folder click).
  // The previous URLSearchParams read at module load was non-reactive,
  // which broke folder navigation in v1.3.0c.1 — clicking a folder
  // updated the URL but the prefix stayed at "".
  const location = useLocation();
  const urlParams = new URLSearchParams(location.searchStr || "");
  const prefix = urlParams.get("prefix") ?? "";
  const token = urlParams.get("token") ?? "";

  const { data: bucketsData, isLoading: bucketsLoading } = useUserRegionBuckets(regionId);
  const bucket = bucketsData?.find((b) => b.id === bid);
  const bucketAlias = bucket?.aliases?.[0] || bid;

  const {
    data: objectsPage,
    isLoading: objectsLoading,
    error: objectsError,
    refetch,
  } = useUserRegionObjects(regionId, bid, prefix, token);

  const presignMutation = useUserRegionPresignGet(regionId, bid);

  const handleFolderClick = (folderPrefix: string) => {
    navigate({
      to: "/files/$regionId/b/$bid",
      params: { regionId, bid },
      search: { prefix: folderPrefix, token: "" },
    });
  };

  const handleDownload = async (key: string) => {
    presignMutation.mutateAsync({ key, ttl: 3600 }).then((presignedUrl) => {
      window.open(presignedUrl.url, "_blank");
    });
  };

  const handleRefresh = () => {
    refetch();
  };

  const handleBack = () => {
    navigate({ to: "/files/$regionId", params: { regionId } });
  };

  const [uploadOpen, setUploadOpen] = useState(false);

  const handleUploadSuccess = () => {
    setUploadOpen(false);
    refetch();
  };

  if (bucketsLoading) {
    return (
      <div className="space-y-6">
        <PageHeader title={<Skeleton className="h-8 w-48" />} actions={null} />
        <BucketListSkeleton />
      </div>
    );
  }

  if (!bucket) {
    return (
      <div className="space-y-6">
        <PageHeader title={`Bucket not found`} actions={<Button onClick={handleBack}>Back</Button>} />
        <EmptyState
          icon="alert-circle"
          title="Bucket not found"
          description="The bucket you are looking for does not exist or your S3 key cannot see it."
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={bucketAlias}
        description={`Objects in ${bucketAlias}`}
        actions={
          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Button variant="outline" onClick={handleBack}>
              Back
            </Button>
            <Button variant="ghost" size="sm" onClick={handleRefresh} disabled={objectsLoading}>
              Refresh
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={() => setUploadOpen(true)}
              disabled={objectsLoading}
            >
              Upload
            </Button>
          </div>
        }
      />

      {prefix && (
        <Breadcrumb
          bucketAlias={bucketAlias}
          prefix={prefix}
          onNavigate={(p) =>
            navigate({
              to: "/files/$regionId/b/$bid",
              params: { regionId, bid },
              search: { prefix: p, token: "" },
            })
          }
        />
      )}

      {objectsLoading ? (
        <BucketListSkeleton />
      ) : objectsError ? (
        <EmptyState
          icon="alert-circle"
          title="Can't browse objects"
          description={`Backend returned an error: ${String(objectsError)}`}
        />
      ) : objectsPage?.objects.length === 0 &&
        (!objectsPage?.commonPrefixes || objectsPage.commonPrefixes.length === 0) ? (
        <EmptyState
          icon="folder-open"
          title={prefix ? "This folder is empty" : "No objects here"}
          description={prefix ? `No objects found in ${prefix}` : "This bucket is empty"}
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead className="text-right w-[140px]">Size</TableHead>
                <TableHead className="text-right w-[160px]">Last modified</TableHead>
                <TableHead className="w-24">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {objectsPage?.commonPrefixes && objectsPage.commonPrefixes.length > 0 && (
                <>
                  {[...objectsPage.commonPrefixes].sort().map((folder) => (
                    // S3 returns each CommonPrefix as a FULL prefix
                    // (e.g. "raw/broadcom-docid/" when listing
                    // "raw/" with delimiter="/"). Pass it through as
                    // the key — do not concatenate with the current
                    // prefix or you'll double-up.
                    <ObjectRow
                      key={folder}
                      object={{ key: folder, size: 0, last_modified: new Date().toISOString() }}
                      isFolder
                      onFolderClick={handleFolderClick}
                    />
                  ))}
                </>
              )}

              {objectsPage?.objects &&
                objectsPage.objects.map((obj) => (
                  <ObjectRow key={obj.key} object={obj} onDownload={handleDownload} />
                ))}
            </TableBody>
          </Table>
        </div>
      )}

      {objectsPage?.isTruncated && (
        <div className="flex flex-col items-center gap-2">
          <p className="text-sm text-muted-foreground">
            Showing first {objectsPage.objects.length} — more available
          </p>
          <Button
            onClick={() =>
              navigate({
                to: "/files/$regionId/b/$bid",
                params: { regionId, bid },
                search: { prefix, token: objectsPage.nextContinuation ?? "" },
              })
            }
          >
            Load more
          </Button>
        </div>
      )}

      {presignMutation.isError && (
        <ErrorBanner message="Failed to generate download link. Try again." />
      )}

      <UploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        regionId={regionId}
        bid={bid}
        prefix={prefix}
        onSuccess={handleUploadSuccess}
      />
    </div>
  );
}

function PageHeader({
  title,
  description,
  actions,
}: {
  title: React.ReactNode;
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

// Breadcrumb renders bucketAlias > raw > broadcom-docid (clickable),
// per v1.3.0c.1: each crumb navigates to the prefix ending at that
// segment. Folder prefixes from S3 always end in "/" so the click
// emits e.g. "raw/" or "raw/broadcom-docid/".
function Breadcrumb({
  bucketAlias,
  prefix,
  onNavigate,
}: {
  bucketAlias: string;
  prefix: string;
  onNavigate: (p: string) => void;
}) {
  const parts = prefix.split("/").filter(Boolean);
  const parentPrefix = parts.length > 1 ? parts.slice(0, parts.length - 1).join("/") + "/" : "";

  return (
    <div className="flex flex-col gap-2">
      <nav className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        <button onClick={() => onNavigate("")} className="hover:text-foreground font-medium">
          {bucketAlias}
        </button>
        {parts.map((part, idx) => {
          const path = parts.slice(0, idx + 1).join("/") + "/";
          const isLast = idx === parts.length - 1;
          return (
            <span key={path} className="flex items-center gap-2">
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
                <path d="m9 18 6-6-6-6" />
              </svg>
              {isLast ? (
                <span className="text-foreground">{part}</span>
              ) : (
                <button onClick={() => onNavigate(path)} className="hover:text-foreground">
                  {part}
                </button>
              )}
            </span>
          );
        })}
      </nav>
      <button
        type="button"
        onClick={() => onNavigate(parentPrefix)}
        className="self-start text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="h-3.5 w-3.5"
        >
          <path d="m15 18-6-6 6-6" />
        </svg>
        Up to parent folder
      </button>
    </div>
  );
}

function BucketListSkeleton() {
  return (
    <div className="rounded-lg border bg-card overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead className="text-right w-[140px]">Size</TableHead>
            <TableHead className="text-right w-[160px]">Last modified</TableHead>
            <TableHead className="w-24">Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {[...Array(5)].map((_, i) => (
            <TableRow key={i}>
              <TableCell><Skeleton className="h-4 w-64" /></TableCell>
              <TableCell><Skeleton className="h-4 w-16 ml-auto" /></TableCell>
              <TableCell><Skeleton className="h-4 w-32 ml-auto" /></TableCell>
              <TableCell><Skeleton className="h-8 w-20" /></TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
