import { createFileRoute } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";
import { useUserKeys } from "@/shared/api/queries";
import { UserKeyPairCard, UserKeyPairCardSkeleton } from "@/components/UserKeyPairCard";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";

export const Route = createFileRoute("/files/keys")({
  component: userPage(KeysHome),
});

function KeysHome() {
  const { data: keysData, isLoading, error } = useUserKeys();
  const errors = keysData?.errors ?? [];

  if (error) {
    return (
      <div className="space-y-6">
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Keys</h1>
          <p className="text-sm text-muted-foreground mt-1">
            S3 credentials for the buckets you can access
          </p>
        </header>
        <ErrorBanner message="Couldn&apos;t connect to backend. Retrying automatically..." />
      </div>
    );
  }

  const keys = keysData?.keys ?? [];

  if (isLoading) {
    return (
      <div className="space-y-6">
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Keys</h1>
          <p className="text-sm text-muted-foreground mt-1">
            S3 credentials for the buckets you can access
          </p>
        </header>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(6)].map((_, i) => (
            <UserKeyPairCardSkeleton key={i} />
          ))}
        </div>
      </div>
    );
  }

  if (keys.length === 0) {
    return (
      <div className="space-y-6">
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Keys</h1>
          <p className="text-sm text-muted-foreground mt-1">
            S3 credentials for the buckets you can access
          </p>
        </header>
        
        {errors.length > 0 ? (
          <div className="rounded-lg border bg-destructive/10 p-4">
            <p className="text-sm text-destructive font-medium">
              Couldn&apos;t load keys from {errors.length} cluster{errors.length > 1 ? "s" : ""}:{" "}
              {errors.map((e) => e.connectionId.slice(0, 8)).join(", ")}
            </p>
          </div>
        ) : (
          <EmptyState
            icon="key"
            title="No keys yet"
            description="Contact your administrator to get one."
          />
        )}
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Keys</h1>
        <p className="text-sm text-muted-foreground mt-1">
          S3 credentials for the buckets you can access
        </p>
      </header>

      {errors.length > 0 ? (
        <div className="rounded-lg border bg-destructive/10 p-4">
          <p className="text-sm text-destructive font-medium">
            Couldn&apos;t load keys from {errors.length} cluster{errors.length > 1 ? "s" : ""}:{" "}
            {errors.map((e) => e.connectionId.slice(0, 8)).join(", ")}
          </p>
        </div>
      ) : null}

<div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {keys.map((key) => (
          <UserKeyPairCard
            key={`${key.id}-${key.connectionId}`}
            keyId={key.id}
            connectionId={key.connectionId}
            bucketAliases={[key.name || "Unknown"]}
            keyName={key.name}
            accessKeyId={key.accessKeyId ?? (key.id as string)}
            created={key.created}
          />
        ))}
      </div>
    </div>
  );
}

export default KeysHome;

