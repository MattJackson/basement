import { useMemo, useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  useUserRegions,
  useUserRegionBuckets,
  useCreateFederation,
  type FederatedBucketCreateRequest,
  type FederationSyncMode,
} from "@/shared/api/queries";

// /files/federated-buckets/new — 5-step federation wizard
// (per ADR-0005 § Federation wizard).
//
// Steps:
//   1. Primary  — region + bucket dropdown
//   2. Replicas — up to 10 region+bucket rows
//   3. Policy   — sync mode + lag alert + write quorum + auto-failover
//   4. Review   — summary
//   5. Confirm  — POST and navigate to detail
//
// Same shape as the backup wizard (5-step linear with Back/Next), but
// the policy step is heavier because federation has more knobs that
// matter day-1 (write quorum, lag threshold) vs backup's "mode +
// retention". A duplicate (region, bucket) pair anywhere in the
// primary + replicas set is rejected client-side — the server enforces
// the same rule, but catching it locally lets the wizard surface a
// targeted error before the operator hits Save.
export const Route = createFileRoute("/files/federated-buckets/new")({
  component: NewFederationPage,
});

type Step = 1 | 2 | 3 | 4 | 5;

interface ReplicaRow {
  regionId: string;
  bucket: string;
}

const STEP_TITLES = ["Primary", "Replicas", "Policy", "Review", "Confirm"];

// Lag alert presets — operator-friendly labels mapped to the server's
// LagAlertSec int. "custom" is a bonus option that exposes the raw int
// field. Default 5 minutes matches federation.DefaultPolicy() server-side.
const LAG_PRESETS: { label: string; seconds: number }[] = [
  { label: "5 minutes (default)", seconds: 300 },
  { label: "15 minutes", seconds: 900 },
  { label: "1 hour", seconds: 3600 },
  { label: "6 hours", seconds: 21600 },
];

// Auto-failover timeout presets when the toggle is on.
const FAILOVER_TIMEOUTS: { label: string; seconds: number }[] = [
  { label: "30 seconds", seconds: 30 },
  { label: "60 seconds", seconds: 60 },
  { label: "5 minutes", seconds: 300 },
];

function NewFederationPage() {
  const navigate = useNavigate();
  const { data: regions = [] } = useUserRegions();
  const createMutation = useCreateFederation();

  const [step, setStep] = useState<Step>(1);

  // Name (entered on the Review step but lifted up so it persists
  // across step navigation — same as the backup wizard's `name` state).
  const [name, setName] = useState("");

  // Primary
  const [primaryRegion, setPrimaryRegion] = useState("");
  const [primaryBucket, setPrimaryBucket] = useState("");
  const { data: primaryBuckets } = useUserRegionBuckets(primaryRegion || null);

  // Replicas — start with one empty row so the wizard has a target to
  // fill on Step 2. "+ Add replica" appends; "Remove" deletes by index.
  const [replicas, setReplicas] = useState<ReplicaRow[]>([
    { regionId: "", bucket: "" },
  ]);

  // Policy
  const [syncMode, setSyncMode] = useState<FederationSyncMode>("continuous");
  const [schedule, setSchedule] = useState("0 * * * *"); // hourly default when scheduled
  const [lagAlertSec, setLagAlertSec] = useState<number>(300);
  const [lagCustom, setLagCustom] = useState<boolean>(false);
  const [writeQuorum, setWriteQuorum] = useState<number>(1);
  const [autoFailover, setAutoFailover] = useState<boolean>(false);
  const [autoFailoverSec, setAutoFailoverSec] = useState<number>(60);

  // v1.10.0.1 — per-step validation errors. Pre-fix the Next button
  // was simply `disabled` whenever the current step's fields weren't
  // satisfied; an operator clicking it on a pristine wizard got no
  // feedback at all (smoke flagged this as "blank submit silently does
  // nothing"). Now Next stays enabled, clicking with incomplete fields
  // surfaces inline `role="alert"` messages, and the wizard refuses to
  // advance until things are filled in.
  const [stepError, setStepError] = useState<string | null>(null);

  // Derived: the (region|bucket) keys already claimed by primary +
  // sibling replica rows. Used to disable already-picked options in the
  // per-replica dropdowns so the operator can't pick the same target
  // twice. The server enforces uniqueness server-side; this is purely
  // a guardrail for the wizard.
  const claimedTargets = useMemo(() => {
    const set = new Set<string>();
    if (primaryRegion && primaryBucket) {
      set.add(`${primaryRegion}|${primaryBucket}`);
    }
    return set;
  }, [primaryRegion, primaryBucket]);

  // Replica-list claim set — recomputed any time the replicas array
  // changes. Membership lookups go through this set rather than
  // .find() so an N×N scan on each render stays linear.
  const replicaClaims = useMemo(() => {
    const set = new Set<string>();
    for (const r of replicas) {
      if (r.regionId && r.bucket) {
        set.add(`${r.regionId}|${r.bucket}`);
      }
    }
    return set;
  }, [replicas]);

  const replicasValid = replicas.length > 0 && replicas.every(
    (r) => r.regionId && r.bucket,
  );
  const duplicateReplica = (() => {
    const seen = new Set<string>();
    if (primaryRegion && primaryBucket) {
      seen.add(`${primaryRegion}|${primaryBucket}`);
    }
    for (const r of replicas) {
      if (!r.regionId || !r.bucket) continue;
      const k = `${r.regionId}|${r.bucket}`;
      if (seen.has(k)) return k;
      seen.add(k);
    }
    return null;
  })();

  const canNextFromStep = (s: Step): boolean => {
    if (s === 1) return !!primaryRegion && !!primaryBucket;
    if (s === 2) return replicasValid && !duplicateReplica;
    if (s === 3) {
      if (syncMode === "scheduled" && !schedule.trim()) return false;
      if (writeQuorum < 1 || writeQuorum > 1 + replicas.length) return false;
      return true;
    }
    if (s === 4) return !!name.trim();
    return true;
  };

  // v1.10.0.1 — produce the inline message we show when Next is
  // clicked but the current step isn't satisfied. Lives alongside
  // `canNextFromStep` rather than inside it so the existing boolean
  // call sites (the disabled-on-click guard, the autofill check) stay
  // unchanged.
  const errorForStep = (s: Step): string | null => {
    if (s === 1) {
      if (!primaryRegion) return "Please pick a primary region.";
      if (!primaryBucket) return "Please pick a primary bucket.";
    }
    if (s === 2) {
      if (replicas.some((r) => !r.regionId || !r.bucket)) {
        return "Each replica row needs both a region and a bucket.";
      }
      if (duplicateReplica) {
        return "A (region, bucket) pair is duplicated. Each replica must be unique.";
      }
    }
    if (s === 3) {
      if (syncMode === "scheduled" && !schedule.trim()) {
        return "Scheduled mode requires a cron expression.";
      }
      if (writeQuorum < 1 || writeQuorum > 1 + replicas.length) {
        return `Write quorum must be between 1 and ${1 + replicas.length}.`;
      }
    }
    if (s === 4) {
      if (!name.trim()) return "Federation name is required.";
    }
    return null;
  };

  const handleNext = () => {
    const err = errorForStep(step);
    if (err) {
      setStepError(err);
      return;
    }
    setStepError(null);
    setStep((s) => (s + 1) as Step);
  };

  const handleBack = () => {
    setStepError(null);
    setStep((s) => (s - 1) as Step);
  };

  const addReplica = () => {
    if (replicas.length >= 10) return;
    setReplicas([...replicas, { regionId: "", bucket: "" }]);
  };

  const updateReplica = (idx: number, patch: Partial<ReplicaRow>) => {
    setReplicas((rs) =>
      rs.map((r, i) => (i === idx ? { ...r, ...patch } : r)),
    );
  };

  const removeReplica = (idx: number) => {
    setReplicas((rs) => rs.filter((_, i) => i !== idx));
  };

  const handleSubmit = () => {
    if (!name.trim()) {
      setStepError("Federation name is required (go back to Review to set it).");
      return;
    }
    setStepError(null);
    const body: FederatedBucketCreateRequest = {
      name: name.trim(),
      primary: { regionId: primaryRegion, bucket: primaryBucket },
      replicas: replicas.map((r) => ({ regionId: r.regionId, bucket: r.bucket })),
      policy: {
        syncMode,
        ...(syncMode === "scheduled" ? { schedule: schedule.trim() } : {}),
        lagAlertSec,
        writeQuorum,
        ...(autoFailover
          ? { autoFailover: true, autoFailoverSec }
          : {}),
      },
    };
    createMutation.mutate(body, {
      onSuccess: (fb) => {
        navigate({
          to: "/files/federated-buckets/$id",
          params: { id: fb.id },
        });
      },
    });
  };

  return (
    <div className="space-y-6 max-w-3xl">
      <Link
        to="/files/federated-buckets"
        className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
      >
        ← Back to federations
      </Link>

      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          New federation
        </h1>
        <p className="text-sm text-muted-foreground mt-1">
          Step {step} of 5 — {STEP_TITLES[step - 1]}
        </p>
      </header>

      <div className="flex items-center gap-2">
        {[1, 2, 3, 4, 5].map((n) => (
          <div
            key={n}
            className={`h-1.5 flex-1 rounded-full ${n <= step ? "bg-primary" : "bg-muted"}`}
          />
        ))}
      </div>

      {step === 1 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Primary</h2>
          <p className="text-xs text-muted-foreground">
            The bucket of record. All writes are confirmed here first; replicas
            are kept in lock-step continuously.
          </p>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="primaryRegion">Region *</Label>
              <select
                id="primaryRegion"
                value={primaryRegion}
                onChange={(e) => {
                  setPrimaryRegion(e.target.value);
                  setPrimaryBucket("");
                }}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm"
              >
                <option value="">— pick a region —</option>
                {regions.map((r) => (
                  <option key={r.id} value={r.id}>
                    {r.alias || r.endpoint}
                  </option>
                ))}
              </select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="primaryBucket">Bucket *</Label>
              <select
                id="primaryBucket"
                value={primaryBucket}
                disabled={!primaryRegion}
                onChange={(e) => setPrimaryBucket(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
              >
                <option value="">— pick a bucket —</option>
                {primaryBuckets?.buckets.map((b) => (
                  <option key={b.id} value={b.aliases?.[0] || b.id}>
                    {b.aliases?.[0] || b.id.slice(0, 12)}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </section>
      )}

      {step === 2 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <div className="flex items-center justify-between gap-2">
            <h2 className="text-sm font-medium text-muted-foreground">Replicas</h2>
            <Button
              variant="outline"
              size="sm"
              onClick={addReplica}
              disabled={replicas.length >= 10}
              data-testid="add-replica"
            >
              + Add replica
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">
            Each replica is a (region, bucket) pair. Up to 10 per federation.
            basement continuously mirrors writes from the primary into each
            replica. You can't pick a target that's already in the primary or
            another replica row.
          </p>
          <div className="space-y-3">
            {replicas.map((r, idx) => (
              <ReplicaRowEditor
                key={idx}
                idx={idx}
                row={r}
                onChange={(patch) => updateReplica(idx, patch)}
                onRemove={() => removeReplica(idx)}
                canRemove={replicas.length > 1}
                claimedTargets={claimedTargets}
                replicaClaims={replicaClaims}
              />
            ))}
          </div>
          {duplicateReplica && (
            <p
              className="text-xs text-destructive"
              data-testid="duplicate-replica-warning"
            >
              Duplicate target: a (region, bucket) pair appears more than once.
              Each replica must be unique.
            </p>
          )}
        </section>
      )}

      {step === 3 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Policy</h2>

          <div className="grid gap-3">
            <Label>Sync mode</Label>
            <RadioGroup
              value={syncMode}
              onValueChange={(v) => setSyncMode(v as FederationSyncMode)}
              className="grid gap-2"
            >
              <ModeOption
                value="continuous"
                current={syncMode}
                title="Continuous (recommended)"
                description="Engine ticks every 10s + reacts to webhook signals when supported. Replicas converge in seconds."
              />
              <ModeOption
                value="scheduled"
                current={syncMode}
                title="Scheduled (cron)"
                description="Replicate on a cron expression. Useful when continuous polling would be too expensive (e.g. metered egress)."
              />
            </RadioGroup>
            {syncMode === "scheduled" && (
              <div className="grid gap-2">
                <Label htmlFor="schedule">Cron expression *</Label>
                <Input
                  id="schedule"
                  value={schedule}
                  onChange={(e) => setSchedule(e.target.value)}
                  placeholder="0 * * * *"
                />
                <p className="text-xs text-muted-foreground">
                  Standard 5-field cron (minute hour dom month dow).
                </p>
              </div>
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="lagAlert">Lag alert</Label>
            <p className="text-xs text-muted-foreground">
              Replicas behind by more than this are flagged "lagging" on the
              detail page; 10× this threshold flips to "stale".
            </p>
            <select
              id="lagAlert"
              value={lagCustom ? "custom" : String(lagAlertSec)}
              onChange={(e) => {
                if (e.target.value === "custom") {
                  setLagCustom(true);
                } else {
                  setLagCustom(false);
                  setLagAlertSec(parseInt(e.target.value, 10));
                }
              }}
              className="w-full max-w-xs rounded-md border bg-background px-3 py-2 text-sm"
            >
              {LAG_PRESETS.map((p) => (
                <option key={p.seconds} value={p.seconds}>
                  {p.label}
                </option>
              ))}
              <option value="custom">Custom…</option>
            </select>
            {lagCustom && (
              <Input
                type="number"
                min={30}
                max={86400}
                value={lagAlertSec}
                onChange={(e) => {
                  const n = parseInt(e.target.value, 10);
                  setLagAlertSec(Number.isFinite(n) ? n : 300);
                }}
                className="max-w-xs"
                aria-label="Custom lag alert seconds"
              />
            )}
          </div>

          <div className="grid gap-2">
            <Label htmlFor="writeQuorum">Write quorum</Label>
            <p className="text-xs text-muted-foreground">
              Number of backends that must confirm before a write is considered
              durable. 1 = primary-only (fastest). {1 + replicas.length} = every
              replica must confirm (safest).
            </p>
            <select
              id="writeQuorum"
              value={writeQuorum}
              onChange={(e) => setWriteQuorum(parseInt(e.target.value, 10))}
              className="w-full max-w-xs rounded-md border bg-background px-3 py-2 text-sm"
            >
              {Array.from({ length: 1 + replicas.length }, (_, i) => i + 1).map(
                (n) => (
                  <option key={n} value={n}>
                    {n === 1
                      ? "1 — primary only (fastest)"
                      : n === 1 + replicas.length
                        ? `${n} — all replicas confirm (safest)`
                        : `${n}`}
                  </option>
                ),
              )}
            </select>
          </div>

          <div className="grid gap-2">
            <label className="flex items-center gap-2 cursor-pointer text-sm">
              <input
                type="checkbox"
                checked={autoFailover}
                onChange={(e) => setAutoFailover(e.target.checked)}
                data-testid="auto-failover-toggle"
                className="h-4 w-4"
              />
              <span className="font-medium">Auto-failover</span>
              <span className="text-xs text-muted-foreground">
                Promote a healthy replica when the primary is unreachable.
              </span>
            </label>
            {autoFailover && (
              <select
                value={autoFailoverSec}
                onChange={(e) =>
                  setAutoFailoverSec(parseInt(e.target.value, 10))
                }
                className="w-full max-w-xs rounded-md border bg-background px-3 py-2 text-sm"
                aria-label="Auto-failover timeout"
              >
                {FAILOVER_TIMEOUTS.map((p) => (
                  <option key={p.seconds} value={p.seconds}>
                    Promote after {p.label} of consecutive failure
                  </option>
                ))}
              </select>
            )}
          </div>
        </section>
      )}

      {step === 4 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Review</h2>
          <div className="grid gap-2">
            <Label htmlFor="name">Federation name *</Label>
            <Input
              id="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. photos"
            />
            <p className="text-xs text-muted-foreground">
              3-64 characters; letters, digits, dashes, underscores.
            </p>
          </div>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">Primary</dt>
            <dd className="font-mono">
              {primaryRegion} / {primaryBucket}
            </dd>
            <dt className="text-muted-foreground">Replicas</dt>
            <dd className="font-mono">
              {replicas.length} target{replicas.length === 1 ? "" : "s"}
              {replicas.map((r, i) => (
                <div key={i} className="text-xs">
                  • {r.regionId} / {r.bucket}
                </div>
              ))}
            </dd>
            <dt className="text-muted-foreground">Sync mode</dt>
            <dd className="font-mono">
              {syncMode}
              {syncMode === "scheduled" && ` (${schedule})`}
            </dd>
            <dt className="text-muted-foreground">Lag alert</dt>
            <dd className="font-mono">{lagAlertSec}s</dd>
            <dt className="text-muted-foreground">Write quorum</dt>
            <dd className="font-mono">
              {writeQuorum} of {1 + replicas.length}
            </dd>
            <dt className="text-muted-foreground">Auto-failover</dt>
            <dd className="font-mono">
              {autoFailover ? `on (${autoFailoverSec}s)` : "off"}
            </dd>
          </dl>
        </section>
      )}

      {step === 5 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Confirm</h2>
          <p className="text-sm">
            Creating "{name.trim()}" will start the replication engine
            immediately. The first tick walks the primary and queues every
            existing object for replicate-to-each-replica.
          </p>
          <p className="text-xs text-muted-foreground">
            Audit log captures{" "}
            <span className="font-mono">federation:create</span> plus a
            per-object{" "}
            <span className="font-mono">federation:replicate_object</span>{" "}
            entry — the latter is filtered out of the admin audit view by
            default to avoid drowning real events.
          </p>
          {createMutation.error && (
            <div
              className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
              data-testid="create-error"
            >
              {String((createMutation.error as Error).message || "Failed to create federation")}
            </div>
          )}
        </section>
      )}

      {/* v1.10.0.1 — inline step error surfaces when Next/Create is
          clicked but the current step isn't satisfied. Pre-fix the
          button was simply disabled, giving the operator nothing to
          act on. Now we tell them exactly what's missing. */}
      {stepError && (
        <div
          role="alert"
          data-testid="step-error"
          className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          {stepError}
        </div>
      )}

      <div className="flex items-center justify-between gap-2">
        <Button
          variant="outline"
          onClick={() => navigate({ to: "/files/federated-buckets" })}
        >
          Cancel
        </Button>
        <div className="flex items-center gap-2">
          {step > 1 && (
            <Button variant="ghost" onClick={handleBack}>
              Back
            </Button>
          )}
          {step < 5 && (
            <Button
              onClick={handleNext}
              aria-invalid={!canNextFromStep(step) ? true : undefined}
            >
              Next
            </Button>
          )}
          {step === 5 && (
            <Button
              onClick={handleSubmit}
              disabled={createMutation.isPending}
              data-testid="create-submit"
            >
              {createMutation.isPending ? "Creating…" : "Create"}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

function ModeOption({
  value,
  current,
  title,
  description,
}: {
  value: FederationSyncMode;
  current: FederationSyncMode;
  title: string;
  description: string;
}) {
  return (
    <label
      className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${current === value ? "border-ring" : ""}`}
    >
      <RadioGroupItem value={value} className="mt-1" />
      <div>
        <div className="font-medium text-sm">{title}</div>
        <p className="text-xs text-muted-foreground mt-0.5">{description}</p>
      </div>
    </label>
  );
}

// ReplicaRowEditor renders one (region, bucket) replica row. Buckets
// are loaded lazily via useUserRegionBuckets(regionId) — same hook the
// backup wizard uses; the per-region bucket list is short-staleTime
// cached so adding several replicas against the same region only
// fetches once.
function ReplicaRowEditor({
  idx,
  row,
  onChange,
  onRemove,
  canRemove,
  claimedTargets,
  replicaClaims,
}: {
  idx: number;
  row: ReplicaRow;
  onChange: (patch: Partial<ReplicaRow>) => void;
  onRemove: () => void;
  canRemove: boolean;
  claimedTargets: Set<string>;
  replicaClaims: Set<string>;
}) {
  const { data: regionsList = [] } = useUserRegions();
  const { data: bucketsList } = useUserRegionBuckets(row.regionId || null);

  // Build the disabled-key set: union of (primary) + (sibling replica)
  // claims. The current row's own selection is whitelisted so the
  // selected option doesn't appear disabled-to-itself.
  const ownKey = row.regionId && row.bucket
    ? `${row.regionId}|${row.bucket}`
    : "";

  const isOptionTaken = (regionId: string, bucket: string): boolean => {
    const k = `${regionId}|${bucket}`;
    if (k === ownKey) return false;
    return claimedTargets.has(k) || replicaClaims.has(k);
  };

  return (
    <div
      className="grid gap-3 sm:grid-cols-[1fr_1fr_auto] items-end rounded-md border p-3"
      data-testid={`replica-row-${idx}`}
    >
      <div className="grid gap-1">
        <Label htmlFor={`replica-region-${idx}`}>Region</Label>
        <select
          id={`replica-region-${idx}`}
          value={row.regionId}
          onChange={(e) => onChange({ regionId: e.target.value, bucket: "" })}
          className="w-full rounded-md border bg-background px-3 py-2 text-sm"
        >
          <option value="">— pick a region —</option>
          {regionsList.map((r) => (
            <option key={r.id} value={r.id}>
              {r.alias || r.endpoint}
            </option>
          ))}
        </select>
      </div>
      <div className="grid gap-1">
        <Label htmlFor={`replica-bucket-${idx}`}>Bucket</Label>
        <select
          id={`replica-bucket-${idx}`}
          value={row.bucket}
          disabled={!row.regionId}
          onChange={(e) => onChange({ bucket: e.target.value })}
          className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
        >
          <option value="">— pick a bucket —</option>
          {bucketsList?.buckets.map((b) => {
            const value = b.aliases?.[0] || b.id;
            const taken = isOptionTaken(row.regionId, value);
            return (
              <option key={b.id} value={value} disabled={taken}>
                {b.aliases?.[0] || b.id.slice(0, 12)}
                {taken ? " (already in use)" : ""}
              </option>
            );
          })}
        </select>
      </div>
      <div>
        <Button
          variant="ghost"
          size="sm"
          onClick={onRemove}
          disabled={!canRemove}
          data-testid={`remove-replica-${idx}`}
        >
          Remove
        </Button>
      </div>
    </div>
  );
}
