// MIGRATE.WIZARD v0.9.0l — /admin/migrate cluster-to-cluster bulk
// migration wizard.
//
// "Move this cluster's data to that cluster" — a higher-level
// workflow on top of the existing single-bucket sync engine. NO new
// sync-engine work, NO bulk backend endpoint (deferred per scope
// decision): the wizard fans out from the frontend, calling the
// existing `POST /user/syncs` once per selected bucket. Cleaner blast
// radius — a single bucket's failure is isolated to its row and
// surfaces inline; the other migrations keep going. On success the
// operator lands on `/files/syncs` where the jobs already live (links
// rather than rebuilding the progress view).
//
// Three-step single-page wizard: Step 1 picks src + dst clusters
// (gated by `srcCid !== dstCid`), Step 2 discovers buckets in the
// source and lets the operator deselect (default: all checked) +
// rename the destination bucket alias per row, Step 3 reviews the
// plan and fires the parallel `useCreateUserSync.mutateAsync` fan-out.
//
// Deep links: `?srcCid=...&dstCid=...` pre-fills Step 1, so the
// cluster detail page and the usage overview can drop the operator
// into the wizard already partly filled.

import { useEffect, useMemo, useState } from "react";
import { createFileRoute, Link, useNavigate, useSearch } from "@tanstack/react-router";
import { toast } from "sonner";

import { adminPage } from "@/shared/layout/adminPage";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Skeleton } from "@/components/ui/skeleton";
import { humanizeBytes } from "@/shared/lib/format";
import {
  useUserClusters,
  useUserClusterBuckets,
  useUserBucket,
  useCreateUserSync,
} from "@/shared/api/queries";

type MigrateSearch = {
  srcCid?: string;
  dstCid?: string;
};

export const Route = createFileRoute("/admin/migrate")({
  component: adminPage(MigrateWizardPage),
  validateSearch: (search): MigrateSearch => ({
    srcCid: typeof search.srcCid === "string" ? search.srcCid : undefined,
    dstCid: typeof search.dstCid === "string" ? search.dstCid : undefined,
  }),
});

type BucketPlan = {
  srcBucketId: string;
  srcAlias: string;
  dstAlias: string;
  selected: boolean;
  status: "pending" | "creating" | "ok" | "error";
  jobId?: string;
  errorMessage?: string;
};

function MigrateWizardPage() {
  const navigate = useNavigate();
  const search = useSearch({ from: "/admin/migrate" }) as MigrateSearch;

  const [step, setStep] = useState<1 | 2 | 3>(1);
  const [srcCid, setSrcCid] = useState(search.srcCid ?? "");
  const [dstCid, setDstCid] = useState(search.dstCid ?? "");
  const [plans, setPlans] = useState<BucketPlan[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [submitDone, setSubmitDone] = useState(false);

  const { data: clusters } = useUserClusters();
  const { data: srcBuckets, isLoading: srcBucketsLoading } =
    useUserClusterBuckets(srcCid);
  const createSync = useCreateUserSync();

  const srcCluster = clusters?.find((c) => c.id === srcCid);
  const dstCluster = clusters?.find((c) => c.id === dstCid);

  // Once srcBuckets resolves on Step 2 entry, seed the plan rows. We
  // key off srcCid so flipping the source resets selection state.
  useEffect(() => {
    if (!srcBuckets || step !== 2) return;
    setPlans((prev) => {
      // If the existing plan already matches this set of bucket IDs,
      // don't clobber the operator's edits (alias rename, deselect).
      const prevIds = new Set(prev.map((p) => p.srcBucketId));
      const nextIds = new Set(srcBuckets.map((b) => b.id));
      const sameSet =
        prev.length === srcBuckets.length &&
        [...prevIds].every((id) => nextIds.has(id));
      if (sameSet) return prev;
      return srcBuckets.map((b) => {
        const alias = b.aliases?.[0] ?? b.id.slice(0, 12);
        return {
          srcBucketId: b.id,
          srcAlias: alias,
          dstAlias: alias,
          selected: true,
          status: "pending" as const,
        };
      });
    });
  }, [srcBuckets, step]);

  const sameCluster = !!srcCid && !!dstCid && srcCid === dstCid;
  const canAdvanceStep1 = !!srcCid && !!dstCid && !sameCluster;
  const selectedPlans = plans.filter((p) => p.selected);
  const canAdvanceStep2 =
    selectedPlans.length > 0 && selectedPlans.every((p) => p.dstAlias.trim().length > 0);

  const handleNextFromStep1 = () => {
    if (!canAdvanceStep1) return;
    setPlans([]); // force re-seed on bucket list arrival
    setStep(2);
  };

  const handleNextFromStep2 = () => {
    if (!canAdvanceStep2) return;
    setStep(3);
  };

  const handleStart = async () => {
    if (!srcCid || !dstCid || selectedPlans.length === 0) return;
    setSubmitting(true);

    // Mark every selected row 'creating' up front so the UI reflects
    // the in-flight state without a per-row state plumb.
    setPlans((prev) =>
      prev.map((p) => (p.selected ? { ...p, status: "creating" } : p)),
    );

    // Fan out in parallel — each mutateAsync is independent. We
    // collect per-row outcomes and patch them onto the plan as the
    // calls settle. A failure on one row never short-circuits the
    // others (Promise.allSettled, not Promise.all).
    const results = await Promise.allSettled(
      selectedPlans.map((p) =>
        createSync.mutateAsync({
          mode: "pull",
          srcConnectionId: srcCid,
          srcBucket: p.srcAlias,
          dstConnectionId: dstCid,
          dstBucket: p.dstAlias.trim(),
        }),
      ),
    );

    let okCount = 0;
    let errCount = 0;
    setPlans((prev) => {
      const next = [...prev];
      let resIdx = 0;
      for (let i = 0; i < next.length; i++) {
        const row = next[i];
        if (!row || !row.selected) continue;
        const r = results[resIdx++];
        if (!r) continue;
        if (r.status === "fulfilled") {
          okCount++;
          next[i] = { ...row, status: "ok", jobId: r.value.id };
        } else {
          errCount++;
          const msg =
            r.reason instanceof Error
              ? r.reason.message
              : String(r.reason ?? "unknown error");
          next[i] = { ...row, status: "error", errorMessage: msg };
        }
      }
      return next;
    });

    setSubmitting(false);
    setSubmitDone(true);

    if (errCount === 0) {
      toast.success(
        `${okCount} migration sync job${okCount === 1 ? "" : "s"} started`,
      );
      navigate({ to: "/files/syncs" });
    } else if (okCount === 0) {
      toast.error(
        `All ${errCount} migration job${errCount === 1 ? "" : "s"} failed — see details below`,
      );
    } else {
      toast.error(
        `${okCount} job${okCount === 1 ? "" : "s"} started, ${errCount} failed — see details below`,
      );
    }
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <Link
        to="/admin/usage"
        className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground"
      >
        &larr; Back to usage
      </Link>

      <header className="space-y-1">
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">
          Migrate cluster
        </h1>
        <p className="text-sm text-muted-foreground max-w-3xl">
          Copy every bucket from one cluster to another in one click. Each
          bucket becomes its own sync job &mdash; you can monitor and
          pause/resume them from the{" "}
          <Link to="/files/syncs" className="text-primary hover:underline">
            syncs
          </Link>{" "}
          page.
        </p>
      </header>

      <StepIndicator current={step} />

      {step === 1 && (
        <Step1Pick
          clusters={clusters ?? []}
          srcCid={srcCid}
          dstCid={dstCid}
          sameCluster={sameCluster}
          onSrcChange={(v) => setSrcCid(v)}
          onDstChange={(v) => setDstCid(v)}
          onNext={handleNextFromStep1}
          canAdvance={canAdvanceStep1}
        />
      )}

      {step === 2 && (
        <Step2Select
          srcLabel={srcCluster?.label ?? srcCid.slice(0, 8)}
          dstLabel={dstCluster?.label ?? dstCid.slice(0, 8)}
          srcCid={srcCid}
          plans={plans}
          loading={srcBucketsLoading || !srcBuckets}
          onToggle={(bid) =>
            setPlans((prev) =>
              prev.map((p) =>
                p.srcBucketId === bid ? { ...p, selected: !p.selected } : p,
              ),
            )
          }
          onSelectAll={(checked) =>
            setPlans((prev) => prev.map((p) => ({ ...p, selected: checked })))
          }
          onRenameDst={(bid, dst) =>
            setPlans((prev) =>
              prev.map((p) =>
                p.srcBucketId === bid ? { ...p, dstAlias: dst } : p,
              ),
            )
          }
          onBack={() => setStep(1)}
          onNext={handleNextFromStep2}
          canAdvance={canAdvanceStep2}
        />
      )}

      {step === 3 && (
        <Step3Review
          srcLabel={srcCluster?.label ?? srcCid.slice(0, 8)}
          dstLabel={dstCluster?.label ?? dstCid.slice(0, 8)}
          plans={selectedPlans}
          submitting={submitting}
          submitDone={submitDone}
          onBack={() => {
            // Don't let operator slip back into edit while in-flight.
            if (submitting) return;
            setStep(2);
            setSubmitDone(false);
            setPlans((prev) =>
              prev.map((p) => ({ ...p, status: "pending", errorMessage: undefined, jobId: undefined })),
            );
          }}
          onStart={handleStart}
          onViewSyncs={() => navigate({ to: "/files/syncs" })}
        />
      )}
    </div>
  );
}

function StepIndicator({ current }: { current: 1 | 2 | 3 }) {
  const steps = [
    { n: 1, label: "Pick clusters" },
    { n: 2, label: "Select buckets" },
    { n: 3, label: "Review & go" },
  ] as const;
  return (
    <ol className="flex items-center gap-2 text-sm" aria-label="Wizard progress">
      {steps.map((s, i) => {
        const active = s.n === current;
        const done = s.n < current;
        return (
          <li key={s.n} className="flex items-center gap-2">
            <span
              className={`inline-flex h-6 w-6 items-center justify-center rounded-full text-xs font-medium tabular-nums ${
                active
                  ? "bg-primary text-primary-foreground"
                  : done
                    ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400"
                    : "bg-muted text-muted-foreground"
              }`}
              aria-current={active ? "step" : undefined}
            >
              {done ? "✓" : s.n}
            </span>
            <span className={active ? "font-medium" : "text-muted-foreground"}>
              {s.label}
            </span>
            {i < steps.length - 1 && (
              <span className="text-muted-foreground/40 mx-1">&rarr;</span>
            )}
          </li>
        );
      })}
    </ol>
  );
}

function Step1Pick({
  clusters,
  srcCid,
  dstCid,
  sameCluster,
  onSrcChange,
  onDstChange,
  onNext,
  canAdvance,
}: {
  clusters: { id: string; label: string }[];
  srcCid: string;
  dstCid: string;
  sameCluster: boolean;
  onSrcChange: (v: string) => void;
  onDstChange: (v: string) => void;
  onNext: () => void;
  canAdvance: boolean;
}) {
  return (
    <section className="space-y-4 rounded-lg border bg-card p-6">
      <h2 className="text-sm font-medium text-muted-foreground">
        Step 1 &mdash; Pick source and destination clusters
      </h2>
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="grid gap-2">
          <Label htmlFor="srcCluster">Source cluster *</Label>
          <select
            id="srcCluster"
            value={srcCid}
            onChange={(e) => onSrcChange(e.target.value)}
            className="w-full rounded-md border bg-background px-3 py-2 text-sm"
          >
            <option value="">&mdash; pick a source &mdash;</option>
            {clusters.map((c) => (
              <option key={c.id} value={c.id}>
                {c.label}
              </option>
            ))}
          </select>
          <p className="text-xs text-muted-foreground">
            Buckets will be copied <em>from</em> this cluster.
          </p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="dstCluster">Destination cluster *</Label>
          <select
            id="dstCluster"
            value={dstCid}
            onChange={(e) => onDstChange(e.target.value)}
            className="w-full rounded-md border bg-background px-3 py-2 text-sm"
          >
            <option value="">&mdash; pick a destination &mdash;</option>
            {clusters.map((c) => (
              <option key={c.id} value={c.id} disabled={c.id === srcCid}>
                {c.label}
                {c.id === srcCid ? " (same as source)" : ""}
              </option>
            ))}
          </select>
          <p className="text-xs text-muted-foreground">
            Buckets will be copied <em>to</em> this cluster.
          </p>
        </div>
      </div>

      {sameCluster && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Source and destination must be different clusters.
        </div>
      )}

      <div className="flex justify-end pt-2">
        <Button onClick={onNext} disabled={!canAdvance}>
          Next &rarr;
        </Button>
      </div>
    </section>
  );
}

function Step2Select({
  srcLabel,
  dstLabel,
  srcCid,
  plans,
  loading,
  onToggle,
  onSelectAll,
  onRenameDst,
  onBack,
  onNext,
  canAdvance,
}: {
  srcLabel: string;
  dstLabel: string;
  srcCid: string;
  plans: BucketPlan[];
  loading: boolean;
  onToggle: (bid: string) => void;
  onSelectAll: (checked: boolean) => void;
  onRenameDst: (bid: string, dst: string) => void;
  onBack: () => void;
  onNext: () => void;
  canAdvance: boolean;
}) {
  const allSelected = plans.length > 0 && plans.every((p) => p.selected);
  const someSelected = plans.some((p) => p.selected);

  return (
    <section className="space-y-4 rounded-lg border bg-card p-6">
      <div className="flex items-baseline justify-between">
        <h2 className="text-sm font-medium text-muted-foreground">
          Step 2 &mdash; Select buckets to migrate
        </h2>
        <p className="text-xs text-muted-foreground">
          From <span className="font-medium text-foreground">{srcLabel}</span>{" "}
          &rarr;{" "}
          <span className="font-medium text-foreground">{dstLabel}</span>
        </p>
      </div>

      {loading ? (
        <Skeleton className="h-32 w-full rounded-md" />
      ) : plans.length === 0 ? (
        <div className="rounded-md border bg-muted/30 p-6 text-sm text-muted-foreground text-center">
          No buckets discovered in the source cluster &mdash; nothing to
          migrate.
        </div>
      ) : (
        <div className="rounded-md border overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[40px]">
                  <Checkbox
                    checked={allSelected}
                    onCheckedChange={(c) => onSelectAll(c === true)}
                    aria-label="Select all buckets"
                  />
                </TableHead>
                <TableHead>Source bucket</TableHead>
                <TableHead className="text-right w-[120px]">Size</TableHead>
                <TableHead className="text-right w-[100px]">Objects</TableHead>
                <TableHead className="w-[260px]">Destination alias</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {plans.map((p) => (
                <PlanRow
                  key={p.srcBucketId}
                  srcCid={srcCid}
                  plan={p}
                  onToggle={() => onToggle(p.srcBucketId)}
                  onRenameDst={(v) => onRenameDst(p.srcBucketId, v)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {plans.length > 0 && !someSelected && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          Select at least one bucket to migrate.
        </div>
      )}

      <div className="flex justify-between pt-2">
        <Button variant="outline" onClick={onBack}>
          &larr; Back
        </Button>
        <Button onClick={onNext} disabled={!canAdvance}>
          Next &rarr;
        </Button>
      </div>
    </section>
  );
}

/** Row fires its own useUserBucket() for size + object count, matching
 *  the cluster-detail pattern. The cluster-scoped bucket list returns
 *  just id + aliases on Garage v1 — admin-grade rows need bytes too. */
function PlanRow({
  srcCid,
  plan,
  onToggle,
  onRenameDst,
}: {
  srcCid: string;
  plan: BucketPlan;
  onToggle: () => void;
  onRenameDst: (v: string) => void;
}) {
  const { data: detail } = useUserBucket(srcCid, plan.srcBucketId);
  return (
    <TableRow className={plan.selected ? "" : "opacity-50"}>
      <TableCell>
        <Checkbox
          checked={plan.selected}
          onCheckedChange={onToggle}
          aria-label={`Select ${plan.srcAlias}`}
        />
      </TableCell>
      <TableCell className="font-medium">{plan.srcAlias}</TableCell>
      <TableCell className="text-right tabular-nums">
        {detail ? humanizeBytes(detail.bytes ?? 0) : <Skeleton className="h-3 w-12 ml-auto" />}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {detail ? (detail.objects ?? 0).toLocaleString() : <Skeleton className="h-3 w-8 ml-auto" />}
      </TableCell>
      <TableCell>
        <Input
          value={plan.dstAlias}
          onChange={(e) => onRenameDst(e.target.value)}
          disabled={!plan.selected}
          placeholder={plan.srcAlias}
          aria-label={`Destination alias for ${plan.srcAlias}`}
        />
      </TableCell>
    </TableRow>
  );
}

function Step3Review({
  srcLabel,
  dstLabel,
  plans,
  submitting,
  submitDone,
  onBack,
  onStart,
  onViewSyncs,
}: {
  srcLabel: string;
  dstLabel: string;
  plans: BucketPlan[];
  submitting: boolean;
  submitDone: boolean;
  onBack: () => void;
  onStart: () => void;
  onViewSyncs: () => void;
}) {
  const okCount = useMemo(() => plans.filter((p) => p.status === "ok").length, [plans]);
  const errCount = useMemo(() => plans.filter((p) => p.status === "error").length, [plans]);

  return (
    <section className="space-y-4 rounded-lg border bg-card p-6">
      <div className="flex items-baseline justify-between">
        <h2 className="text-sm font-medium text-muted-foreground">
          Step 3 &mdash; Review and start
        </h2>
        <p className="text-xs text-muted-foreground">
          Migrating {plans.length} bucket{plans.length === 1 ? "" : "s"} from{" "}
          <span className="font-medium text-foreground">{srcLabel}</span>{" "}
          &rarr;{" "}
          <span className="font-medium text-foreground">{dstLabel}</span>
        </p>
      </div>

      <div className="rounded-md border overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Source bucket</TableHead>
              <TableHead>Destination bucket</TableHead>
              <TableHead className="w-[200px]">Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {plans.map((p) => (
              <TableRow key={p.srcBucketId}>
                <TableCell className="font-medium">{p.srcAlias}</TableCell>
                <TableCell>{p.dstAlias.trim() || p.srcAlias}</TableCell>
                <TableCell>
                  <StatusBadge plan={p} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {submitDone && errCount > 0 && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {errCount} bucket{errCount === 1 ? "" : "s"} failed to start. Review
          the inline messages, then either retry by going back to Step 2 or
          continue to the syncs page to monitor the {okCount} job
          {okCount === 1 ? "" : "s"} that did start.
        </div>
      )}

      <div className="flex justify-between pt-2">
        <Button variant="outline" onClick={onBack} disabled={submitting}>
          &larr; Back
        </Button>
        {submitDone ? (
          <Button onClick={onViewSyncs}>View syncs &rarr;</Button>
        ) : (
          <Button onClick={onStart} disabled={submitting || plans.length === 0}>
            {submitting
              ? `Starting ${plans.length} job${plans.length === 1 ? "" : "s"}…`
              : `Start migration (${plans.length} job${plans.length === 1 ? "" : "s"})`}
          </Button>
        )}
      </div>
    </section>
  );
}

function StatusBadge({ plan }: { plan: BucketPlan }) {
  switch (plan.status) {
    case "pending":
      return <span className="text-xs text-muted-foreground">Ready</span>;
    case "creating":
      return (
        <span className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs font-medium">
          Starting…
        </span>
      );
    case "ok":
      return (
        <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:text-emerald-400">
          Started {plan.jobId ? `(${plan.jobId.slice(0, 8)})` : ""}
        </span>
      );
    case "error":
      return (
        <span
          className="inline-flex items-center rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive"
          title={plan.errorMessage}
        >
          Failed: {plan.errorMessage ? truncate(plan.errorMessage, 40) : "unknown"}
        </span>
      );
  }
}

function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return `${s.slice(0, max - 1)}…`;
}
