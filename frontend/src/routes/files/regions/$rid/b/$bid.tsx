import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { useUserRegion, useUserRegionObjects, useUserRegionPresignGet } from "@/shared/api/queries";
import { ObjectRow } from "@/components/objects/ObjectRow";

// /files/regions/$rid/b/$bid — object browser inside one bucket of
// one UserRegion. All S3 ops are signed with the region's S3 key on
// the server side, attributing activity in backend audit logs to the
// user's identity per ADR-0002.
export const Route = createFileRoute("/files/regions/$rid/b/$bid")({
  component: RegionBucketObjects,
});

function RegionBucketObjects() {
  const { rid, bid } = Route.useParams();
  const navigate = useNavigate();

  // window.location.search direct read keeps tsc happy across
  // TanStack Router majors (same pattern the legacy $cid route used).
  const urlParams = new URLSearchParams(typeof window !== "undefined" ? window.location.search : "");
  const prefix = urlParams.get("prefix") ?? "";
  const token = urlParams.get("token") ?? "";

  const { data: region } = useUserRegion(rid);
  const { data: objectsPage, isLoading: objectsLoading, error: objectsError, refetch } = useUserRegionObjects(
    rid,
    bid,
    prefix,
    token,
  );
  const presign = useUserRegionPresignGet(rid, bid);

  const title = region?.alias ? `${region.alias} • ${bid}` : bid;

  const handleFolderClick = (folderPrefix: string) => {
    navigate({
      to: "/files/regions/$rid/b/$bid",
      params: { rid, bid },
      search: { prefix: folderPrefix, token: "" },
    });
  };

  const handleDownload = async (key: string) => {
    const url = await presign.mutateAsync({ key, ttl: 3600 });
    if (url?.url) window.open(url.url, "_blank");
  };

  return (
    <div className="space-y-6">
      <Link to="/files/regions/$rid" params={{ rid }} className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        &larr; Back to bucket list
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{title}</h1>
          {region && (
            <p className="text-sm text-muted-foreground mt-1">{region.endpoint}</p>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={() => refetch()} disabled={objectsLoading}>
            Refresh
          </Button>
        </div>
      </header>

      {prefix && (
        <Breadcrumb prefix={prefix} onNavigate={(p) => navigate({ to: "/files/regions/$rid/b/$bid", params: { rid, bid }, search: { prefix: p, token: "" } })} />
      )}

      {objectsError ? (
        <EmptyState
          icon="alert-circle"
          title="Can't browse objects"
          description={`Backend returned an error: ${String(objectsError)}`}
        />
      ) : objectsLoading ? (
        <ObjectListSkeleton />
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
              {objectsPage?.prefixes?.map((folder) => {
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
              {objectsPage?.objects?.map((obj) => (
                <ObjectRow key={obj.key} object={obj} onDownload={handleDownload} />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {objectsPage?.isTruncated && (
        <div className="flex justify-center">
          <Button onClick={() => navigate({ to: "/files/regions/$rid/b/$bid", params: { rid, bid }, search: { prefix, token: objectsPage.nextContinuation ?? "" } })}>
            Load more
          </Button>
        </div>
      )}

      {presign.isError && (
        <ErrorBanner message="Failed to generate download link. Try again." />
      )}
    </div>
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

function ObjectListSkeleton() {
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
