import { createFileRoute, Link, useRouter } from "@tanstack/react-router";
import { useState } from "react";

import {
  useUserRegions,
  useDeleteUserRegion,
  useRotateUserRegion,
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

// addressingStyleLabel renders the per-card subtitle phrase. Defaults
// to "path-style" when the field is missing — defensive against an
// in-flight server upgrade where the FE may briefly see a stale wire
// shape with no addressingStyle.
function addressingStyleLabel(style: UserRegion["addressingStyle"]): string {
  if (style === "virtual_host") return "via virtual-host";
  return "via path-style";
}

// rotationRecentlyHappened returns true when updatedAt is within the
// last 24h AND meaningfully after createdAt — the v1.3.0c "Last
// rotated 2h ago" hint is only useful for the immediate post-rotate
// window. A 60s slop on the createdAt comparison absorbs millisecond
// drift between Create's timestamp and its first persisted row.
function rotationRecentlyHappened(region: UserRegion): boolean {
  if (!region.updatedAt) return false;
  const updated = new Date(region.updatedAt).getTime();
  const created = new Date(region.createdAt).getTime();
  if (!Number.isFinite(updated) || !Number.isFinite(created)) return false;
  if (updated - created < 60_000) return false;
  return Date.now() - updated < 24 * 60 * 60 * 1000;
}

function RegionKeyCard({ region }: { region: UserRegion }) {
  const router = useRouter();
  const deleteRegion = useDeleteUserRegion();
  const rotateRegion = useRotateUserRegion();
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
  const [rotateAccessKey, setRotateAccessKey] = useState("");
  const [rotateSecret, setRotateSecret] = useState("");
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

  const onRotate = async () => {
    if (!rotateAccessKey.trim() || !rotateSecret) return;
    await rotateRegion.mutateAsync({
      regionId: region.id,
      accessKeyId: rotateAccessKey.trim(),
      secretKey: rotateSecret,
    });
    setRotateOpen(false);
    setRotateAccessKey("");
    setRotateSecret("");
    router.invalidate();
  };

  const recentlyRotated = rotationRecentlyHappened(region);

  return (
    <Card data-testid="region-key-card">
      <CardHeader>
        <CardTitle className="truncate">{region.alias || "(unnamed key)"}</CardTitle>
        <CardDescription className="font-mono text-xs truncate">
          {region.endpoint}
        </CardDescription>
        <p
          className="text-xs text-muted-foreground"
          data-testid="region-addressing-style"
        >
          {addressingStyleLabel(region.addressingStyle)}
        </p>
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
          <div className="min-h-[44px] flex items-center">
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
        </div>

        <p className="text-xs text-muted-foreground">
          {region.lastUsedAt
            ? `Last used ${humanizeTime(region.lastUsedAt)}`
            : "Never used"}
        </p>

        {recentlyRotated && (
          <p
            className="text-xs text-muted-foreground"
            data-testid="region-last-rotated"
          >
            {`Last rotated ${humanizeTime(region.updatedAt)}`}
          </p>
        )}

        <div className="pt-2 border-t flex justify-end gap-2">
          <div className="min-h-[44px] flex items-center">
            <Button
              size="sm"
              variant="secondary"
              onClick={() => setRotateOpen(true)}
              data-testid="rotate-region-key-button"
            >
              Rotate key
            </Button>
          </div>
          <div className="min-h-[44px] flex items-center">
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

      <Dialog open={rotateOpen} onOpenChange={setRotateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rotate key?</DialogTitle>
            <DialogDescription>
              Paste a freshly-minted access key for{" "}
              <span className="font-mono">{region.endpoint}</span>. Your alias,
              region, and history all stay; only the credentials flip.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <label
                htmlFor={`rotate-ak-${region.id}`}
                className="text-sm font-medium"
              >
                New access key ID
              </label>
              <input
                id={`rotate-ak-${region.id}`}
                type="text"
                value={rotateAccessKey}
                onChange={(e) => setRotateAccessKey(e.target.value)}
                autoComplete="off"
                spellCheck={false}
                placeholder="GK..."
                className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                data-testid="rotate-region-access-key"
              />
            </div>
            <div className="space-y-1">
              <label
                htmlFor={`rotate-sk-${region.id}`}
                className="text-sm font-medium"
              >
                New secret access key
              </label>
              <input
                id={`rotate-sk-${region.id}`}
                type="password"
                value={rotateSecret}
                onChange={(e) => setRotateSecret(e.target.value)}
                autoComplete="new-password"
                spellCheck={false}
                placeholder={"••••••••"}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
                data-testid="rotate-region-secret"
              />
            </div>
          </div>
          {rotateRegion.error ? (
            <p className="text-sm text-destructive" data-testid="rotate-error">
              {rotateRegion.error.message}
            </p>
          ) : null}
          <DialogFooter>
            <Button variant="secondary" onClick={() => setRotateOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={onRotate}
              disabled={
                rotateRegion.isPending ||
                !rotateAccessKey.trim() ||
                !rotateSecret
              }
              data-testid="rotate-region-submit"
            >
              {rotateRegion.isPending ? "Rotating..." : "Rotate"}
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
