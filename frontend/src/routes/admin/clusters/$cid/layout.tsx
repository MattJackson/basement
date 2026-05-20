import { createFileRoute, Link } from "@tanstack/react-router";
import { useState } from "react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import { humanizeBytes } from "@/shared/lib/format";
import { useLayout, useGetCluster } from "@/shared/api/queries";
import { useStageLayoutChange, useApplyLayout, useRevertLayout } from "@/shared/api/mutations";
import { adminPage } from "@/shared/layout/adminPage";

export const Route = createFileRoute("/admin/clusters/$cid/layout")({
  component: adminPage(ClusterLayoutScreen),
});

type RowDraft = {
  role: string;
  zone: string;
  capacityGB: string;
  tags: string;
};

const ROLE_OPTIONS = ["storage", "gateway", ""];

function ClusterLayoutScreen() {
  const { cid } = Route.useParams();
  const { data: cluster } = useGetCluster(cid);
  const { data: layout, isLoading, error } = useLayout(cid);
  const stageChange = useStageLayoutChange(cid);
  const applyLayout = useApplyLayout(cid);
  const revertLayout = useRevertLayout(cid);

  const [drafts, setDrafts] = useState<Record<string, RowDraft>>({});
  const [revertDialogOpen, setRevertDialogOpen] = useState(false);

  // Cluster drivers that don't own their cluster topology (AWS S3,
  // soon MinIO) return ErrUnsupported on layout calls. Surface this
  // gracefully instead of letting the editor render against an
  // empty/error state.
  const driver = cluster?.driver;
  const layoutUnsupported = driver === "aws-s3";

  if (layoutUnsupported) {
    return (
      <div className="space-y-6">
        <BackLink cid={cid} clusterLabel={cluster?.label} />
        <div className="rounded-lg border bg-card p-6 space-y-2">
          <h2 className="text-sm font-medium">Layout not supported</h2>
          <p className="text-sm text-muted-foreground max-w-prose">
            basement only exposes layout editing for backends that
            own their own cluster topology (Garage). The{" "}
            <Badge variant="secondary">{driver}</Badge> driver manages
            its layout internally — there&apos;s nothing to edit here.
          </p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <BackLink cid={cid} clusterLabel={cluster?.label} />
        <ErrorBanner message="Couldn't load layout." />
      </div>
    );
  }

  if (isLoading || !layout) {
    return (
      <div className="space-y-6">
        <BackLink cid={cid} clusterLabel={cluster?.label} />
        <Skeleton className="h-8 w-48" />
        <div className="rounded-lg border bg-card p-4">
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    );
  }

  const nodes = layout.nodes ?? [];
  const staged = layout.staged;

  const getDraft = (nodeId: string, fallback: { role?: string; zone?: string; capacity?: number; tags?: string[] }): RowDraft => {
    return (
      drafts[nodeId] ?? {
        role: fallback.role ?? "",
        zone: fallback.zone ?? "",
        capacityGB: fallback.capacity ? String(Math.round(fallback.capacity / 1024 ** 3)) : "",
        tags: (fallback.tags ?? []).join(", "),
      }
    );
  };

  const setDraft = (nodeId: string, patch: Partial<RowDraft>) => {
    setDrafts((d) => ({ ...d, [nodeId]: { ...getDraft(nodeId, {}), ...patch } }));
  };

  const handleStage = (nodeId: string) => {
    const draft = getDraft(nodeId, {});
    const capGB = parseFloat(draft.capacityGB);
    const tags = draft.tags
      .split(",")
      .map((t) => t.trim())
      .filter(Boolean);
    stageChange.mutate({
      nodeId,
      role: (draft.role || undefined) as "storage" | "gateway" | "" | undefined,
      zone: draft.zone || undefined,
      capacity: !isNaN(capGB) ? Math.round(capGB * 1024 ** 3) : undefined,
      tags: tags.length > 0 ? tags : undefined,
    });
  };

  const handleApply = () => applyLayout.mutate();
  const handleRevert = () => {
    revertLayout.mutate();
    setRevertDialogOpen(false);
  };

  return (
    <div className="space-y-6">
      <BackLink cid={cid} clusterLabel={cluster?.label} />

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
            Layout · {cluster?.label ?? cid.slice(0, 12)}
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            Edit per-node role, zone, capacity, and tags. Stage → preview → apply.
          </p>
        </div>
        <div className="text-sm text-muted-foreground">Version {layout.version}</div>
      </header>

      <section className="space-y-3">
        <h2 className="text-sm font-medium text-muted-foreground">Nodes</h2>
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Node</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Zone</TableHead>
                  <TableHead>Capacity (GB)</TableHead>
                  <TableHead>Tags</TableHead>
                  <TableHead className="w-24"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {nodes.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6}>
                      <EmptyState
                        icon="server"
                        title="No nodes"
                        description="This cluster reports no nodes yet."
                      />
                    </TableCell>
                  </TableRow>
                ) : (
                  nodes.map((n) => {
                    const draft = getDraft(n.id, { role: n.role, zone: n.zone, capacity: n.capacity, tags: n.tags });
                    return (
                      <TableRow key={n.id}>
                        <TableCell className="font-mono text-xs">{n.id.slice(0, 12)}</TableCell>
                        <TableCell>
                          <select
                            value={draft.role}
                            onChange={(e) => setDraft(n.id, { role: e.target.value })}
                            className="rounded-md border bg-background px-2 py-1 text-sm"
                          >
                            {ROLE_OPTIONS.map((r) => (
                              <option key={r} value={r}>
                                {r || "unassigned"}
                              </option>
                            ))}
                          </select>
                        </TableCell>
                        <TableCell>
                          <Input
                            value={draft.zone}
                            onChange={(e) => setDraft(n.id, { zone: e.target.value })}
                            placeholder="dc1"
                            className="w-24"
                          />
                        </TableCell>
                        <TableCell>
                          <Input
                            value={draft.capacityGB}
                            onChange={(e) => setDraft(n.id, { capacityGB: e.target.value })}
                            placeholder="1024"
                            type="number"
                            className="w-28"
                          />
                          {n.capacity ? (
                            <div className="text-xs text-muted-foreground mt-1">
                              now: {humanizeBytes(n.capacity)}
                            </div>
                          ) : null}
                        </TableCell>
                        <TableCell>
                          <Input
                            value={draft.tags}
                            onChange={(e) => setDraft(n.id, { tags: e.target.value })}
                            placeholder="ssd, fast"
                            className="w-40"
                          />
                        </TableCell>
                        <TableCell>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleStage(n.id)}
                            disabled={stageChange.isPending}
                          >
                            Stage
                          </Button>
                        </TableCell>
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
        </div>
      </section>

      {staged && staged.nodes && staged.nodes.length > 0 ? (
        <section className="space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-muted-foreground">
              Staged layout <span className="text-muted-foreground/60">(target version {staged.version ?? "?"})</span>
            </h2>
            <div className="flex gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setRevertDialogOpen(true)}
                disabled={revertLayout.isPending}
              >
                Revert
              </Button>
              <Button size="sm" onClick={handleApply} disabled={applyLayout.isPending}>
                {applyLayout.isPending ? "Applying…" : "Apply"}
              </Button>
            </div>
          </div>
          <div className="rounded-lg border bg-card p-4">
            <ul className="space-y-1.5 text-sm">
              {staged.nodes.map((n) => (
                <li key={n.id} className="flex items-center gap-2 text-muted-foreground">
                  <span className="font-mono text-xs">{n.id.slice(0, 12)}</span>
                  <Badge variant="secondary" className="capitalize">{n.role ?? "unassigned"}</Badge>
                  {n.zone ? <span>· {n.zone}</span> : null}
                  {n.capacity ? <span>· {humanizeBytes(n.capacity)}</span> : null}
                  {n.tags && n.tags.length > 0 ? <span>· {n.tags.join(", ")}</span> : null}
                </li>
              ))}
            </ul>
          </div>
        </section>
      ) : null}

      <AlertDialog open={revertDialogOpen} onOpenChange={(o) => !o && setRevertDialogOpen(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revert staged layout?</AlertDialogTitle>
            <AlertDialogDescription>
              Throws away any pending staged changes. The applied
              layout remains in place.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setRevertDialogOpen(false)}>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleRevert}>Revert</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function BackLink({ cid, clusterLabel }: { cid: string; clusterLabel?: string }) {
  return (
    <Link
      to="/admin/clusters/$cid"
      params={{ cid }}
      className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
    >
      ← {clusterLabel ?? "Back to cluster"}
    </Link>
  );
}
