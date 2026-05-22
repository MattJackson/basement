import { useMemo, useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  useUserRegions,
  useUserRegionBuckets,
  useCreateWebhook,
  WEBHOOK_EVENT_TYPES,
  type WebhookCreateRequest,
  type WebhookEventType,
  type WebhookMintResponse,
} from "@/shared/api/queries";
import { WebhookSecretShownOnceDialog } from "@/shared/ui/WebhookSecretShownOnceDialog";
import { useQueryClient } from "@tanstack/react-query";

// /files/webhooks/new — single-page create form for a new webhook
// subscription.
//
// Webhooks are simple enough that a one-screen form is the right
// shape: name + URL + event checkboxes + region/bucket scope + prefix
// + optional secret. Multi-step would feel performative for what is
// really five fields. Submit posts the create body and immediately
// opens the WebhookSecretShownOnceDialog with the freshly-minted
// secret; the operator must explicitly Done before the dialog dismisses
// + we navigate back to the list.
//
// Validation:
//   - Name: 3-64 chars of [A-Za-z0-9_-] — mirrors the backend
//     webhookNameRegex so the server never has the last word on a
//     locally-invalid form.
//   - Target URL: must parse, scheme must be http or https. An HTTPS
//     warning fires on http:// URLs (the backend accepts both, but the
//     UI nudges operators toward HTTPS).
//   - Events: at least one must be checked.
//   - Region / Bucket / Prefix: optional. Bucket select is disabled
//     until a region is picked.
//   - Secret: optional. Blank → server mints a 32-char hex token.
export const Route = createFileRoute("/files/webhooks/new")({
  component: NewWebhookPage,
});

function NewWebhookPage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data: regions = [] } = useUserRegions();
  const createMutation = useCreateWebhook();

  const [name, setName] = useState("");
  const [targetUrl, setTargetUrl] = useState("");
  const [selectedEvents, setSelectedEvents] = useState<Set<WebhookEventType>>(
    new Set(["object.created", "object.deleted"]),
  );
  const [regionId, setRegionId] = useState("");
  const [bucket, setBucket] = useState("");
  const [prefix, setPrefix] = useState("");
  const [secret, setSecret] = useState("");

  const [mintResult, setMintResult] = useState<WebhookMintResponse | null>(
    null,
  );

  const { data: bucketsList } = useUserRegionBuckets(regionId || null);

  const validation = useMemo(() => validate({
    name,
    targetUrl,
    events: selectedEvents,
  }), [name, targetUrl, selectedEvents]);

  const showHttpsWarning = useMemo(() => {
    const trimmed = targetUrl.trim();
    return trimmed.toLowerCase().startsWith("http://");
  }, [targetUrl]);

  const toggleEvent = (ev: WebhookEventType) => {
    setSelectedEvents((prev) => {
      const next = new Set(prev);
      if (next.has(ev)) {
        next.delete(ev);
      } else {
        next.add(ev);
      }
      return next;
    });
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!validation.ok) return;
    const body: WebhookCreateRequest = {
      name: name.trim(),
      targetUrl: targetUrl.trim(),
      events: Array.from(selectedEvents),
      bucketFilter:
        regionId
          ? { regionId, bucket: bucket || "" }
          : undefined,
      prefixFilter: prefix || undefined,
      secret: secret.trim() || undefined,
    };
    try {
      const res = await createMutation.mutateAsync(body);
      qc.invalidateQueries({ queryKey: ["user", "webhooks"] });
      setMintResult(res);
    } catch (err) {
      console.error("create webhook failed", err);
    }
  };

  const handleDoneWithSecret = () => {
    setMintResult(null);
    navigate({ to: "/files/webhooks" });
  };

  return (
    <div className="space-y-6 max-w-3xl">
      <Link
        to="/files/webhooks"
        className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
      >
        ← Back to webhooks
      </Link>

      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          New webhook
        </h1>
        <p className="text-sm text-muted-foreground mt-1">
          Subscribe to bucket events. Deliveries POST a JSON envelope to
          your target URL with an HMAC signature so the receiver can
          verify authenticity.
        </p>
      </header>

      <form
        onSubmit={handleSubmit}
        className="space-y-6"
        data-testid="new-webhook-form"
      >
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Identity</h2>
          <div className="grid gap-2">
            <Label htmlFor="name">Name *</Label>
            <Input
              id="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. ci-deploy, slack-alerts"
              autoFocus
              data-testid="webhook-name-input"
            />
            <p className="text-xs text-muted-foreground">
              Letters, numbers, underscore, and dash. 3-64 chars.
            </p>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="targetUrl">Target URL *</Label>
            <Input
              id="targetUrl"
              value={targetUrl}
              onChange={(e) => setTargetUrl(e.target.value)}
              placeholder="https://ci.example.com/hooks/basement"
              data-testid="webhook-target-url-input"
            />
            {showHttpsWarning && (
              <p
                className="text-xs text-amber-700 dark:text-amber-400"
                data-testid="webhook-https-warning"
              >
                Heads up: plain http URLs send the signed body over the
                wire in cleartext. Prefer https for any non-local target.
              </p>
            )}
          </div>
        </section>

        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Events</h2>
          <p className="text-xs text-muted-foreground">
            At least one event must be selected. The webhook fires only
            for matching event types after applying any filter below.
          </p>
          <div className="grid gap-2">
            {WEBHOOK_EVENT_TYPES.map((ev) => (
              <label
                key={ev}
                className="flex items-start gap-2 text-sm cursor-pointer"
              >
                <Checkbox
                  checked={selectedEvents.has(ev)}
                  onCheckedChange={() => toggleEvent(ev)}
                  data-testid={`event-checkbox-${ev}`}
                />
                <span>
                  <span className="font-mono text-xs">{ev}</span>
                  <span className="ml-2 text-muted-foreground text-xs">
                    {eventBlurb(ev)}
                  </span>
                </span>
              </label>
            ))}
          </div>
        </section>

        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">
            Filter (optional)
          </h2>
          <p className="text-xs text-muted-foreground">
            Limit the webhook to a single region, a single bucket within
            that region, or a key prefix. Leaving everything blank
            subscribes to every event the engine sees for your buckets.
          </p>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="regionId">Region</Label>
              <select
                id="regionId"
                value={regionId}
                onChange={(e) => {
                  setRegionId(e.target.value);
                  setBucket("");
                }}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm"
                data-testid="webhook-region-select"
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
              <Label htmlFor="bucket">Bucket</Label>
              <select
                id="bucket"
                value={bucket}
                disabled={!regionId}
                onChange={(e) => setBucket(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
                data-testid="webhook-bucket-select"
              >
                <option value="">All buckets in region</option>
                {bucketsList?.buckets.map((b) => (
                  <option key={b.id} value={b.id}>
                    {b.aliases?.[0] || b.id.slice(0, 12)}
                  </option>
                ))}
              </select>
            </div>
            <div className="grid gap-2 sm:col-span-2">
              <Label htmlFor="prefix">Prefix</Label>
              <Input
                id="prefix"
                value={prefix}
                onChange={(e) => setPrefix(e.target.value)}
                placeholder="e.g. photos/"
                data-testid="webhook-prefix-input"
              />
              <p className="text-xs text-muted-foreground">
                Match keys starting with this string. Empty = no prefix
                filter.
              </p>
            </div>
          </div>
        </section>

        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">
            Signing secret (optional)
          </h2>
          <div className="grid gap-2">
            <Label htmlFor="secret">Secret</Label>
            <Input
              id="secret"
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              placeholder="leave blank to auto-generate"
              data-testid="webhook-secret-input"
              autoComplete="new-password"
            />
            <p className="text-xs text-muted-foreground">
              Used to HMAC-SHA256 each outbound body. 16+ chars
              recommended. Leave blank and we'll mint a 32-character hex
              token for you — shown exactly once on the next screen.
            </p>
          </div>
        </section>

        {createMutation.error && (
          <div
            role="alert"
            className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
            data-testid="webhook-create-error"
          >
            {(createMutation.error as Error).message.replace(/^[A-Z_]+:\s*/, "")}
          </div>
        )}

        {!validation.ok && validation.message && (
          <div className="text-xs text-muted-foreground" data-testid="webhook-validation-hint">
            {validation.message}
          </div>
        )}

        <div className="flex items-center justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => navigate({ to: "/files/webhooks" })}
            disabled={createMutation.isPending}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={!validation.ok || createMutation.isPending}
            data-testid="webhook-create-submit"
          >
            {createMutation.isPending ? "Creating…" : "Create webhook"}
          </Button>
        </div>
      </form>

      {mintResult && (
        <WebhookSecretShownOnceDialog
          result={mintResult}
          mode="create"
          onDone={handleDoneWithSecret}
        />
      )}
    </div>
  );
}

interface ValidateInput {
  name: string;
  targetUrl: string;
  events: Set<WebhookEventType>;
}

interface ValidationResult {
  ok: boolean;
  message: string;
}

// validate runs the same shape-checks the backend handler does so a
// half-filled form can't submit (and the operator gets immediate
// feedback). The backend remains the authority for any rule we don't
// duplicate (e.g. region-ownership) — those still surface via the
// createMutation.error banner.
function validate(input: ValidateInput): ValidationResult {
  const name = input.name.trim();
  if (!/^[A-Za-z0-9_-]{3,64}$/.test(name)) {
    return {
      ok: false,
      message: "Name must be 3-64 characters of letters, digits, dashes, or underscores.",
    };
  }
  const target = input.targetUrl.trim();
  if (!target) {
    return { ok: false, message: "Target URL is required." };
  }
  try {
    const u = new URL(target);
    if (u.protocol !== "http:" && u.protocol !== "https:") {
      return { ok: false, message: "Target URL must use http or https." };
    }
  } catch {
    return { ok: false, message: "Target URL must be a fully-qualified URL." };
  }
  if (input.events.size === 0) {
    return { ok: false, message: "Pick at least one event type." };
  }
  return { ok: true, message: "" };
}

// eventBlurb returns the short operator-facing description rendered
// next to each checkbox. Mirrors the comments in
// internal/webhook/types.go on the EventType constants so the FE
// vocabulary tracks the backend's intent.
function eventBlurb(ev: WebhookEventType): string {
  switch (ev) {
    case "object.created":
      return "an object is uploaded to a matching bucket";
    case "object.deleted":
      return "an object is removed from a matching bucket";
    case "object.modified":
      return "an existing object is overwritten";
    default:
      return "";
  }
}
