import { createFileRoute } from "@tanstack/react-router";
import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";
import { useKey } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/keys/$id")({
  component: adminPage(KeyDetailScreen),
});

function BackLink() {
  return (
    <Link to="/admin/keys" className="text-sm text-muted-foreground hover:text-foreground inline-flex items-center gap-1">
      ← Keys
    </Link>
  );
}

function PermissionChips({ read, write, owner }: { read: boolean; write: boolean; owner: boolean }) {
  const chips = [];
  if (owner) chips.push(<Badge key="owner" variant="destructive" className="text-xs">Owner</Badge>);
  if (write) chips.push(<Badge key="write" variant="secondary" className="text-xs">Write</Badge>);
  if (read) chips.push(<Badge key="read" variant="outline" className="text-xs">Read</Badge>);
  return <div className="flex gap-1">{chips}</div>;
}

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
  const { id } = Route.useParams();
  const { data: key, isLoading, error } = useKey(id);

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

  return (
    <div className="space-y-6">
      <BackLink />

      {/* Header */}
      <div className="space-y-2">
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{key.name ?? "Unnamed key"}</h1>
        <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground">
          <span>{key.accessKeyId}</span>
          <button
            type="button"
            onClick={() => navigator.clipboard.writeText(key.accessKeyId ?? key.id)}
            className="rounded-md p-1.5 hover:bg-muted opacity-60 hover:opacity-100 transition-opacity"
            aria-label="Copy to clipboard"
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
        <CardHeader>Bucket access</CardHeader>
        <CardContent className="pt-6">
          {key.buckets && key.buckets.length > 0 ? (
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
          ) : (
            <EmptyState
              icon="database"
              title="No bucket access"
              description="This key has no permissions on any bucket."
            />
          )}
        </CardContent>
      </Card>
    </div>
  );
}
