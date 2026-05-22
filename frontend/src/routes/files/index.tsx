import { createFileRoute, Link } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";
import { useUserRegions } from "@/shared/api/queries";
import { UserRegionCard } from "@/components/clusters/UserRegionCard";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";

export const Route = createFileRoute("/files/")({
  component: userPage(FilesHome),
});

function FilesHome() {
  const { data: regions, isLoading, error } = useUserRegions();
  const list = regions ?? [];

  if (error) {
    return (
      <div className="space-y-6">
        <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
          <div>
            <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Regions</h1>
            <p className="text-sm text-muted-foreground mt-1">
              Storage endpoints you have S3 credentials for
            </p>
          </div>
        </header>
        <ErrorBanner message="Couldn&apos;t connect to backend. Retrying automatically..." />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Regions</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Storage endpoints you have S3 credentials for
          </p>
        </div>
        {list.length > 0 && (
          <Link
            to="/files/regions/new"
            className="inline-flex items-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
          >
            + Add region
          </Link>
        )}
      </header>

      {isLoading ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(3)].map((_, i) => (
            <div key={i} className="rounded-xl border bg-card p-4 space-y-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-6 w-20" />
              <Skeleton className="h-4 w-16" />
            </div>
          ))}
        </div>
      ) : list.length === 0 ? (
        <EmptyState
          icon="server"
          title="No regions yet"
          description="Add an S3 endpoint and credentials to start browsing buckets."
          action={
            <Link
              to="/files/regions/new"
              className="inline-flex items-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            >
              + Add region
            </Link>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {list.map((r) => (
            <UserRegionCard
              key={r.id}
              id={r.id}
              alias={r.alias}
              endpoint={r.endpoint}
              region={r.region}
              accessKeyId={r.accessKeyId}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export default FilesHome;
