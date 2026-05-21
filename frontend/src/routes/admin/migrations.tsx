// ADR-0001 cycle v0.9.0h — /admin/migrations: one-click migration of
// orphaned legacy S3 credentials.
//
// Lists every Connection whose config still carries the pre-v0.9.0d
// access_key_id + secret_key fields. For each, shows discovered bucket
// aliases as checkboxes (pre-checked), a user dropdown (defaults to
// the currently-logged-in user — typically matthew on basement.pq.io),
// and a "Migrate this cluster" button. On success the row disappears
// from the list (React Query refetch). When the list goes empty, the
// page surfaces a "All clusters migrated" panel + a link back to
// /admin/clusters.

import { useMemo, useState } from "react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { adminPage } from "@/shared/layout/adminPage";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  useOrphanCreds,
  useMigrateOrphanCreds,
  type OrphanCred,
} from "@/shared/api/queries";
import { useUser } from "@/shared/auth/useUser";

export const Route = createFileRoute("/admin/migrations")({
  component: adminPage(MigrationsPage),
});

function MigrationsPage() {
  const { data: user } = useUser();
  const isHostAdmin = user?.uiAdmin === true;
  const { data, isLoading, error } = useOrphanCreds(isHostAdmin);

  if (!isHostAdmin) {
    return (
      <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
        Host Admin access required to view migrations.
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[300px] text-muted-foreground">
        Loading orphan credentials...
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
        Failed to load orphan credentials: {(error as Error).message}
      </div>
    );
  }

  const orphans = data?.orphans ?? [];

  return (
    <div className="space-y-8">
      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          Credential migrations
        </h1>
        <p className="text-sm text-muted-foreground mt-1 max-w-3xl">
          Connections imported before the v0.9 RBAC model carried per-cluster
          S3 credentials in config. The runtime no longer reads those &mdash;
          object browsing requires a per-user{" "}
          <span className="font-medium">BucketGrant</span>. Use this page to
          convert each orphan into a grant for a specific user and strip the
          legacy fields from the Connection.
        </p>
      </header>

      {orphans.length === 0 ? (
        <AllMigratedPanel />
      ) : (
        <div className="space-y-4">
          {orphans.map((o) => (
            <OrphanCard
              key={o.connectionId}
              orphan={o}
              defaultUserId={user?.username ?? ""}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function OrphanCard({
  orphan,
  defaultUserId,
}: {
  orphan: OrphanCred;
  defaultUserId: string;
}) {
  const qc = useQueryClient();
  const migrate = useMigrateOrphanCreds();
  const [userId, setUserId] = useState(defaultUserId);

  // Pre-check every discovered bucket; an empty discovery list means the
  // operator must type an alias manually below. The parent passes a
  // stable key={connectionId} on this card so a remount drops/reseeds
  // state when the orphan list refetches — no useEffect-resync needed.
  const initialSelection = useMemo(
    () => new Set(orphan.discoveredBuckets),
    [orphan.discoveredBuckets],
  );
  const [selected, setSelected] = useState<Set<string>>(initialSelection);
  const [manualAlias, setManualAlias] = useState("");

  const toggle = (alias: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(alias)) next.delete(alias);
      else next.add(alias);
      return next;
    });
  };

  const handleMigrate = async () => {
    const aliases = Array.from(selected);
    if (manualAlias.trim()) aliases.push(manualAlias.trim());
    if (aliases.length === 0) {
      toast.error("Pick at least one bucket alias to migrate.");
      return;
    }
    if (!userId.trim()) {
      toast.error("User is required.");
      return;
    }
    try {
      const res = await migrate.mutateAsync({
        connectionId: orphan.connectionId,
        userId: userId.trim(),
        bucketAliases: aliases,
      });
      toast.success(
        `Migrated ${res.grantsCreated} bucket${
          res.grantsCreated === 1 ? "" : "s"
        } on ${orphan.label}`,
      );
      // Refetch the orphans list so this row disappears.
      void qc.invalidateQueries({
        queryKey: ["admin", "migrations", "orphan_creds"],
      });
    } catch (e) {
      toast.error(
        `Migration failed: ${(e as Error).message ?? "unknown error"}`,
      );
    }
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
          <CardTitle className="text-lg flex items-center gap-2">
            {orphan.label}
            <span className="text-xs font-normal rounded bg-muted px-1.5 py-0.5 text-muted-foreground">
              {orphan.driver}
            </span>
          </CardTitle>
          <div className="text-xs text-muted-foreground font-mono">
            access key: {truncateMiddle(orphan.accessKeyId, 24)}
            {orphan.hasSecretKey ? " (secret present)" : " (secret missing)"}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <div className="grid gap-2">
          <Label className="text-xs uppercase tracking-wider text-muted-foreground">
            Discovered buckets
          </Label>
          {orphan.discoveredBuckets.length === 0 ? (
            <p className="text-sm text-muted-foreground italic">
              Could not list buckets from this cluster (admin token may be
              missing or cluster offline). Enter an alias manually below.
            </p>
          ) : (
            <div className="flex flex-wrap gap-3">
              {orphan.discoveredBuckets.map((alias) => (
                <label
                  key={alias}
                  className="flex items-center gap-2 rounded-md border bg-card px-3 py-1.5 cursor-pointer hover:bg-muted/40"
                >
                  <Checkbox
                    checked={selected.has(alias)}
                    onCheckedChange={() => toggle(alias)}
                  />
                  <span className="text-sm font-medium">{alias}</span>
                </label>
              ))}
            </div>
          )}
        </div>

        <div className="grid sm:grid-cols-2 gap-3">
          <div className="grid gap-2">
            <Label
              htmlFor={`manual-${orphan.connectionId}`}
              className="text-xs uppercase tracking-wider text-muted-foreground"
            >
              Add bucket alias (optional)
            </Label>
            <Input
              id={`manual-${orphan.connectionId}`}
              value={manualAlias}
              onChange={(e) => setManualAlias(e.target.value)}
              placeholder="e.g. lsi"
            />
          </div>
          <div className="grid gap-2">
            <Label
              htmlFor={`user-${orphan.connectionId}`}
              className="text-xs uppercase tracking-wider text-muted-foreground"
            >
              Grant to user
            </Label>
            <Input
              id={`user-${orphan.connectionId}`}
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
              placeholder="matthew"
            />
          </div>
        </div>

        <div className="flex items-center justify-between pt-2">
          <p className="text-xs text-muted-foreground">
            After migration: creds are stripped from this Connection&apos;s
            config and re-stored as a per-user BucketGrant.
          </p>
          <Button
            onClick={handleMigrate}
            disabled={migrate.isPending}
          >
            {migrate.isPending ? "Migrating..." : "Migrate this cluster"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

function AllMigratedPanel() {
  return (
    <Card>
      <CardContent className="py-10 text-center space-y-3">
        <h2 className="text-lg font-semibold">All clusters migrated</h2>
        <p className="text-sm text-muted-foreground">
          No Connections carry orphaned credentials. The runtime now sources
          every user-tier S3 op from per-user grants.
        </p>
        <Link
          to="/admin/clusters"
          className="inline-block text-sm font-medium text-primary hover:underline"
        >
          Back to clusters &rarr;
        </Link>
      </CardContent>
    </Card>
  );
}

function truncateMiddle(s: string, max: number): string {
  if (s.length <= max) return s;
  const keep = Math.floor((max - 1) / 2);
  return `${s.slice(0, keep)}...${s.slice(-keep)}`;
}
