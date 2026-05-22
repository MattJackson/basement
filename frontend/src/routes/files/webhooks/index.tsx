import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/shared/ui/EmptyState";
import {
  useWebhooks,
  useDeleteWebhook,
  useEnableWebhook,
  useDisableWebhook,
  useTestWebhook,
  type Webhook,
} from "@/shared/api/queries";
import { useQueryClient } from "@tanstack/react-query";

// /files/webhooks — list of the caller's webhook subscriptions.
//
// Renders a wide table (same shape as the federation list) because each
// row carries five distinct pieces of state — events, filters, enabled,
// lastDelivery, failureCount — that don't fit the card grid cleanly. A
// 10s auto-refresh is inherited from useWebhooks() so lastDelivery +
// failureCount tick visibly without manual reload.
export const Route = createFileRoute("/files/webhooks/")({
  component: WebhooksListPage,
});

function WebhooksListPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: webhooks = [], isLoading } = useWebhooks();
  const deleteMutation = useDeleteWebhook();
  const enableMutation = useEnableWebhook();
  const disableMutation = useDisableWebhook();
  const testMutation = useTestWebhook();

  const handleDelete = async (wh: Webhook) => {
    const msg =
      `Delete webhook "${wh.name}"? Pending deliveries will be dropped. ` +
      `This cannot be undone.`;
    if (!confirm(msg)) return;
    try {
      await deleteMutation.mutateAsync(wh.id);
      qc.invalidateQueries({ queryKey: ["user", "webhooks"] });
    } catch (e) {
      console.error("delete webhook failed", e);
    }
  };

  const handleToggle = async (wh: Webhook) => {
    try {
      if (wh.enabled) {
        await disableMutation.mutateAsync(wh.id);
      } else {
        await enableMutation.mutateAsync(wh.id);
      }
      qc.invalidateQueries({ queryKey: ["user", "webhooks"] });
    } catch (e) {
      console.error("toggle webhook failed", e);
    }
  };

  const handleTest = async (wh: Webhook) => {
    try {
      await testMutation.mutateAsync(wh.id);
      // Soft-refresh so the lastDelivery cell flips when the engine
      // finishes the synthetic delivery (typically <1s on a healthy
      // target).
      qc.invalidateQueries({ queryKey: ["user", "webhooks"] });
    } catch (e) {
      console.error("test webhook failed", e);
    }
  };

  if (isLoading) {
    return <div className="space-y-6">Loading…</div>;
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
            Webhooks
          </h1>
          <p className="text-sm text-muted-foreground mt-1">
            HTTP callbacks fired when objects are created, modified, or
            deleted in your buckets. Each delivery is HMAC-signed so the
            receiver can verify the payload came from basement.
          </p>
        </div>
        <Button onClick={() => navigate({ to: "/files/webhooks/new" })}>
          + New webhook
        </Button>
      </header>

      {webhooks.length === 0 ? (
        <EmptyState
          icon="server"
          title="No webhooks configured"
          description="Create one to receive HTTP notifications on bucket events. Each delivery is HMAC-signed so your receiver can verify the payload."
          action={
            <Button onClick={() => navigate({ to: "/files/webhooks/new" })}>
              + New webhook
            </Button>
          }
        />
      ) : (
        <div className="rounded-lg border bg-card overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Target URL</TableHead>
                <TableHead>Events</TableHead>
                <TableHead>Filter</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last delivery</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {webhooks.map((wh) => (
                <WebhookRow
                  key={wh.id}
                  wh={wh}
                  onTest={handleTest}
                  onToggle={handleToggle}
                  onDelete={handleDelete}
                  isMutating={
                    deleteMutation.isPending ||
                    enableMutation.isPending ||
                    disableMutation.isPending ||
                    testMutation.isPending
                  }
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  );
}

function WebhookRow({
  wh,
  onTest,
  onToggle,
  onDelete,
  isMutating,
}: {
  wh: Webhook;
  onTest: (wh: Webhook) => void;
  onToggle: (wh: Webhook) => void;
  onDelete: (wh: Webhook) => void;
  isMutating: boolean;
}) {
  return (
    <TableRow>
      <TableCell>
        <Link
          to="/files/webhooks/$id"
          params={{ id: wh.id }}
          className="font-medium hover:underline"
        >
          {wh.name}
        </Link>
      </TableCell>
      <TableCell
        className="font-mono text-xs text-muted-foreground max-w-[260px] truncate"
        title={wh.targetUrl}
      >
        {wh.targetUrl}
      </TableCell>
      <TableCell>
        <div className="flex flex-wrap gap-1">
          {wh.events.map((ev) => (
            <span
              key={ev}
              className="rounded-md border bg-muted/30 px-1.5 py-0.5 font-mono text-[10px]"
            >
              {ev}
            </span>
          ))}
        </div>
      </TableCell>
      <TableCell className="text-xs font-mono">
        <FilterCell wh={wh} />
      </TableCell>
      <TableCell>
        <StatusBadges wh={wh} />
      </TableCell>
      <TableCell>
        <LastDeliveryCell wh={wh} />
      </TableCell>
      <TableCell className="text-right whitespace-nowrap">
        <Button
          variant="outline"
          size="sm"
          onClick={() => onTest(wh)}
          disabled={!wh.enabled || isMutating}
          data-testid={`test-button-${wh.id}`}
          className="mr-1"
        >
          Test
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => onToggle(wh)}
          disabled={isMutating}
          data-testid={`toggle-button-${wh.id}`}
          className="mr-1"
        >
          {wh.enabled ? "Disable" : "Enable"}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => onDelete(wh)}
          disabled={isMutating}
          data-testid={`delete-button-${wh.id}`}
        >
          Delete
        </Button>
      </TableCell>
    </TableRow>
  );
}

// FilterCell renders a one-line summary of the BucketFilter +
// PrefixFilter combo. "All regions" when neither is set; otherwise
// region/bucket pair with the prefix appended if present.
function FilterCell({ wh }: { wh: Webhook }) {
  const parts: string[] = [];
  if (wh.bucketFilter?.regionId) {
    if (wh.bucketFilter.bucket) {
      parts.push(`${wh.bucketFilter.regionId}/${wh.bucketFilter.bucket}`);
    } else {
      parts.push(`${wh.bucketFilter.regionId}/*`);
    }
  }
  if (wh.prefixFilter) {
    parts.push(`prefix=${wh.prefixFilter}`);
  }
  if (parts.length === 0) {
    return <span className="text-muted-foreground italic">all regions</span>;
  }
  return <span>{parts.join("  ")}</span>;
}

function StatusBadges({ wh }: { wh: Webhook }) {
  return (
    <div className="flex items-center gap-1">
      <span
        className={`inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs font-medium ${
          wh.enabled
            ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
            : "border-muted-foreground/40 bg-muted/30 text-muted-foreground"
        }`}
        data-testid={`status-pill-${wh.id}`}
      >
        <span className="h-1.5 w-1.5 rounded-full bg-current opacity-80" />
        {wh.enabled ? "enabled" : "disabled"}
      </span>
      {wh.failureCount && wh.failureCount > 0 ? (
        <span
          className="inline-flex items-center rounded-md border border-red-500/40 bg-red-500/10 px-1.5 py-0.5 text-xs font-medium text-red-700 dark:text-red-400"
          data-testid={`failure-badge-${wh.id}`}
          title={`${wh.failureCount} consecutive delivery failures since the last success.`}
        >
          {wh.failureCount} fail
        </span>
      ) : null}
    </div>
  );
}

function LastDeliveryCell({ wh }: { wh: Webhook }) {
  const ld = wh.lastDelivery;
  if (!ld) {
    return (
      <span className="text-xs text-muted-foreground">never delivered</span>
    );
  }
  const when = new Date(ld.deliveredAt).toLocaleString();
  return (
    <div className="text-xs">
      <div className="flex items-center gap-1">
        <span
          className={
            ld.success ? "text-emerald-700 dark:text-emerald-400" : "text-red-700 dark:text-red-400"
          }
        >
          {ld.success ? "200" : `HTTP ${ld.httpStatus || "—"}`}
        </span>
        <span className="text-muted-foreground">·</span>
        <span className="font-mono text-muted-foreground">
          {ld.durationMs}ms
        </span>
      </div>
      <div className="text-muted-foreground whitespace-nowrap">{when}</div>
    </div>
  );
}
