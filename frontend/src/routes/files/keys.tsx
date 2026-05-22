import { createFileRoute, Link } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";
import { useUserRegions } from "@/shared/api/queries";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";

export const Route = createFileRoute("/files/keys")({
  component: userPage(KeysHome),
});

// /files/keys was the legacy fan-out view of S3 keys across every
// cluster a user had a BucketGrant on. ADR-0002 retired the
// BucketGrant model and replaced "cluster a user has access to" with
// "endpoint the user has a key for" — so /files/keys is now a
// keychain view: one card per UserRegion, with the access key ID +
// endpoint shown. The actual secret never leaves the server.
function KeysHome() {
  const { data: regions, isLoading, error } = useUserRegions();
  const list = regions ?? [];

  if (error) {
    return (
      <div className="space-y-6">
        <Header />
        <ErrorBanner message="Couldn&apos;t connect to backend. Retrying automatically..." />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <Header />
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(3)].map((_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-4 w-40" />
                <Skeleton className="h-3 w-56" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-3 w-32" />
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    );
  }

  if (list.length === 0) {
    return (
      <div className="space-y-6">
        <Header />
        <EmptyState
          icon="key"
          title="No keys yet"
          description="Add a region (and S3 credentials) to put a key on your keychain."
          action={
            <Link
              to="/files/regions/new"
              className="inline-flex items-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            >
              + Add region
            </Link>
          }
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <Header />
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {list.map((r) => (
          <Card key={r.id} data-testid="user-key-card">
            <CardHeader>
              <CardTitle className="truncate">{r.alias || r.endpoint}</CardTitle>
              <CardDescription className="space-y-1">
                <div className="truncate text-xs">{r.endpoint}</div>
                {r.region && (
                  <div className="text-xs">region: {r.region}</div>
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-1">
              <div className="text-xs text-muted-foreground">Access key</div>
              <div className="font-mono text-sm break-all">{r.accessKeyId}</div>
              {r.lastUsedAt && (
                <div className="pt-2 text-xs text-muted-foreground">
                  Last used {new Date(r.lastUsedAt).toLocaleString()}
                </div>
              )}
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}

function Header() {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Keys</h1>
        <p className="text-sm text-muted-foreground mt-1">
          S3 credentials for the regions on your keychain
        </p>
      </div>
    </header>
  );
}

export default KeysHome;
