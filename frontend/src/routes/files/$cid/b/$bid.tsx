import { createFileRoute } from "@tanstack/react-router";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { useUserClusterBuckets, useUserObjects, useUserPresignGet } from "@/shared/api/queries";
import { ObjectRow } from "@/components/objects/ObjectRow";
import { UploadDialog } from "@/components/upload/UploadDialog";

export const Route = createFileRoute("/files/$cid/b/$bid")({
  component: UserBucketObjects,
});

function UserBucketObjects() {
  const { cid, bid } = Route.useParams();
  const navigate = useNavigate();

  // Use window.location.search directly — TanStack Router's
  // useSearch generic API has stricter type constraints in newer
  // versions. URL params are simple here so plain URLSearchParams
  // works and keeps tsc happy across router-major-version bumps.
  const urlParams = new URLSearchParams(typeof window !== "undefined" ? window.location.search : "");
  const prefix = urlParams.get("prefix") ?? "";
  const token = urlParams.get("token") ?? "";

  const { data: bucketsData, isLoading: bucketsLoading } = useUserClusterBuckets(cid);
  const bucket = bucketsData?.find((b) => b.id === bid);
  const bucketAlias = bucket?.aliases?.[0] || "(no alias)";

  const { data: objectsPage, isLoading: objectsLoading, refetch } = useUserObjects(
    cid,
    bid,
    prefix,
    token,
  );

  const presignMutation = useUserPresignGet(cid, bid);

  const handleFolderClick = (folderPrefix: string) => {
    navigate({
      to: "/files/$cid/b/$bid",
      params: { cid, bid },
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
    navigate({ to: "/files/$cid", params: { cid } });
  };

  const [uploadOpen, setUploadOpen] = useState(false);

  const handleUploadSuccess = () => {
    setUploadOpen(false);
    refetch(); // Invalidate and refresh object list
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
          description="The bucket you are looking for does not exist or you do not have access to it."
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
        <Breadcrumb prefix={prefix} onNavigate={(p) => navigate({ to: "/files/$cid/b/$bid", params: { cid, bid }, search: { prefix: p, token: "" } })} />
      )}

      {objectsLoading ? (
        <BucketListSkeleton />
      ) : objectsPage?.objects.length === 0 && (!objectsPage?.prefixes || objectsPage.prefixes.length === 0) ? (
        <EmptyState
          icon="folder-open"
          title="No objects here"
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
              {objectsPage?.prefixes && objectsPage.prefixes.length > 0 && (
                <>
                  {objectsPage.prefixes.map((folder) => {
                    const folderKey = prefix ? `${prefix}${folder}` : folder;
                    return (
                      <ObjectRow
                        key={folderKey}
                        object={{ key: folderKey, size: 0, last_modified: new Date().toISOString() }}
                        isFolder
                        onFolderClick={handleFolderClick}
                      />
                    );
                  })}
                </>
              )}

              {objectsPage?.objects && objectsPage.objects.map((obj) => (
                <ObjectRow
                  key={obj.key}
                  object={obj}
                  onDownload={handleDownload}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {objectsPage?.isTruncated && (
        <div className="flex justify-center">
          <Button onClick={() => navigate({ to: "/files/$cid/b/$bid", params: { cid, bid }, search: { prefix, token: objectsPage.nextContinuation ?? "" } })}>
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
        cid={cid}
        bid={bid}
        prefix={prefix}
        onSuccess={handleUploadSuccess}
      />
    </div>
  );
}

function PageHeader({ title, description, actions }: { title: React.ReactNode; description?: string; actions?: React.ReactNode }) {
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

function Breadcrumb({ prefix, onNavigate }: { prefix: string; onNavigate: (p: string) => void }) {
  const parts = prefix.split("/").filter(Boolean);
  
  return (
    <nav className="flex items-center gap-2 text-sm text-muted-foreground">
      <button onClick={() => onNavigate("")} className="hover:text-foreground">
        {parts[0] ? ".." : "Root"}
      </button>
      {parts.map((part, idx) => {
        const path = parts.slice(0, idx + 1).join("/");
        return (
          <span key={path} className="flex items-center gap-2">
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4">
              <path d="m9 18 6-6-6-6"/>
            </svg>
            <button onClick={() => onNavigate(path)} className="hover:text-foreground">
              {part}
            </button>
          </span>
        );
      })}
    </nav>
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
