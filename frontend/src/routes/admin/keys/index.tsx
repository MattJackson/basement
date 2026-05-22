import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from "react";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { useKeys, useListClusters } from "@/shared/api/queries";
import type { components } from "@/shared/api/types.gen";
import { adminPage } from "@/shared/layout/adminPage";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useCreateKey } from "@/shared/api/mutations";
import { DeleteKeyConfirm } from "@/shared/ui/DeleteKeyConfirm";
import { useDeleteKey } from "@/shared/api/mutations";
import { Checkbox } from "@/components/ui/checkbox";
import { ClusterBadge } from "@/components/ClusterBadge";
import { ClusterFilter } from "@/components/ClusterFilter";
import { useElevationGuard } from "@/shared/auth/elevation";

export const Route = createFileRoute("/admin/keys/")({
  component: adminPage(KeysScreen),
});

function KeysScreen() {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [clusterFilter, setClusterFilter] = useState<string | null>(null);
  const { data: keysData, isLoading, error } = useKeys();
  
  // Create key dialog state
  const [createOpen, setCreateOpen] = useState(false);
  const [newKeyName, setNewKeyName] = useState("");
  const createKey = useCreateKey();
  const [createdKey, setCreatedKey] = useState<components["schemas"]["Key"] | null>(null);
  const [savedConfirmed, setSavedConfirmed] = useState(false);
  
  // Delete key dialog state
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [keyToDelete, setKeyToDelete] = useState<{ cid: string; id: string; name?: string } | null>(null);
  const deleteKey = useDeleteKey();
  const { data: clusters } = useListClusters();
  const targetCid = clusterFilter ?? clusters?.[0]?.id ?? null;
  // v1.3.0a.3: key:delete + key:create are ADMIN-tier on the backend.
  // Wrap the click handlers so a 403 ELEVATION_REQUIRED pops the
  // modal and retries on success — one-click after elevate instead
  // of "elevate → modal closes → re-click to actually delete".
  const runWithElevation = useElevationGuard();

  const keys = keysData?.keys ?? [];
  const errors = keysData?.errors ?? [];

  const filteredKeys = keys.filter((key) => {
    if (clusterFilter && key.connectionId !== clusterFilter) return false;
    if (!search) return true;
    const needle = search.toLowerCase();
    const nameMatch = key.name?.toLowerCase().includes(needle) ?? false;
    const idMatch = key.id?.toLowerCase().includes(needle) ?? false;
    return nameMatch || idMatch;
  });

  const errorBanner = errors.length > 0 ? (
    <div className="rounded-lg border bg-destructive/10 p-4">
      <p className="text-sm text-destructive font-medium">
        Couldn&apos;t load keys from {errors.length} cluster{errors.length > 1 ? "s" : ""}:{" "}
        {errors.map((e) => e.connectionId.slice(0, 8)).join(", ")}
      </p>
      <details className="mt-2 text-xs">
        <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
          View details
        </summary>
        <ul className="mt-2 space-y-1">
          {errors.map((e, idx) => (
            <li key={idx} className="text-destructive">
              {e.connectionId.slice(0, 8)}: {e.message}
            </li>
          ))}
        </ul>
      </details>
    </div>
  ) : null;

  const header = (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Access keys</h1>
        <p className="text-sm text-muted-foreground mt-1">
          S3 credentials with per-bucket permissions.
        </p>
      </div>
      <div className="flex items-center gap-2 w-full sm:w-auto">
        <Input
          placeholder="Search by name..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 sm:w-64"
        />
        <ClusterFilter selectedClusterId={clusterFilter} onFilterChange={setClusterFilter} />
        <Button variant="outline" onClick={() => { setCreateOpen(true); setNewKeyName(""); }}>
          New
        </Button>
      </div>
    </header>
  );

  if (error) {
    return (
      <div className="space-y-6">
        {header}
        {errorBanner}
        <ErrorBanner message="Couldn&apos;t connect to cluster. Retrying automatically..." />
      </div>
    );
  }

  const handleCreateKey = async () => {
    if (!newKeyName.trim() || !targetCid) return;
    try {
      const key = await runWithElevation(() =>
        createKey.mutateAsync({ cid: targetCid, name: newKeyName.trim() }),
      );
      setCreateOpen(false);
      setNewKeyName("");
      setCreatedKey(key);
    } catch {
      // Errors (including ELEVATION_CANCELLED) surface via the
      // mutation's existing error banner above the input.
    }
  };

  const handleDeleteClick = (cid: string, id: string, name?: string) => {
    setKeyToDelete({ cid, id, name });
    setDeleteOpen(true);
  };

  const handleDeleteConfirm = async () => {
    if (!keyToDelete) return;
    const target = keyToDelete;
    setDeleteOpen(false);
    setKeyToDelete(null);
    try {
      await runWithElevation(() =>
        deleteKey.mutateAsync({ cid: target.cid, id: target.id }),
      );
    } catch {
      // ELEVATION_CANCELLED / network errors surface via the
      // mutation's existing error toast.
    }
  };

  return (
    <div className="space-y-6">
      {header}
      {errorBanner}

      {isLoading ? (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-48">Cluster</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Access Key ID</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(5)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-48" /></TableCell>
                  <TableCell><Skeleton className="h-8 w-8 rounded" /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : filteredKeys?.length === 0 ? (
        <EmptyState
          icon="key"
          title={search ? "No keys match your search" : "No access keys created yet"}
          description={
            search
              ? "Try a different search term or create a new key."
              : "Create your first API access key to get started."
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-48">Cluster</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Access Key ID</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredKeys?.map((key) => (
                <TableRow
                  key={key.id}
                  className="cursor-pointer hover:bg-muted/50"
                  onClick={() => {
                    navigate({ to: "/admin/clusters/$cid/keys/$id", params: { cid: key.connectionId, id: key.id } });
                  }}
                >
                  <TableCell>
                    <ClusterBadge connectionId={key.connectionId} />
                  </TableCell>
                  <TableCell className="font-medium">{key.name ?? "—"}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs">{key.id}</span>
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <Button
                              variant="ghost"
                              size="sm"
                              className="h-6 w-6 p-0"
                              onClick={(e) => {
                                e.stopPropagation();
                                navigator.clipboard.writeText(key.id);
                              }}
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
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>
                            <p>Copy key ID</p>
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </div>
                  </TableCell>
                  <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger onClick={(e) => e.stopPropagation()}>
                          <Button variant="ghost" className="h-8 w-8 p-0">
                            <span className="sr-only">Open menu</span>
                            <svg
                              xmlns="http://www.w3.org/2000/svg"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                              className="h-4 w-4"
                            >
                              <circle cx="12" cy="12" r="1" />
                              <circle cx="19" cy="12" r="1" />
                              <circle cx="5" cy="12" r="1" />
                            </svg>
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            onClick={(e) => {
                              e.stopPropagation();
                              navigate({ to: "/admin/clusters/$cid/keys/$id", params: { cid: key.connectionId, id: key.id } });
                            }}
                          >
                            View
                          </DropdownMenuItem>
                          <DropdownMenuItem
                            variant="destructive"
                            onClick={(e) => {
                              e.stopPropagation();
                              handleDeleteClick(key.connectionId, key.id, key.name);
                            }}
                          >
                            Delete
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Create Key Dialog */}
      <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); if (!open) { setNewKeyName(""); createKey.reset(); } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create access key</DialogTitle>
            <DialogDescription>
              Create a new API access key with per-bucket permissions. You&apos;ll see the secret key only once — save it securely.
            </DialogDescription>
          </DialogHeader>
          {createKey.error && (
            <div
              role="alert"
              className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
            >
              {createKey.error.message.replace(/^[A-Z_]+:\s*/, "")}
            </div>
          )}
          <Input
            placeholder="Key name (required)"
            value={newKeyName}
            onChange={(e) => { setNewKeyName(e.target.value); if (createKey.error) createKey.reset(); }}
            disabled={createKey.isPending}
            autoFocus
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)} disabled={createKey.isPending}>Cancel</Button>
            <Button onClick={handleCreateKey} disabled={!newKeyName.trim() || createKey.isPending}>
              {createKey.isPending ? "Creating…" : "Create key"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Created Key Secret Display Dialog — Garage returns the
          secret access key exactly once. dismissible=false so a
          stray click outside or Escape doesn't lose the only copy. */}
      {createdKey && (
        <Dialog
          open={true}
          dismissible={false}
          onOpenChange={() => { /* explicit confirm only */ }}
        >
          <DialogContent className="max-w-xl">
            <DialogHeader>
              <DialogTitle>Save this — it won&apos;t be shown again</DialogTitle>
              <DialogDescription>
                The backend returns the secret access key for{" "}
                <code className="rounded bg-muted px-1 py-0.5 font-mono text-foreground">
                  {createdKey.name ?? createdKey.id}
                </code>{" "}
                exactly once. Copy it now, then store it in your password
                manager / S3 client. We can&apos;t show it again later.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-3">
              <div>
                <div className="text-xs font-medium text-muted-foreground mb-1">
                  Access Key ID
                </div>
                <div className="flex gap-2">
                  <code className="flex-1 rounded bg-muted px-3 py-2 font-mono text-sm break-all">
                    {createdKey.accessKeyId ?? createdKey.id}
                  </code>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => navigator.clipboard.writeText(createdKey.accessKeyId ?? createdKey.id)}
                  >
                    Copy
                  </Button>
                </div>
              </div>

              <div>
                <div className="text-xs font-medium text-amber-700 dark:text-amber-400 mb-1">
                  Secret Access Key — shown once
                </div>
                <div className="flex gap-2">
                  <code className="flex-1 rounded border-2 border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950/30 px-3 py-2 font-mono text-sm break-all">
                    {createdKey.secretAccessKey ?? "(no secret returned by backend)"}
                  </code>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => navigator.clipboard.writeText(createdKey.secretAccessKey ?? "")}
                    disabled={!createdKey.secretAccessKey}
                  >
                    Copy
                  </Button>
                </div>
              </div>

              {/* Pre-filled ~/.aws/credentials snippet. Not a live aws-cli
                  invocation — just a paste-ready profile block so the
                  operator can drop it into the SDK config most S3
                  clients pick up by default. The profile name uses the
                  key's display name (or "basement" if anonymous) so
                  multiple keys don't collide. */}
              {createdKey.secretAccessKey && (
                <div>
                  <div className="text-xs font-medium text-muted-foreground mb-1">
                    ~/.aws/credentials snippet
                  </div>
                  <div className="flex gap-2">
                    <pre
                      className="flex-1 rounded bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-all"
                      data-testid="aws-cli-snippet"
                    >
                      {(() => {
                        const profile = (createdKey.name ?? "basement")
                          .toLowerCase()
                          .replace(/[^a-z0-9_-]+/g, "-")
                          .replace(/^-+|-+$/g, "") || "basement";
                        return `[${profile}]
aws_access_key_id = ${createdKey.accessKeyId ?? createdKey.id}
aws_secret_access_key = ${createdKey.secretAccessKey}`;
                      })()}
                    </pre>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => {
                        const profile = (createdKey.name ?? "basement")
                          .toLowerCase()
                          .replace(/[^a-z0-9_-]+/g, "-")
                          .replace(/^-+|-+$/g, "") || "basement";
                        navigator.clipboard.writeText(
                          `[${profile}]\naws_access_key_id = ${createdKey.accessKeyId ?? createdKey.id}\naws_secret_access_key = ${createdKey.secretAccessKey}`,
                        );
                      }}
                    >
                      Copy
                    </Button>
                  </div>
                </div>
              )}

              <label className="flex items-start gap-2 pt-2 cursor-pointer select-none">
                <Checkbox
                  checked={savedConfirmed}
                  onCheckedChange={(c) => setSavedConfirmed(c === true)}
                  data-testid="saved-confirmed-checkbox"
                />
                <span className="text-sm">
                  I&apos;ve copied the secret access key into a password manager or
                  S3 client. I understand it will not be shown again.
                </span>
              </label>
            </div>

            <DialogFooter>
              <Button
                onClick={() => { setSavedConfirmed(false); setCreatedKey(null); }}
                disabled={!savedConfirmed}
                data-testid="saved-confirmed-button"
                // Stop Enter-key auto-submit
                type="button"
              >
                Done
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}

      <DeleteKeyConfirm
        open={deleteOpen}
        keyName={keyToDelete?.name}
        isDeleting={deleteKey.isPending}
        onConfirm={handleDeleteConfirm}
        onCancel={() => { setDeleteOpen(false); setKeyToDelete(null); }}
      />
    </div>
  );
}
