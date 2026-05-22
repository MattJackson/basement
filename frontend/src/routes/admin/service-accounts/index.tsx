import { useState } from "react";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

import {
  useServiceAccounts,
  useDeleteServiceAccount,
  useRotateServiceAccount,
  type ServiceAccount,
  type ServiceAccountWithSecret,
} from "@/shared/api/queries";
import { useElevationGuard } from "@/shared/auth/elevation";
import { humanizeTime } from "@/shared/lib/format";
import { SecretShownOnceDialog } from "@/shared/ui/SecretShownOnceDialog";

// NOTE: do NOT wrap in adminPage() — parent layout (service-accounts.tsx)
// owns the chrome.
export const Route = createFileRoute("/admin/service-accounts/")({
  component: ServiceAccountsPage,
});

function ServiceAccountsPage() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [search, setSearch] = useState("");
  const { data: rows, isLoading, error } = useServiceAccounts();

  const deleteSA = useDeleteServiceAccount();
  const rotateSA = useRotateServiceAccount();
  // Rotate + revoke are both gated by host:manage_users on the
  // backend — ADMIN-tier. Wrap so the elevation modal fires on 403
  // and the action retries automatically on success.
  const runWithElevation = useElevationGuard();

  // Confirm-revoke dialog state.
  const [revokeTarget, setRevokeTarget] = useState<ServiceAccount | null>(null);

  // The shown-once dialog reuses the v1.7.0c <SecretShownOnceDialog>
  // for both create and rotate. The rotate flow keys off this state
  // — the create flow lives on /admin/service-accounts/new and
  // navigates back here with the dialog open via location state.
  const [rotated, setRotated] = useState<ServiceAccountWithSecret | null>(null);

  const filtered = (rows ?? []).filter((sa) => {
    if (!search) return true;
    const needle = search.toLowerCase();
    return (
      sa.name.toLowerCase().includes(needle) ||
      sa.accessKeyId.toLowerCase().includes(needle)
    );
  });

  const handleRotate = async (sa: ServiceAccount) => {
    try {
      const result = await runWithElevation(() => rotateSA.mutateAsync(sa.id));
      setRotated(result);
      queryClient.invalidateQueries({ queryKey: ["admin", "service-accounts"] });
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  const handleConfirmRevoke = async () => {
    if (!revokeTarget) return;
    const target = revokeTarget;
    setRevokeTarget(null);
    try {
      await runWithElevation(() => deleteSA.mutateAsync(target.id));
      queryClient.invalidateQueries({ queryKey: ["admin", "service-accounts"] });
      toast.success(`Revoked ${target.name}`);
    } catch (e) {
      if ((e as Error)?.message === "ELEVATION_CANCELLED") return;
      toast.error((e as Error).message);
    }
  };

  const header = (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          Service accounts
        </h1>
        <p className="text-sm text-muted-foreground mt-1">
          Long-lived bearer credentials for scripts, CI, and CLI tools.
          Each carries the capabilities you grant when minting it.
        </p>
      </div>
      <div className="flex items-center gap-2 w-full sm:w-auto">
        <Input
          placeholder="Search by name or access key..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 sm:w-72"
        />
        <Button
          variant="outline"
          onClick={() => navigate({ to: "/admin/service-accounts/new" })}
          data-testid="new-sa-button"
        >
          + New service account
        </Button>
      </div>
    </header>
  );

  if (error) {
    return (
      <div className="space-y-6">
        {header}
        <ErrorBanner message={error.message} />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {header}

      {isLoading ? (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Access key</TableHead>
                <TableHead>Capabilities</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[...Array(3)].map((_, i) => (
                <TableRow key={i}>
                  <TableCell><Skeleton className="h-4 w-32" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-40" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-12" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-20" /></TableCell>
                  <TableCell><Skeleton className="h-4 w-16" /></TableCell>
                  <TableCell><Skeleton className="h-8 w-8 rounded" /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon="key"
          title={search ? "No service accounts match your search" : "No service accounts yet"}
          description={
            search
              ? "Try a different search term or mint a new one."
              : "Mint a service account to issue a long-lived bearer credential for scripts or CI."
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Access key</TableHead>
                <TableHead>Capabilities</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="w-16">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filtered.map((sa) => (
                <ServiceAccountRow
                  key={sa.id}
                  sa={sa}
                  onRotate={() => handleRotate(sa)}
                  onRevoke={() => setRevokeTarget(sa)}
                  rotateBusy={rotateSA.isPending}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {/* Revoke confirmation — soft-delete via backend. Two-field
          dialog (just the warning + confirm button) fits the
          popups-max-2-fields rule. */}
      <Dialog
        open={revokeTarget !== null}
        onOpenChange={(open) => { if (!open) setRevokeTarget(null); }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Revoke service account?</DialogTitle>
            <DialogDescription>
              Revoke service account{" "}
              <code className="rounded bg-muted px-1 py-0.5 font-mono text-foreground">
                {revokeTarget?.name}
              </code>
              ? Any client using this key will lose access. This cannot
              be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setRevokeTarget(null)}
              disabled={deleteSA.isPending}
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={handleConfirmRevoke}
              disabled={deleteSA.isPending}
              data-testid="confirm-revoke-button"
            >
              {deleteSA.isPending ? "Revoking…" : "Revoke"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rotate result: same shown-once dialog as the create flow.
          Reuses the shared component so the copy + a11y semantics
          stay consistent between the two reveal paths. */}
      {rotated && (
        <SecretShownOnceDialog
          result={rotated}
          mode="rotate"
          onDone={() => setRotated(null)}
        />
      )}
    </div>
  );
}

function ServiceAccountRow({
  sa,
  onRotate,
  onRevoke,
  rotateBusy,
}: {
  sa: ServiceAccount;
  onRotate: () => void;
  onRevoke: () => void;
  rotateBusy: boolean;
}) {
  const status = saStatus(sa);
  const isRevoked = !!sa.revokedAt;
  const isExpired = status === "expired";

  return (
    <TableRow data-testid={`sa-row-${sa.id}`}>
      <TableCell className="font-medium">
        {/* v1.8.0d — Name links to the detail page so operators can
            grab the MCP config snippet (Use with MCP card) after the
            shown-once dialog has closed. */}
        <Link
          to="/admin/service-accounts/$id"
          params={{ id: sa.id }}
          className="hover:underline"
          data-testid={`sa-name-link-${sa.id}`}
        >
          {sa.name}
        </Link>
      </TableCell>
      <TableCell>
        <span className="font-mono text-xs break-all">{sa.accessKeyId}</span>
      </TableCell>
      <TableCell className="text-sm">
        {sa.capabilities.length === 0 ? (
          <span className="text-muted-foreground italic">none</span>
        ) : (
          <span title={sa.capabilities.map((c) => `${c.id} @ ${c.scope}`).join("\n")}>
            {sa.capabilities.length}
          </span>
        )}
      </TableCell>
      <TableCell className="text-sm text-muted-foreground">
        {sa.lastUsedAt ? humanizeTime(sa.lastUsedAt) : "never"}
      </TableCell>
      <TableCell className="text-sm text-muted-foreground">
        {sa.expiresAt ? humanizeTime(sa.expiresAt) : "never"}
      </TableCell>
      <TableCell>
        <StatusBadge status={status} />
      </TableCell>
      <TableCell>
        <DropdownMenu>
          <DropdownMenuTrigger>
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
              disabled={rotateBusy || isRevoked || isExpired}
              onClick={onRotate}
              data-testid={`rotate-${sa.id}`}
            >
              Rotate secret
            </DropdownMenuItem>
            <DropdownMenuItem
              variant="destructive"
              disabled={isRevoked}
              onClick={onRevoke}
              data-testid={`revoke-${sa.id}`}
            >
              Revoke
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </TableCell>
    </TableRow>
  );
}

// saStatus collapses the (RevokedAt, ExpiresAt) pair into one of
// three labels for the status pill. Order matters — revoked beats
// expired beats active, mirroring the backend's bearer-middleware
// check sequence in v1.7.0b.
export function saStatus(sa: ServiceAccount): "revoked" | "expired" | "active" {
  if (sa.revokedAt) return "revoked";
  if (sa.expiresAt && new Date(sa.expiresAt).getTime() < Date.now()) {
    return "expired";
  }
  return "active";
}

function StatusBadge({ status }: { status: "revoked" | "expired" | "active" }) {
  if (status === "revoked") {
    return (
      <Badge
        variant="outline"
        className="border-destructive/40 bg-destructive/10 text-destructive"
      >
        Revoked
      </Badge>
    );
  }
  if (status === "expired") {
    return (
      <Badge
        variant="outline"
        className="border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400"
      >
        Expired
      </Badge>
    );
  }
  return (
    <Badge
      variant="outline"
      className="border-green-500/40 bg-green-500/10 text-green-700 dark:text-green-400"
    >
      Active
    </Badge>
  );
}
