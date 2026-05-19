import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes, humanizeTime } from "@/shared/lib/format";
import { useBucket } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/buckets/$id")({
  component: adminPage(AdminBucketDetail),
});

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (copied) {
      const timer = setTimeout(() => setCopied(false), 2000);
      return () => clearTimeout(timer);
    }
  }, [copied]);

  return (
    <button
      type="button"
      onClick={() => {
        navigator.clipboard.writeText(text);
        setCopied(true);
      }}
      className="rounded-md p-1.5 hover:bg-muted opacity-60 hover:opacity-100 transition-opacity"
      aria-label="Copy to clipboard"
    >
      {copied ? (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="h-3 w-3 text-green-600"
        >
          <polyline points="20 6 9 17 4 12" />
        </svg>
      ) : (
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
      )}
    </button>
  );
}

function AdminBucketDetail() {
  const { id } = Route.useParams();
  const { data: bucket, isLoading, error } = useBucket(id);

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink />
        <ErrorBanner message="Couldn't load bucket details." />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
          <BackLink />
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-4 w-96" />
          <Card>
            <CardContent className="pt-6">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                {[...Array(3)].map((_, i) => (
                  <div key={i} className="text-center p-4 rounded-lg bg-muted/50">
                    <Skeleton className="h-6 w-24 mx-auto mb-1" />
                    <Skeleton className="h-3 w-16 mx-auto" />
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
          <Card>
            <CardContent className="pt-6">
              <div className="space-y-3 max-w-md">
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-3/4" />
              </div>
            </CardContent>
          </Card>
          <Card>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-1/3">Key ID</TableHead>
                  <TableHead className="w-1/3">Name</TableHead>
                  <TableHead className="w-1/3">Permissions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {[...Array(5)].map((_, i) => (
                  <TableRow key={i}>
                    <TableCell><Skeleton className="h-4 w-full" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                    <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Card>
      </div>
    );
  }

  if (!bucket) {
    return (
      <div className="space-y-6">
        <BackLink />
        <EmptyState
          icon="database"
          title="Bucket not found"
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
            <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
              {bucket.aliases?.[0] ?? bucket.id.slice(0, 12)}
            </h1>
            <div className="flex items-center gap-2 text-xs font-mono text-muted-foreground">
              <span>{bucket.id}</span>
              <CopyButton text={bucket.id} />
              {bucket.aliases && bucket.aliases.length > 1 ? (
                bucket.aliases.slice(1).map((alias) => (
                  <Badge key={alias} variant="secondary" className="text-xs">
                    {alias}
                  </Badge>
                ))
              ) : null}
            </div>
          </div>

          {/* Stats card */}
          <Card>
            <CardContent className="pt-6">
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                <div className="text-center p-4 rounded-lg bg-muted/50">
                  <div className="text-xl font-semibold tabular-nums">
                    {humanizeBytes(bucket.bytes)}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">Size</div>
                </div>
                <div className="text-center p-4 rounded-lg bg-muted/50">
                  <div className="text-xl font-semibold tabular-nums">
                    {(bucket.objects ?? 0).toLocaleString()}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">Objects</div>
                </div>
                <div className="text-center p-4 rounded-lg bg-muted/50">
                  <div className="text-xl font-semibold tabular-nums">
                    {bucket.unfinishedUploads && bucket.unfinishedUploads > 0
                      ? bucket.unfinishedUploads.toLocaleString()
                      : "—"}
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">Unfinished uploads</div>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Quotas card */}
          <Card>
            <CardHeader>Quotas</CardHeader>
            <CardContent className="pt-6">
              <dl className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-w-md">
                <div>
                  <dt className="text-sm text-muted-foreground">Max size</dt>
                  <dd className="font-medium tabular-nums mt-1">
                    {bucket.quotas?.maxSize != null ? (
                      humanizeBytes(bucket.quotas.maxSize)
                    ) : bucket.quotas == null ? (
                      "Unlimited"
                    ) : (
                      "—"
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-sm text-muted-foreground">Max objects</dt>
                  <dd className="font-medium tabular-nums mt-1">
                    {bucket.quotas?.maxObjects != null ? (
                      bucket.quotas.maxObjects.toLocaleString()
                    ) : bucket.quotas == null ? (
                      "Unlimited"
                    ) : (
                      "—"
                    )}
                  </dd>
                </div>
              </dl>
            </CardContent>
          </Card>

          {/* Attached keys table */}
          <Card>
            <CardHeader>Attached keys</CardHeader>
            <CardContent className="pt-6">
              {bucket.keys == null || bucket.keys.length === 0 ? (
                <EmptyState
                  icon="key"
                  title="No keys attached"
                  description="Grant a key access to this bucket from the Keys page."
                />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-1/3">Key ID</TableHead>
                      <TableHead className="w-1/3">Name</TableHead>
                      <TableHead className="w-1/3">Permissions</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {bucket.keys.map((keyAccess) => (
                      <TableRow key={keyAccess.keyId}>
                        <TableCell>
                          <span className="font-mono text-xs">
                            {keyAccess.keyId.slice(0, 12)}…{keyAccess.keyId.slice(-4)}
                          </span>
                        </TableCell>
                        <TableCell>{keyAccess.name ?? "—"}</TableCell>
                        <TableCell>
                          <div className="flex items-center gap-1.5">
                            {keyAccess.read ? (
                              <Badge variant="outline" className="text-[10px] px-1 py-0 h-4 bg-green-50 text-green-700 border-green-200">R</Badge>
                            ) : (
                              <span className="text-xs text-muted-foreground opacity-30">R</span>
                            )}
                            {keyAccess.write ? (
                              <Badge variant="outline" className="text-[10px] px-1 py-0 h-4 bg-blue-50 text-blue-700 border-blue-200">W</Badge>
                            ) : (
                              <span className="text-xs text-muted-foreground opacity-30">W</span>
                            )}
                            {keyAccess.owner ? (
                              <Badge variant="outline" className="text-[10px] px-1 py-0 h-4 bg-amber-50 text-amber-700 border-amber-200">O</Badge>
                            ) : (
                              <span className="text-xs text-muted-foreground opacity-30">O</span>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>

          {/* Created */}
          {bucket.created && humanizeTime(bucket.created) !== "—" && (
            <p className="text-xs text-muted-foreground">Created {humanizeTime(bucket.created)}</p>
          )}
    </div>
  );
}

function BackLink() {
  return (
    <Link
      to="/admin"
      className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
    >
      ← Buckets
    </Link>
  );
}
