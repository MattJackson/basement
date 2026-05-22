import { useState } from "react";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  useWebhook,
  useDeleteWebhook,
  useTestWebhook,
  useEnableWebhook,
  useDisableWebhook,
  useUpdateWebhook,
  useUserRegions,
  WEBHOOK_EVENT_TYPES,
  type Webhook,
  type WebhookCreateRequest,
  type WebhookEventType,
  type WebhookMintResponse,
} from "@/shared/api/queries";
import { WebhookSecretShownOnceDialog } from "@/shared/ui/WebhookSecretShownOnceDialog";
import { useQueryClient } from "@tanstack/react-query";

// /files/webhooks/$id — webhook detail page.
//
// Three sections:
//   - Configuration card: target URL, events, filter, prefix, secret
//     status (always "redacted"), created/updated stamps. "Edit" flips
//     the card into inline edit mode; saving rotates the secret iff the
//     operator typed a new value in the password field.
//   - Last delivery card: success/HTTP/duration/error from the most
//     recent attempt + a "Test webhook" button that fires a synthetic
//     event so the operator can validate their target without waiting.
//   - Header: enable/disable toggle, status pill, failure-count badge,
//     delete button.
//
// The page polls on a 5s interval (inherited from useWebhook) so the
// Last-delivery card updates within one engine tick of a real or
// synthetic delivery completing.
export const Route = createFileRoute("/files/webhooks/$id/")({
  component: WebhookDetailPage,
});

function WebhookDetailPage() {
  const { id } = Route.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: webhook, isLoading, error } = useWebhook(id);
  const { data: regions = [] } = useUserRegions();
  const testMutation = useTestWebhook();
  const enableMutation = useEnableWebhook();
  const disableMutation = useDisableWebhook();
  const deleteMutation = useDeleteWebhook();
  const updateMutation = useUpdateWebhook();

  const [editing, setEditing] = useState(false);
  const [mintResult, setMintResult] = useState<WebhookMintResponse | null>(null);

  if (isLoading) return <div className="space-y-6">Loading…</div>;
  if (error || !webhook) {
    return (
      <div className="space-y-6">
        <Link
          to="/files/webhooks"
          className="text-sm text-muted-foreground hover:underline"
        >
          ← Back to webhooks
        </Link>
        <p className="text-sm text-destructive">
          Webhook not found or you don't have access.
        </p>
      </div>
    );
  }

  const regionAlias = (rid: string): string => {
    const r = regions.find((rr) => rr.id === rid);
    return r?.alias || r?.endpoint || rid || "(any region)";
  };

  const handleTest = async () => {
    try {
      await testMutation.mutateAsync(webhook.id);
      // The synthetic delivery completes asynchronously; the 5s poll
      // surfaces the result. A short follow-up invalidate makes the
      // first refresh land sooner.
      setTimeout(() => {
        qc.invalidateQueries({ queryKey: ["user", "webhooks", webhook.id] });
      }, 1000);
    } catch (e) {
      console.error("test webhook failed", e);
    }
  };

  const handleToggle = async () => {
    try {
      if (webhook.enabled) {
        await disableMutation.mutateAsync(webhook.id);
      } else {
        await enableMutation.mutateAsync(webhook.id);
      }
      qc.invalidateQueries({ queryKey: ["user", "webhooks", webhook.id] });
      qc.invalidateQueries({ queryKey: ["user", "webhooks"] });
    } catch (e) {
      console.error("toggle webhook failed", e);
    }
  };

  const handleDelete = async () => {
    const msg =
      `Delete webhook "${webhook.name}"? Pending deliveries will be ` +
      `dropped. This cannot be undone.`;
    if (!confirm(msg)) return;
    try {
      await deleteMutation.mutateAsync(webhook.id);
      navigate({ to: "/files/webhooks" });
    } catch (e) {
      console.error("delete webhook failed", e);
    }
  };

  const handleSaveEdit = async (patch: WebhookCreateRequest) => {
    try {
      const res = await updateMutation.mutateAsync({ id: webhook.id, body: patch });
      qc.invalidateQueries({ queryKey: ["user", "webhooks", webhook.id] });
      qc.invalidateQueries({ queryKey: ["user", "webhooks"] });
      setEditing(false);
      // If the response carries a non-empty Secret the server rotated
      // and we need to show the shown-once dialog. Otherwise the patch
      // was secret-preserving and we're done.
      if ("secret" in res && (res as WebhookMintResponse).secret) {
        setMintResult(res as WebhookMintResponse);
      }
    } catch (e) {
      console.error("update webhook failed", e);
    }
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <Link
        to="/files/webhooks"
        className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
      >
        ← Back to webhooks
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
            {webhook.name}
          </h1>
          <p
            className="text-sm text-muted-foreground mt-1 font-mono truncate max-w-md"
            title={webhook.targetUrl}
          >
            {webhook.targetUrl}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <StatusBadge enabled={webhook.enabled} />
          {webhook.failureCount && webhook.failureCount > 0 ? (
            <span
              className="inline-flex items-center rounded-md border border-red-500/40 bg-red-500/10 px-2 py-1 text-xs font-medium text-red-700 dark:text-red-400"
              data-testid="failure-count-badge"
              title="Consecutive delivery failures since the last success."
            >
              {webhook.failureCount} consecutive failures
            </span>
          ) : null}
        </div>
      </header>

      <section className="rounded-lg border bg-card p-6 space-y-4">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-medium text-muted-foreground">
            Configuration
          </h2>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={handleToggle}
              disabled={enableMutation.isPending || disableMutation.isPending}
              data-testid="toggle-button"
            >
              {webhook.enabled ? "Disable" : "Enable"}
            </Button>
            {!editing && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditing(true)}
                data-testid="edit-button"
              >
                Edit
              </Button>
            )}
            <Button
              variant="ghost"
              size="sm"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
              data-testid="delete-button"
            >
              Delete
            </Button>
          </div>
        </div>

        {editing ? (
          <EditForm
            initial={webhook}
            regions={regions.map((r) => ({
              id: r.id,
              alias: r.alias,
              endpoint: r.endpoint,
            }))}
            isSaving={updateMutation.isPending}
            error={updateMutation.error}
            onCancel={() => setEditing(false)}
            onSave={handleSaveEdit}
          />
        ) : (
          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">Target URL</dt>
            <dd className="font-mono break-all">{webhook.targetUrl}</dd>
            <dt className="text-muted-foreground">Events</dt>
            <dd>
              <div className="flex flex-wrap gap-1">
                {webhook.events.map((ev) => (
                  <span
                    key={ev}
                    className="rounded-md border bg-muted/30 px-1.5 py-0.5 font-mono text-[10px]"
                  >
                    {ev}
                  </span>
                ))}
              </div>
            </dd>
            <dt className="text-muted-foreground">Region filter</dt>
            <dd className="font-mono">
              {webhook.bucketFilter?.regionId
                ? regionAlias(webhook.bucketFilter.regionId)
                : "all regions"}
            </dd>
            <dt className="text-muted-foreground">Bucket filter</dt>
            <dd className="font-mono">
              {webhook.bucketFilter?.bucket || "all buckets"}
            </dd>
            <dt className="text-muted-foreground">Prefix filter</dt>
            <dd className="font-mono">{webhook.prefixFilter || "(none)"}</dd>
            <dt className="text-muted-foreground">Secret</dt>
            <dd
              className="font-mono text-muted-foreground"
              data-testid="secret-status"
            >
              {webhook.hasSecret ? "redacted — saved on server" : "(none)"}
            </dd>
            <dt className="text-muted-foreground">Created</dt>
            <dd>{new Date(webhook.createdAt).toLocaleString()}</dd>
            <dt className="text-muted-foreground">Updated</dt>
            <dd>{new Date(webhook.updatedAt).toLocaleString()}</dd>
          </dl>
        )}
      </section>

      <section className="rounded-lg border bg-card p-6 space-y-4">
        <div className="flex items-center justify-between gap-2">
          <h2 className="text-sm font-medium text-muted-foreground">
            Last delivery
          </h2>
          <Button
            variant="outline"
            size="sm"
            onClick={handleTest}
            disabled={!webhook.enabled || testMutation.isPending}
            data-testid="test-button"
          >
            {testMutation.isPending ? "Queueing…" : "Test webhook"}
          </Button>
        </div>
        <LastDeliveryCard webhook={webhook} />
        {testMutation.data && (
          <div
            className="rounded-md border border-emerald-500/40 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-400"
            data-testid="test-queued-banner"
          >
            Test event queued ({testMutation.data.event}). The delivery
            result will appear here within a few seconds.
          </div>
        )}
        {testMutation.error && (
          <div
            className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
            data-testid="test-error"
          >
            {(testMutation.error as Error).message.replace(/^[A-Z_]+:\s*/, "")}
          </div>
        )}
      </section>

      {mintResult && (
        <WebhookSecretShownOnceDialog
          result={mintResult}
          mode="rotate"
          onDone={() => setMintResult(null)}
        />
      )}
    </div>
  );
}

function StatusBadge({ enabled }: { enabled: boolean }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium ${
        enabled
          ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400"
          : "border-muted-foreground/40 bg-muted/30 text-muted-foreground"
      }`}
      data-testid="status-badge"
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {enabled ? "enabled" : "disabled"}
    </span>
  );
}

function LastDeliveryCard({ webhook }: { webhook: Webhook }) {
  const ld = webhook.lastDelivery;
  if (!ld) {
    return (
      <p
        className="text-sm text-muted-foreground"
        data-testid="last-delivery-empty"
      >
        No deliveries yet. Click "Test webhook" to send a synthetic event,
        or wait for a matching bucket event to fire the subscription.
      </p>
    );
  }
  return (
    <dl
      className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm"
      data-testid="last-delivery-detail"
    >
      <dt className="text-muted-foreground">Delivered at</dt>
      <dd>{new Date(ld.deliveredAt).toLocaleString()}</dd>
      <dt className="text-muted-foreground">HTTP status</dt>
      <dd className="font-mono">
        <span
          className={
            ld.success
              ? "text-emerald-700 dark:text-emerald-400"
              : "text-red-700 dark:text-red-400"
          }
          data-testid="last-delivery-status"
        >
          {ld.httpStatus || (ld.success ? 200 : "—")}
        </span>{" "}
        <span className="text-muted-foreground">
          ({ld.success ? "success" : "failed"})
        </span>
      </dd>
      <dt className="text-muted-foreground">Duration</dt>
      <dd className="font-mono">{ld.durationMs}ms</dd>
      {ld.error && (
        <>
          <dt className="text-muted-foreground">Error</dt>
          <dd
            className="text-destructive break-all"
            data-testid="last-delivery-error"
          >
            {ld.error}
          </dd>
        </>
      )}
    </dl>
  );
}

interface EditFormProps {
  initial: Webhook;
  regions: { id: string; alias?: string; endpoint: string }[];
  isSaving: boolean;
  error: Error | null;
  onCancel: () => void;
  onSave: (body: WebhookCreateRequest) => void;
}

// EditForm is the inline edit shape for the configuration card. The
// secret field is empty by default — typing into it rotates the secret
// on save, leaving it blank preserves the existing one. Same semantics
// as the v1.7.0c service-account rotate flow.
function EditForm({
  initial,
  regions,
  isSaving,
  error,
  onCancel,
  onSave,
}: EditFormProps) {
  const [targetUrl, setTargetUrl] = useState(initial.targetUrl);
  const [selectedEvents, setSelectedEvents] = useState<Set<WebhookEventType>>(
    new Set(initial.events),
  );
  const [regionId, setRegionId] = useState(initial.bucketFilter?.regionId || "");
  const [bucket, setBucket] = useState(initial.bucketFilter?.bucket || "");
  const [prefix, setPrefix] = useState(initial.prefixFilter || "");
  const [secret, setSecret] = useState("");

  const toggleEvent = (ev: WebhookEventType) => {
    setSelectedEvents((prev) => {
      const next = new Set(prev);
      if (next.has(ev)) next.delete(ev);
      else next.add(ev);
      return next;
    });
  };

  const canSave =
    targetUrl.trim() !== "" && selectedEvents.size > 0 && !isSaving;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSave) return;
    const body: WebhookCreateRequest = {
      name: initial.name,
      targetUrl: targetUrl.trim(),
      events: Array.from(selectedEvents),
      bucketFilter: regionId ? { regionId, bucket: bucket || "" } : undefined,
      prefixFilter: prefix || undefined,
      secret: secret.trim() || undefined,
      enabled: initial.enabled,
    };
    onSave(body);
  };

  return (
    <form
      onSubmit={handleSubmit}
      className="space-y-4"
      data-testid="webhook-edit-form"
    >
      <div className="grid gap-2">
        <Label htmlFor="edit-target-url">Target URL</Label>
        <Input
          id="edit-target-url"
          value={targetUrl}
          onChange={(e) => setTargetUrl(e.target.value)}
          data-testid="edit-target-url-input"
        />
      </div>

      <div className="grid gap-2">
        <Label>Events</Label>
        <div className="grid gap-1">
          {WEBHOOK_EVENT_TYPES.map((ev) => (
            <label
              key={ev}
              className="flex items-center gap-2 text-sm cursor-pointer"
            >
              <Checkbox
                checked={selectedEvents.has(ev)}
                onCheckedChange={() => toggleEvent(ev)}
                data-testid={`edit-event-checkbox-${ev}`}
              />
              <span className="font-mono text-xs">{ev}</span>
            </label>
          ))}
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        <div className="grid gap-2">
          <Label htmlFor="edit-region">Region filter</Label>
          <select
            id="edit-region"
            value={regionId}
            onChange={(e) => {
              setRegionId(e.target.value);
              setBucket("");
            }}
            className="w-full rounded-md border bg-background px-3 py-2 text-sm"
          >
            <option value="">All my regions</option>
            {regions.map((r) => (
              <option key={r.id} value={r.id}>
                {r.alias || r.endpoint}
              </option>
            ))}
          </select>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="edit-bucket">Bucket filter</Label>
          <Input
            id="edit-bucket"
            value={bucket}
            onChange={(e) => setBucket(e.target.value)}
            placeholder="all buckets in region"
            disabled={!regionId}
          />
        </div>
      </div>

      <div className="grid gap-2">
        <Label htmlFor="edit-prefix">Prefix filter</Label>
        <Input
          id="edit-prefix"
          value={prefix}
          onChange={(e) => setPrefix(e.target.value)}
          placeholder="e.g. photos/"
        />
      </div>

      <div className="grid gap-2">
        <Label htmlFor="edit-secret">Rotate secret (optional)</Label>
        <Input
          id="edit-secret"
          type="password"
          value={secret}
          onChange={(e) => setSecret(e.target.value)}
          placeholder="leave blank to preserve existing secret"
          autoComplete="new-password"
          data-testid="edit-secret-input"
        />
        <p className="text-xs text-muted-foreground">
          Type a new value to rotate the signing key. Blank preserves
          the current secret. Any rotated value is shown exactly once on
          save.
        </p>
      </div>

      {error && (
        <div
          role="alert"
          className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          {error.message.replace(/^[A-Z_]+:\s*/, "")}
        </div>
      )}

      <div className="flex items-center justify-end gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={onCancel}
          disabled={isSaving}
        >
          Cancel
        </Button>
        <Button
          type="submit"
          disabled={!canSave}
          data-testid="edit-save-button"
        >
          {isSaving ? "Saving…" : "Save changes"}
        </Button>
      </div>
    </form>
  );
}
