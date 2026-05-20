import { createFileRoute } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";
import { useUserClusters } from "@/shared/api/queries";
import { UserClusterCard } from "@/components/clusters/UserClusterCard";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";

export const Route = createFileRoute("/files/")({
  component: userPage(FilesHome),
});

function FilesHome() {
  const { data: clustersData, isLoading, error } = useUserClusters();
  const clusters = clustersData ?? [];

  if (error) {
    return (
      <div className="space-y-6">
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Clusters</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Storage you have access to
          </p>
        </header>
        <ErrorBanner message="Couldn&apos;t connect to backend. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Clusters</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Storage you have access to
        </p>
      </header>

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="rounded-xl border bg-card p-4 space-y-3">
              <div className="flex items-center gap-3">
                <Skeleton className="h-3 w-3 rounded-full" />
                <Skeleton className="h-4 w-32 flex-1" />
              </div>
              <Skeleton className="h-6 w-20" />
              <Skeleton className="h-4 w-16" />
            </div>
          ))}
        </div>
      ) : clusters.length === 0 ? (
        <EmptyState
          icon="server"
          title="No clusters yet"
          description="Contact your administrator to get access."
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {clusters.map((cluster) => (
            <UserClusterCard
              key={cluster.id}
              id={cluster.id}
              label={cluster.label}
              driver={cluster.driver}
              color={cluster.color}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export default FilesHome;

