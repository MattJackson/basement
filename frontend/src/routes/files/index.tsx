import { createFileRoute } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";
import { useUserRegions } from "@/shared/api/queries";
import { UserKeyCard } from "@/components/keys/UserKeyCard";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";

// /files = "My Regions" — heading kept per ADR-0002, but v1.2.0d
// refines the model: each card is one of the user's ACCESS KEYS. A
// user may add multiple keys against the same endpoint with different
// aliases ("Work S3", "Personal S3"), so the subtitle clarifies the
// region-as-keychain-entry framing. The canonical CTA is now "Add a
// key" pointing at /files/keys/new; /files/regions/new redirects there
// for back-compat.
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
              href="/files/keys/new"
              className="inline-flex items-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            >
              + Add a key
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
          title="No keys yet"
          description="Add a key from your cluster admin to see the buckets it can reach."
          action={
            <a
              href="/files/keys/new"
              className="inline-flex items-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            >
              + Add a key
            </a>
          }
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {regions.map((region) => (
            <UserKeyCard key={region.id} region={region} />
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
          Each card is one of your access keys — click to browse the buckets it
          can see
        </p>
      </div>
      {action && <div className="flex items-center gap-2">{action}</div>}
    </header>
  );
}

export default FilesHome;
