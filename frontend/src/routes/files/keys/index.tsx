import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useState } from "react";

import {
  useUserRegions,
  useDeleteUserRegion,
  type UserRegion,
} from "@/shared/api/queries";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CardDescription,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeTime } from "@/shared/lib/format";

// ADR-0002 + v1.2.0d: the canonical "manage my keys" view. Each card
// is one access key in the user's keychain. v1.2.0d allows multiple
// keys against the same endpoint (different aliases), so this page is
// the per-key admin surface — copy access key ID, see last-used, or
// delete an individual key. Adding a new key lives at /files/keys/new
// (the legacy /files/regions/new URL redirects there).
export const Route = createFileRoute("/files/keys/")({
  component: KeysIndex,
});

function KeysIndex() {
  const { data: regions, isLoading, error } = useUserRegions();

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader />
        <ErrorBanner message="Couldn&apos;t load your keys. Retrying automatically..." />
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <PageHeader />
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {[...Array(3)].map((_, i) => (
            <RegionKeyCardSkeleton key={i} />
          ))}
        </div>
      </div>
    );
  }

  const list = regions ?? [];

  if (list.length === 0) {
    return (
      <div className="space-y-6">
        <PageHeader />
        <EmptyState
          icon="key"
          title="No keys yet"
          description="Add a key from your cluster admin to start browsing buckets."
          action={
            <Link to="/files/keys/new">
              <Button>Add a key</Button>
            </Link>
          }
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <PageHeader />
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {list.map((region) => (
          <RegionKeyCard key={region.id} region={region} />
        ))}
      </div>
    </div>
  );
}

function PageHeader() {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">My Keys</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Access keys you&apos;ve added — one row per key, multiple keys per
          endpoint allowed
        </p>
      </div>
      <Link to="/files/keys/new">
        <Button>Add a key</Button>
      </Link>
    </header>
  );
}

function RegionKeyCard({ region }: { region: UserRegion }) {
  const router = useRouter();
  const deleteRegion = useDeleteUserRegion();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(region.accessKeyId);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard failures aren't critical — the user can still
      // select the text manually.
    }
  };

  const onDelete = async () => {
    await deleteRegion.mutateAsync(region.id);
    setConfirmOpen(false);
    router.invalidate();
  };

  return (
    <Card data-testid="region-key-card">
      <CardHeader>
        <CardTitle className="truncate">{region.alias || "(unnamed key)"}</CardTitle>
        <CardDescription className="font-mono text-xs truncate">
          {region.endpoint}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div>
          <p className="text-xs text-muted-foreground mb-1">Access Key ID</p>
          <div className="flex items-center gap-2">
            <code
              className="flex-1 truncate rounded border bg-muted px-2 py-1 text-xs font-mono"
              data-testid="region-access-key"
            >
              {region.accessKeyId}
            </code>
            <Button
              size="sm"
              variant="secondary"
              onClick={onCopy}
              data-testid="copy-region-key-button"
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          {region.lastUsedAt
            ? `Last used ${humanizeTime(region.lastUsedAt)}`
            : "Never used"}
        </p>

        <div className="pt-2 border-t flex justify-end">
          <Button
            size="sm"
            variant="ghost"
            className="text-destructive hover:text-destructive"
            onClick={() => setConfirmOpen(true)}
            data-testid="delete-region-key-button"
          >
            Delete
          </Button>
        </div>
      </CardContent>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete key?</DialogTitle>
            <DialogDescription>
              This revokes basement&apos;s stored key for{" "}
              <span className="font-mono">{region.endpoint}</span>. The key still
              exists on the backend until your cluster admin deletes it there.
            </DialogDescription>
          </DialogHeader>
          {deleteRegion.error ? (
            <p className="text-sm text-destructive">
              {deleteRegion.error.message}
            </p>
          ) : null}
          <DialogFooter>
            <Button variant="secondary" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={onDelete}
              disabled={deleteRegion.isPending}
            >
              {deleteRegion.isPending ? "Deleting..." : "Delete key"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Card>
  );
}

function RegionKeyCardSkeleton() {
  return (
    <Card data-testid="region-key-card-skeleton">
      <CardHeader>
        <div className="h-5 w-32 rounded bg-muted animate-pulse" />
        <div className="mt-2 h-3 w-48 rounded bg-muted animate-pulse" />
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="h-3 w-20 rounded bg-muted animate-pulse" />
        <div className="h-7 w-full rounded bg-muted animate-pulse" />
        <div className="h-3 w-24 rounded bg-muted animate-pulse" />
      </CardContent>
    </Card>
  );
}

export default KeysIndex;
