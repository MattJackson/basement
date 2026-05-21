import { useState } from "react";
import { Link, useNavigate } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
import {
  useBucketLifecycle,
  usePutBucketLifecycle,
} from "@/shared/api/queries";
import type { LifecycleRule, LifecycleCapabilities } from "@/shared/api/queries";

type ActionKind = "expire" | "transition" | "abort_mpu" | "noncurrent";

interface FormState {
  id: string;
  status: "Enabled" | "Disabled";
  prefix: string;
  action: ActionKind;
  days: string;
  tier: string;
}

/**
 * LifecycleRuleEditor is the per-rule form used by both the "new" and
 * "edit" routes. It loads the bucket's existing policy (so the
 * editor can capability-gate fields + so save can splice the rule
 * into the existing array) and renders the form fields. Save replaces
 * the entire policy via PUT — drivers either accept the full
 * replacement (S3 / MinIO) or treat the lifecycleRules field as a
 * full-overwrite (Garage v2), so this is the right primitive.
 *
 * Per operator design rule: rule has 4+ fields (status, prefix,
 * action, days, tier) so the editor is a ROUTE, not a dialog.
 */
export function LifecycleRuleEditor({
  cid,
  bid,
  ruleId,
}: {
  cid: string;
  bid: string;
  ruleId?: string;
}) {
  const { data, isLoading, error } = useBucketLifecycle(cid, bid);

  if (isLoading || !data) {
    return (
      <div className="space-y-6 max-w-2xl">
        <BackLink cid={cid} bid={bid} />
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6 max-w-2xl">
        <BackLink cid={cid} bid={bid} />
        <ErrorBanner message="Couldn't load lifecycle policy." />
      </div>
    );
  }

  if (!data.capabilities.supported) {
    return (
      <div className="space-y-6 max-w-2xl">
        <BackLink cid={cid} bid={bid} />
        <ErrorBanner message="Lifecycle policies are not supported on this driver." />
      </div>
    );
  }

  // Loaded-and-supported: hand off to the form component, which can
  // safely seed its state from data via lazy useState — no effect
  // needed, so the react-hooks/set-state-in-effect lint stays happy
  // and the form doesn't bounce on the first render.
  return (
    <LifecycleRuleEditorForm
      cid={cid}
      bid={bid}
      ruleId={ruleId}
      initialData={data}
    />
  );
}

function LifecycleRuleEditorForm({
  cid,
  bid,
  ruleId,
  initialData,
}: {
  cid: string;
  bid: string;
  ruleId?: string;
  initialData: NonNullable<ReturnType<typeof useBucketLifecycle>["data"]>;
}) {
  const navigate = useNavigate();
  const putMutation = usePutBucketLifecycle(cid, bid);

  const isEdit = !!ruleId;
  const caps = initialData.capabilities;

  // Seed form state once from initialData. Subsequent updates to the
  // backing query don't re-seed the form (operator's in-flight edits
  // win), which matches every other edit page in the app.
  const [form, setForm] = useState<FormState>(() => {
    if (isEdit) {
      const existing = initialData.rules.find((r) => r.id === ruleId);
      if (existing) {
        const action = inferAction(existing);
        return {
          id: existing.id,
          status: existing.status,
          prefix: existing.prefix ?? "",
          action,
          days: String(daysForAction(existing, action) ?? ""),
          tier: existing.transitionTier ?? caps.transitionTiers[0] ?? "",
        };
      }
    }
    return {
      id: "",
      status: "Enabled",
      prefix: "",
      action: "expire",
      days: "30",
      tier: caps.transitionTiers[0] ?? "",
    };
  });

  const cancelHref = {
    to: "/admin/clusters/$cid/buckets/$id" as const,
    params: { cid, id: bid },
  };

  const data = initialData;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const rule = formToRule(form, caps);
    if (!rule) return;

    // Splice the rule into the existing policy: replace if editing,
    // append if new. The whole array is sent — the backend overwrites
    // the policy with what we PUT.
    const others = data.rules.filter((r) => r.id !== rule.id);
    if (isEdit && !data.rules.some((r) => r.id === ruleId)) {
      // Editing a rule that's gone — give up gracefully.
      navigate(cancelHref);
      return;
    }
    const next = [...others, rule];
    putMutation.mutate(
      { rules: next },
      {
        onSuccess: () => {
          navigate(cancelHref);
        },
      },
    );
  };

  const daysLabelMap: Record<ActionKind, string> = {
    expire: "Delete after (days)",
    transition: "Transition after (days)",
    abort_mpu: "Cancel incomplete uploads after (days)",
    noncurrent: "Delete noncurrent versions after (days)",
  };

  return (
    <div className="space-y-6 max-w-2xl">
      <BackLink cid={cid} bid={bid} />

      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          {isEdit ? "Edit lifecycle rule" : "New lifecycle rule"}
        </h1>
        <p className="text-sm text-muted-foreground mt-1">
          Express what happens to objects as they age — basement translates this into the
          backend's native lifecycle format.
        </p>
      </header>

      {putMutation.isError && (
        <ErrorBanner message={String(putMutation.error?.message ?? "Couldn't save rule.")} />
      )}

      <form onSubmit={handleSubmit} className="space-y-6">
        <section className="space-y-2">
          <Label htmlFor="ruleId">Rule ID</Label>
          <Input
            id="ruleId"
            value={form.id}
            onChange={(e) => setForm({ ...form, id: e.target.value })}
            placeholder="e.g. archive-logs-after-30d"
            required
            disabled={isEdit}
          />
          <p className="text-xs text-muted-foreground">
            Pick a memorable name. Used to identify this rule when editing later.
          </p>
        </section>

        <section className="space-y-2">
          <Label>Status</Label>
          <div className="flex items-center gap-4">
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="status"
                value="Enabled"
                checked={form.status === "Enabled"}
                onChange={() => setForm({ ...form, status: "Enabled" })}
              />
              Enabled
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="radio"
                name="status"
                value="Disabled"
                checked={form.status === "Disabled"}
                onChange={() => setForm({ ...form, status: "Disabled" })}
              />
              Disabled
            </label>
          </div>
        </section>

        <section className="space-y-2">
          <Label htmlFor="prefix">Prefix (optional)</Label>
          <Input
            id="prefix"
            value={form.prefix}
            onChange={(e) => setForm({ ...form, prefix: e.target.value })}
            placeholder="e.g. logs/"
          />
          <p className="text-xs text-muted-foreground">
            Only objects whose key starts with this prefix are affected. Leave empty to apply to
            every object in the bucket.
          </p>
        </section>

        <section className="space-y-2">
          <Label>Action</Label>
          <div className="grid gap-2">
            <ActionRadio
              checked={form.action === "expire"}
              disabled={!caps.expiration}
              label="Expire (delete the object)"
              onSelect={() => setForm({ ...form, action: "expire" })}
            />
            <ActionRadio
              checked={form.action === "transition"}
              disabled={!caps.transition}
              label="Transition to a colder storage tier"
              onSelect={() => setForm({ ...form, action: "transition" })}
            />
            <ActionRadio
              checked={form.action === "abort_mpu"}
              disabled={!caps.abortMultipartDays}
              label="Cancel incomplete multipart uploads"
              onSelect={() => setForm({ ...form, action: "abort_mpu" })}
            />
            <ActionRadio
              checked={form.action === "noncurrent"}
              disabled={!caps.noncurrentDays}
              label="Expire noncurrent versions (versioned buckets only)"
              onSelect={() => setForm({ ...form, action: "noncurrent" })}
            />
          </div>
          <p className="text-xs text-muted-foreground">
            Grayed-out actions aren't supported by this driver.
          </p>
        </section>

        <section className="space-y-2">
          <Label htmlFor="days">{daysLabelMap[form.action]}</Label>
          <Input
            id="days"
            type="number"
            min={1}
            max={36500}
            value={form.days}
            onChange={(e) => setForm({ ...form, days: e.target.value })}
            required
          />
        </section>

        {form.action === "transition" && (
          <section className="space-y-2">
            <Label htmlFor="tier">Tier</Label>
            <select
              id="tier"
              className="border border-input bg-background rounded-md h-9 px-3 text-sm w-full max-w-xs"
              value={form.tier}
              onChange={(e) => setForm({ ...form, tier: e.target.value })}
              required
            >
              {caps.transitionTiers.map((t) => (
                <option key={t} value={t}>
                  {t}
                </option>
              ))}
            </select>
          </section>
        )}

        <div className="flex items-center gap-3">
          <Button type="submit" disabled={putMutation.isPending || !form.id.trim()}>
            {putMutation.isPending ? "Saving…" : isEdit ? "Save changes" : "Create rule"}
          </Button>
          <Link
            {...cancelHref}
            className="inline-flex items-center justify-center rounded-lg border border-border bg-background hover:bg-muted h-8 px-2.5 text-sm font-medium transition-colors"
          >
            Cancel
          </Link>
        </div>
      </form>
    </div>
  );
}

function ActionRadio({
  checked,
  disabled,
  label,
  onSelect,
}: {
  checked: boolean;
  disabled: boolean;
  label: string;
  onSelect: () => void;
}) {
  return (
    <label
      className={`flex items-center gap-2 text-sm rounded-md border px-3 py-2 ${
        disabled
          ? "cursor-not-allowed opacity-50"
          : checked
          ? "border-foreground/40 bg-muted/40 cursor-pointer"
          : "hover:bg-muted/30 cursor-pointer"
      }`}
    >
      <input
        type="radio"
        name="action"
        checked={checked}
        disabled={disabled}
        onChange={onSelect}
      />
      <span>{label}</span>
    </label>
  );
}

function BackLink({ cid, bid }: { cid: string; bid: string }) {
  return (
    <Link
      to="/admin/clusters/$cid/buckets/$id"
      params={{ cid, id: bid }}
      className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
    >
      ← Back to bucket
    </Link>
  );
}

// formToRule assembles a wire-format LifecycleRule from the form
// state, honouring the chosen action so unused fields stay undefined.
function formToRule(form: FormState, caps: LifecycleCapabilities): LifecycleRule | null {
  const days = parseInt(form.days, 10);
  if (!Number.isFinite(days) || days < 1) return null;

  const rule: LifecycleRule = {
    id: form.id.trim(),
    status: form.status,
    prefix: form.prefix.trim() || undefined,
  };

  switch (form.action) {
    case "expire":
      if (!caps.expiration) return null;
      rule.expirationDays = days;
      break;
    case "transition":
      if (!caps.transition || !form.tier) return null;
      rule.transitionDays = days;
      rule.transitionTier = form.tier;
      break;
    case "abort_mpu":
      if (!caps.abortMultipartDays) return null;
      rule.abortMultipartDays = days;
      break;
    case "noncurrent":
      if (!caps.noncurrentDays) return null;
      rule.noncurrentDays = days;
      break;
  }
  return rule;
}

// inferAction picks the rule's primary action from its populated
// fields. If a rule has multiple actions (unusual but legal on S3),
// we prefer expire > transition > abort_mpu > noncurrent — the
// editor only round-trips ONE action per rule, so multi-action rules
// edited via this UI will be flattened on save (existing AWS-console
// users with such rules can keep editing those via the console).
function inferAction(rule: LifecycleRule): ActionKind {
  if (rule.expirationDays != null) return "expire";
  if (rule.transitionDays != null) return "transition";
  if (rule.abortMultipartDays != null) return "abort_mpu";
  if (rule.noncurrentDays != null) return "noncurrent";
  return "expire";
}

function daysForAction(rule: LifecycleRule, a: ActionKind): number | undefined {
  switch (a) {
    case "expire":
      return rule.expirationDays;
    case "transition":
      return rule.transitionDays;
    case "abort_mpu":
      return rule.abortMultipartDays;
    case "noncurrent":
      return rule.noncurrentDays;
  }
}
