import { createFileRoute } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";
import { useUserRegions } from "@/shared/api/queries";
import { UserRegionCard } from "@/components/regions/UserRegionCard";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";

// /files = "My Regions" (ADR-0002, v1.1.0c). Was "My Clusters" — but
// the user's mental model is now "I own a keychain of (endpoint, key)
// pairs called Regions." Cluster is an admin concept; admins browse
// clusters via /admin/clusters/{cid}. See ADR-0002 for the why.
export const Route = createFileRoute("/files/")({
  component: userPage(FilesHome),
});

function FilesHome() {
  const { data: regionsData, isLoading, error } = useUserRegions();
  const regions = regionsData ?? [];

  if (error) {
    return (
      <div className="space-y-6">
        <Header />
        <ErrorBanner message="Couldn&apos;t load your regions. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <Header
        action={
          regions.length > 0 ? (
            <a
              href="/files/regions/new"
              className="inline-flex items-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            >
              + Connect a region
            </a>
          ) : null
        }
      />

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="rounded-xl border bg-card p-4 space-y-3">
              <div className="flex items-center gap-3">
                <Skeleton className="h-3 w-3 rounded-full" />
                <Skeleton className="h-4 w-32 flex-1" />
              </div>
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-24" />
            </div>
          ))}
        </div>
      ) : regions.length === 0 ? (
        <EmptyState
          icon="server"
          title="No regions yet"
          description="Connect to a region with an S3 access key from your cluster admin."
          action={
            <a
              href="/files/regions/new"
              className="inline-flex items-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            >
              + Connect a region
            </a>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {regions.map((region) => (
            <UserRegionCard key={region.id} region={region} />
          ))}
        </div>
      )}
    </div>
  );
}

function Header({ action }: { action?: React.ReactNode }) {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Regions</h1>
        <p className="text-sm text-muted-foreground mt-1">
          S3 endpoints you have a key for
        </p>
      </div>
      {action && <div className="flex items-center gap-2">{action}</div>}
    </header>
  );
}

export default FilesHome;
