import { useMemo, useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
  useUserRegions,
  useUserRegionBuckets,
  useCreateUserBackup,
  type UserBackupCreateRequest,
  type BackupMode,
} from "@/shared/api/queries";

// /files/backups/new — multi-step backup wizard.
//
// Step semantics (v1.5.0b adds "Mode" — the wizard is now 5 steps):
//   1. Source             — region + bucket dropdown (+ optional prefix)
//   2. Destination        — region + bucket dropdown (+ optional prefix; hidden in snapshot mode)
//   3. Mode + retention   — mirror (overwrite) vs snapshot (timestamped history) + GFS keep counts
//   4. Schedule           — radio: manual / daily / weekly / monthly / custom cron
//   5. Name + review      — operator-friendly name + summary, then save
//
// Steps are linear with Back/Next; we don't try to be clever with
// step-skipping because the form is short. Each step validates only
// the fields it owns so a half-filled form can't accidentally
// submit. The final save happens on the review step.
export const Route = createFileRoute("/files/backups/new")({
  component: NewBackupPage,
});

type Step = 1 | 2 | 3 | 4 | 5;
type ScheduleKind = "manual" | "daily" | "weekly" | "monthly" | "custom";

const WEEKDAYS = [
  { value: "1", label: "Monday" },
  { value: "2", label: "Tuesday" },
  { value: "3", label: "Wednesday" },
  { value: "4", label: "Thursday" },
  { value: "5", label: "Friday" },
  { value: "6", label: "Saturday" },
  { value: "0", label: "Sunday" },
];

// Default GFS retention — matches the server's DefaultRetention()
// so the wizard preview matches what the runner will actually apply
// if the operator submits the form without changing the values.
const DEFAULT_RETENTION = { keepDaily: 7, keepWeekly: 4, keepMonthly: 12 };

function NewBackupPage() {
  const navigate = useNavigate();
  const { data: regions } = useUserRegions();
  const createBackup = useCreateUserBackup();

  const [step, setStep] = useState<Step>(1);

  // Source
  const [srcRegionId, setSrcRegionId] = useState("");
  const [srcBucket, setSrcBucket] = useState("");
  const [srcPrefix, setSrcPrefix] = useState("");

  // Destination
  const [dstRegionId, setDstRegionId] = useState("");
  const [dstBucket, setDstBucket] = useState("");
  const [dstPrefix, setDstPrefix] = useState("");

  // Mode + retention (v1.5.0b)
  const [mode, setMode] = useState<BackupMode>("mirror");
  const [keepDaily, setKeepDaily] = useState<number>(DEFAULT_RETENTION.keepDaily);
  const [keepWeekly, setKeepWeekly] = useState<number>(DEFAULT_RETENTION.keepWeekly);
  const [keepMonthly, setKeepMonthly] = useState<number>(DEFAULT_RETENTION.keepMonthly);

  // Schedule
  const [scheduleKind, setScheduleKind] = useState<ScheduleKind>("manual");
  const [scheduleTime, setScheduleTime] = useState("03:00"); // HH:MM
  const [scheduleWeekday, setScheduleWeekday] = useState("1"); // Monday
  const [scheduleDayOfMonth, setScheduleDayOfMonth] = useState("1");
  const [customCron, setCustomCron] = useState("0 3 * * *");

  // Name + review
  const [name, setName] = useState("");

  const { data: srcBuckets } = useUserRegionBuckets(srcRegionId || null);
  const { data: dstBuckets } = useUserRegionBuckets(dstRegionId || null);

  const schedule = useMemo(
    () =>
      buildSchedule({
        kind: scheduleKind,
        time: scheduleTime,
        weekday: scheduleWeekday,
        dayOfMonth: scheduleDayOfMonth,
        custom: customCron,
      }),
    [scheduleKind, scheduleTime, scheduleWeekday, scheduleDayOfMonth, customCron],
  );

  const canNextFromStep = (s: Step): boolean => {
    if (s === 1) return !!srcRegionId && !!srcBucket;
    if (s === 2) return !!dstRegionId && !!dstBucket;
    if (s === 3) {
      // Snapshot mode requires at least one non-zero keep-count
      // so the prune phase doesn't immediately wipe the snapshot.
      if (mode === "snapshot") {
        return keepDaily + keepWeekly + keepMonthly > 0;
      }
      return true;
    }
    if (s === 4) return !!schedule;
    return true;
  };

  const handleSubmit = () => {
    if (!name.trim() || !schedule) return;
    const body: UserBackupCreateRequest = {
      name: name.trim(),
      srcRegionId,
      srcBucket,
      srcPrefix: srcPrefix || undefined,
      dstRegionId,
      dstBucket,
      // dstPrefix is ignored by the runner in snapshot mode, so we
      // omit it from the wire body when snapshot is selected — keeps
      // the persisted record clean.
      dstPrefix: mode === "snapshot" ? undefined : dstPrefix || undefined,
      schedule,
      mode,
      retention:
        mode === "snapshot"
          ? { keepDaily, keepWeekly, keepMonthly }
          : undefined,
    };
    createBackup.mutate(body, {
      onSuccess: () => navigate({ to: "/files/backups" }),
    });
  };

  return (
    <div className="space-y-6 max-w-3xl">
      <Link to="/files/backups" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        ← Back to backups
      </Link>

      <header>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">New backup</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Step {step} of 5 — {STEP_TITLES[step - 1]}
        </p>
      </header>

      {/* Progress bar */}
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
          <h2 className="text-sm font-medium text-muted-foreground">Source</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="srcRegion">Region *</Label>
              <select
                id="srcRegion"
                value={srcRegionId}
                onChange={(e) => { setSrcRegionId(e.target.value); setSrcBucket(""); }}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm"
              >
                <option value="">— pick a region —</option>
                {regions?.map((r) => (
                  <option key={r.id} value={r.id}>{r.alias || r.endpoint}</option>
                ))}
              </select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="srcBucket">Bucket *</Label>
              <select
                id="srcBucket"
                value={srcBucket}
                disabled={!srcRegionId}
                onChange={(e) => setSrcBucket(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
              >
                <option value="">— pick a bucket —</option>
                {srcBuckets?.buckets.map((b) => (
                  <option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0, 12)}</option>
                ))}
              </select>
            </div>
            <div className="grid gap-2 sm:col-span-2">
              <Label htmlFor="srcPrefix">Prefix (optional)</Label>
              <Input id="srcPrefix" value={srcPrefix} onChange={(e) => setSrcPrefix(e.target.value)} placeholder="e.g. photos/" />
            </div>
          </div>
        </section>
      )}

      {step === 2 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Destination</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="dstRegion">Region *</Label>
              <select
                id="dstRegion"
                value={dstRegionId}
                onChange={(e) => { setDstRegionId(e.target.value); setDstBucket(""); }}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm"
              >
                <option value="">— pick a region —</option>
                {regions?.map((r) => (
                  <option key={r.id} value={r.id}>{r.alias || r.endpoint}</option>
                ))}
              </select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="dstBucket">Bucket *</Label>
              <select
                id="dstBucket"
                value={dstBucket}
                disabled={!dstRegionId}
                onChange={(e) => setDstBucket(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
              >
                <option value="">— pick a bucket —</option>
                {dstBuckets?.buckets.map((b) => (
                  <option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0, 12)}</option>
                ))}
              </select>
            </div>
            <div className="grid gap-2 sm:col-span-2">
              <Label htmlFor="dstPrefix">Prefix (optional)</Label>
              <Input id="dstPrefix" value={dstPrefix} onChange={(e) => setDstPrefix(e.target.value)} placeholder="e.g. backups/" />
              <p className="text-xs text-muted-foreground">
                Snapshot-mode backups ignore this prefix and own the destination layout themselves
                ({"{bucket}/{name}/{timestamp}/"}).
              </p>
            </div>
          </div>
        </section>
      )}

      {step === 3 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Mode &amp; retention</h2>
          <RadioGroup
            value={mode}
            onValueChange={(v) => setMode(v as BackupMode)}
            className="grid gap-2"
          >
            <ModeOption
              value="mirror"
              current={mode}
              title="Mirror (overwrite destination)"
              description="Each run overwrites the destination prefix. Use when you only ever care about the latest copy — simplest, no point-in-time history."
            />
            <ModeOption
              value="snapshot"
              current={mode}
              title="Snapshot (timestamped history)"
              description={
                "Each run writes to {dstBucket}/{name}/{timestamp}/. Old snapshots are pruned automatically by the retention policy below."
              }
            />
          </RadioGroup>

          {mode === "snapshot" && (
            <div className="rounded-md border bg-muted/20 p-4 space-y-4">
              <h3 className="text-sm font-medium">Retention policy</h3>
              <p className="text-xs text-muted-foreground">
                Grandfather-Father-Son rotation. The keep set is the union of the three buckets — a single
                snapshot can count toward all three at once. Default 7/4/12 covers about 14 months with 23
                snapshots at steady state.
              </p>
              <div className="grid gap-4 sm:grid-cols-3">
                <RetentionField id="keepDaily" label="Keep daily" value={keepDaily} onChange={setKeepDaily} hint="Last N days, latest per day." />
                <RetentionField id="keepWeekly" label="Keep weekly" value={keepWeekly} onChange={setKeepWeekly} hint="Last N ISO-weeks." />
                <RetentionField id="keepMonthly" label="Keep monthly" value={keepMonthly} onChange={setKeepMonthly} hint="Last N calendar months." />
              </div>
              {keepDaily + keepWeekly + keepMonthly === 0 && (
                <p className="text-xs text-destructive">
                  All keep counts are zero — every snapshot would be pruned immediately after each run.
                </p>
              )}
            </div>
          )}
        </section>
      )}

      {step === 4 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Schedule</h2>
          <RadioGroup
            value={scheduleKind}
            onValueChange={(v) => setScheduleKind(v as ScheduleKind)}
            className="grid gap-2"
          >
            <ScheduleOption value="manual" current={scheduleKind} title="Manual only" description="Runs only when you click 'Run now' on the list." />
            <ScheduleOption value="daily" current={scheduleKind} title="Daily" description="Every day at the time below." />
            <ScheduleOption value="weekly" current={scheduleKind} title="Weekly" description="Every week on the chosen day + time." />
            <ScheduleOption value="monthly" current={scheduleKind} title="Monthly" description="Every month on the chosen day-of-month + time." />
            <ScheduleOption value="custom" current={scheduleKind} title="Custom cron" description="5-field cron expression (minute hour dom month dow)." />
          </RadioGroup>

          {(scheduleKind === "daily" || scheduleKind === "weekly" || scheduleKind === "monthly") && (
            <div className="grid gap-2 max-w-xs">
              <Label htmlFor="scheduleTime">Time (HH:MM, 24h)</Label>
              <Input id="scheduleTime" type="time" value={scheduleTime} onChange={(e) => setScheduleTime(e.target.value)} />
            </div>
          )}
          {scheduleKind === "weekly" && (
            <div className="grid gap-2 max-w-xs">
              <Label htmlFor="scheduleWeekday">Day of week</Label>
              <select
                id="scheduleWeekday"
                value={scheduleWeekday}
                onChange={(e) => setScheduleWeekday(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-sm"
              >
                {WEEKDAYS.map((d) => <option key={d.value} value={d.value}>{d.label}</option>)}
              </select>
            </div>
          )}
          {scheduleKind === "monthly" && (
            <div className="grid gap-2 max-w-xs">
              <Label htmlFor="scheduleDayOfMonth">Day of month (1-28)</Label>
              <Input
                id="scheduleDayOfMonth"
                type="number"
                min={1}
                max={28}
                value={scheduleDayOfMonth}
                onChange={(e) => setScheduleDayOfMonth(e.target.value)}
              />
              <p className="text-xs text-muted-foreground">Capped at 28 so February always fires.</p>
            </div>
          )}
          {scheduleKind === "custom" && (
            <div className="grid gap-2">
              <Label htmlFor="customCron">Cron expression</Label>
              <Input id="customCron" value={customCron} onChange={(e) => setCustomCron(e.target.value)} placeholder="0 3 * * *" />
              <p className="text-xs text-muted-foreground font-mono">Resolves to: {schedule}</p>
            </div>
          )}
          <p className="text-xs text-muted-foreground">
            Schedule preview: <span className="font-mono">{schedule}</span>
          </p>
        </section>
      )}

      {step === 5 && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Name &amp; review</h2>
          <div className="grid gap-2">
            <Label htmlFor="name">Backup name *</Label>
            <Input id="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. lsi → cheshire weekly" />
          </div>
          <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">Source</dt>
            <dd className="font-mono">{srcBucket}{srcPrefix ? ` (${srcPrefix})` : ""}</dd>
            <dt className="text-muted-foreground">Destination</dt>
            <dd className="font-mono">{dstBucket}{mode === "mirror" && dstPrefix ? ` (${dstPrefix})` : ""}</dd>
            <dt className="text-muted-foreground">Mode</dt>
            <dd className="font-mono">{mode}</dd>
            {mode === "snapshot" && (
              <>
                <dt className="text-muted-foreground">Retention</dt>
                <dd className="font-mono">{keepDaily}d / {keepWeekly}w / {keepMonthly}m</dd>
              </>
            )}
            <dt className="text-muted-foreground">Schedule</dt>
            <dd className="font-mono">{schedule}</dd>
          </dl>
          {createBackup.error && (
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {String((createBackup.error as Error).message || "Failed to create backup")}
            </div>
          )}
        </section>
      )}

      <div className="flex items-center justify-between gap-2">
        <Button variant="outline" onClick={() => navigate({ to: "/files/backups" })}>
          Cancel
        </Button>
        <div className="flex items-center gap-2">
          {step > 1 && (
            <Button variant="ghost" onClick={() => setStep((s) => (s - 1) as Step)}>
              Back
            </Button>
          )}
          {step < 5 && (
            <Button
              onClick={() => setStep((s) => (s + 1) as Step)}
              disabled={!canNextFromStep(step)}
            >
              Next
            </Button>
          )}
          {step === 5 && (
            <Button onClick={handleSubmit} disabled={!name.trim() || createBackup.isPending}>
              {createBackup.isPending ? "Saving…" : "Save backup"}
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

const STEP_TITLES = ["Source", "Destination", "Mode & retention", "Schedule", "Name & review"];

function ScheduleOption({
  value,
  current,
  title,
  description,
}: {
  value: ScheduleKind;
  current: ScheduleKind;
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

function ModeOption({
  value,
  current,
  title,
  description,
}: {
  value: BackupMode;
  current: BackupMode;
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

function RetentionField({
  id,
  label,
  value,
  onChange,
  hint,
}: {
  id: string;
  label: string;
  value: number;
  onChange: (n: number) => void;
  hint: string;
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type="number"
        min={0}
        value={value}
        onChange={(e) => {
          const n = parseInt(e.target.value, 10);
          onChange(Number.isFinite(n) && n >= 0 ? n : 0);
        }}
      />
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}

// buildSchedule maps the wizard's structured choices to the cron
// expression accepted by the backend's robfig/cron/v3 parser
// (5-field "minute hour dom month dow"). "manual" is the special
// case the server treats as "no recurring schedule".
function buildSchedule(opts: {
  kind: ScheduleKind;
  time: string;
  weekday: string;
  dayOfMonth: string;
  custom: string;
}): string {
  if (opts.kind === "manual") return "manual";
  if (opts.kind === "custom") return opts.custom.trim();

  // Parse HH:MM, defaulting to 03:00 if malformed.
  const [hStr, mStr] = (opts.time || "03:00").split(":");
  const h = clampInt(parseInt(hStr ?? "3", 10), 0, 23, 3);
  const m = clampInt(parseInt(mStr ?? "0", 10), 0, 59, 0);

  if (opts.kind === "daily") return `${m} ${h} * * *`;
  if (opts.kind === "weekly") {
    const dow = clampInt(parseInt(opts.weekday, 10), 0, 6, 1);
    return `${m} ${h} * * ${dow}`;
  }
  if (opts.kind === "monthly") {
    const dom = clampInt(parseInt(opts.dayOfMonth, 10), 1, 28, 1);
    return `${m} ${h} ${dom} * *`;
  }
  return "manual";
}

function clampInt(n: number, lo: number, hi: number, fallback: number): number {
  if (!Number.isFinite(n)) return fallback;
  if (n < lo) return lo;
  if (n > hi) return hi;
  return Math.floor(n);
}
