// v1.11.0a — first-run onboarding wizard.
//
// Auto-shown on the next /admin/* entry when the deploy has 0 clusters
// AND 0 non-admin users AND the operator has never dismissed it. Manual
// access via /admin/first-run remains available regardless of state.
//
// Architecture: a single multi-step component with internal step state
// rather than five sub-routes. Each step has Skip + Next buttons (Done
// at the end). Steps 3-4 are explicitly optional — the linear stepper
// surfaces them so the operator can see what's coming, but Skip
// preserves their right to bail at any point. Cluster (step 2) is the
// only mandatory step in terms of value-delivery, but even there Skip
// converges on the "I'll set up later" dismiss path so a wedged
// operator never gets trapped.
//
// Mobile-friendly: all buttons are min-h-[44px] (the Apple HIG target),
// stepper wraps to a vertical layout on small viewports, and copy
// stays short enough that no horizontal scroll is needed.

import { useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { adminPage } from "@/shared/layout/adminPage";
import { useQueryClient } from "@tanstack/react-query";
import { useCreateCluster } from "@/shared/api/mutations";
import { client } from "@/shared/api/client";
import { toast } from "sonner";
import type { components } from "@/shared/api/types.gen";

export const Route = createFileRoute("/admin/first-run")({
  component: adminPage(FirstRunWizard),
});

type Driver = "garage-v1" | "garage" | "aws-s3" | "minio";

const DRIVER_OPTIONS: { value: Driver; label: string; hint: string }[] = [
  { value: "garage-v1", label: "Garage v1", hint: "Garage 1.x admin API." },
  { value: "garage", label: "Garage v2", hint: "Garage 2.x admin API (beta upstream)." },
  { value: "aws-s3", label: "AWS S3", hint: "Native AWS S3 with IAM credentials." },
  { value: "minio", label: "MinIO / OpenMaxIO", hint: "S3-compatible local + OpenMaxIO fork." },
];

const STEPS = [
  { key: "welcome", label: "Welcome" },
  { key: "cluster", label: "Cluster" },
  { key: "oidc", label: "OIDC (optional)" },
  { key: "user", label: "Team (optional)" },
  { key: "done", label: "Done" },
] as const;

// Shared button class — min-h-[44px] meets the mobile tap-target floor
// the v1.10 smoke pass enforced across the rest of the admin surface.
const PRIMARY_BTN = "min-h-[44px] sm:min-h-0";

function FirstRunWizard() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [stepIdx, setStepIdx] = useState(0);
  // Index is clamped to [0, STEPS.length-1] by goNext/goBack so the
  // lookup is always defined; ?? "welcome" is the TS-narrowing
  // fallback for noUncheckedIndexedAccess.
  const step = STEPS[stepIdx]?.key ?? "welcome";

  // Step state lives at the wizard level so Back/Next preserves what
  // the operator already typed if they bounce around. The "skip"
  // semantic is "advance without saving" — Next attempts a save first,
  // Skip just bumps stepIdx.

  // Step 2 — cluster.
  const [clusterLabel, setClusterLabel] = useState("");
  const [driver, setDriver] = useState<Driver>("garage-v1");
  const [adminUrl, setAdminUrl] = useState("");
  const [adminToken, setAdminToken] = useState("");
  const [s3AccessKey, setS3AccessKey] = useState("");
  const [s3SecretKey, setS3SecretKey] = useState("");
  const [s3Region, setS3Region] = useState("");
  const [testState, setTestState] = useState<"idle" | "testing" | "ok" | "fail">("idle");
  const [testMessage, setTestMessage] = useState("");
  const [createdClusterId, setCreatedClusterId] = useState<string | null>(null);
  const createCluster = useCreateCluster();

  // Step 3 — OIDC. Skip-prominent. Values map onto the org-capabilities
  // OIDC block via PATCH /admin/system.
  const [oidcIssuer, setOidcIssuer] = useState("");
  const [oidcClientId, setOidcClientId] = useState("");
  const [oidcClientSecret, setOidcClientSecret] = useState("");
  const [oidcSaving, setOidcSaving] = useState(false);

  // Step 4 — team member invite.
  const [inviteUsername, setInviteUsername] = useState("");
  const [inviteLink, setInviteLink] = useState<string | null>(null);
  const [inviteSubmitting, setInviteSubmitting] = useState(false);

  const goNext = () => setStepIdx((i) => Math.min(STEPS.length - 1, i + 1));
  const goBack = () => setStepIdx((i) => Math.max(0, i - 1));

  // dismiss() is called from "I'll set up later" AND from the Done step
  // so both paths converge on the same latch — the AppShell never
  // auto-routes again. After dismiss, send the operator to /admin so
  // they land somewhere useful instead of staying on the wizard.
  const dismiss = async (destination: string = "/admin/clusters") => {
    try {
      await fetch("/api/v1/admin/onboarding/dismiss", {
        method: "POST",
        credentials: "include",
      });
    } catch {
      // Non-fatal — operator can still navigate manually. We
      // intentionally don't surface a toast here because the wizard
      // is closing anyway and a dismiss failure on the latch is a
      // server-side problem that'll resurface on the next admin entry.
    }
    queryClient.invalidateQueries({ queryKey: ["admin", "onboarding", "state"] });
    // Cast to never satisfies TanStack's literal-union `to` constraint
    // — the live string is one of two known admin paths.
    navigate({ to: destination as never });
  };

  // Test connection on step 2. Re-uses the same /admin/clusters/{cid}/_test
  // endpoint the cluster detail page uses, but we don't have a cid yet
  // — so this path actually CREATES the cluster (so we have something
  // to test), then runs the test. If the test fails we keep the cluster
  // around (operator can edit) and surface the failure inline. This is
  // a deliberate trade-off: the alternative (an unauthenticated /test
  // endpoint that doesn't persist) would duplicate driver-building
  // logic and bypass label-uniqueness validation.
  const handleTestConnection = async () => {
    if (!clusterLabel.trim()) {
      setTestState("fail");
      setTestMessage("Label is required before testing.");
      return;
    }

    setTestState("testing");
    setTestMessage("");

    const config: Record<string, string> = {};
    if (driver === "garage-v1" || driver === "garage") {
      config.admin_url = adminUrl;
      config.admin_token = adminToken;
    } else if (driver === "aws-s3") {
      config.region = s3Region || "us-east-1";
      config.access_key = s3AccessKey;
      config.secret_key = s3SecretKey;
      if (adminUrl) config.endpoint = adminUrl;
    } else if (driver === "minio") {
      config.endpoint = adminUrl;
      config.access_key = s3AccessKey;
      config.secret_key = s3SecretKey;
      config.region = s3Region || "us-east-1";
    }

    const spec: components["schemas"]["ConnectionSpec"] = {
      label: clusterLabel.trim(),
      driver,
      config,
    };

    try {
      // If we already created on a previous click, use that id and skip
      // re-create — re-runs of the Test button should test the existing
      // cluster, not stack up duplicates.
      let cid = createdClusterId;
      if (!cid) {
        const created = await createCluster.mutateAsync(spec);
        cid = (created as { id?: string }).id ?? null;
        if (cid) setCreatedClusterId(cid);
      }
      if (!cid) {
        setTestState("fail");
        setTestMessage("Cluster create returned no id.");
        return;
      }

      const { data, response } = await client.POST("/admin/clusters/{cid}/_test", {
        params: { path: { cid } },
      });
      if (!response.ok || !data) {
        setTestState("fail");
        setTestMessage(`Test failed (HTTP ${response.status})`);
        return;
      }
      const result = data as { ok?: boolean; message?: string };
      if (result.ok) {
        setTestState("ok");
        setTestMessage(result.message || "Connection healthy.");
      } else {
        setTestState("fail");
        setTestMessage(result.message || "Connection unavailable.");
      }
    } catch (err) {
      setTestState("fail");
      setTestMessage((err as Error).message || "Test failed.");
    }
  };

  // Step 2 Next: if the operator already tested + got ok, advance
  // directly. Otherwise try a create + test as a convenience so they
  // can't accidentally skip the validation. They can still hit Skip to
  // bypass the cluster entirely.
  const handleClusterNext = async () => {
    if (testState === "ok") {
      goNext();
      return;
    }
    await handleTestConnection();
    // Don't auto-advance on a fail — operator needs to see the error
    // and correct their config first.
  };

  const handleOidcSave = async () => {
    if (!oidcIssuer.trim() || !oidcClientId.trim() || !oidcClientSecret.trim()) {
      // Treat partial-fill as skip — wizard never blocks here.
      goNext();
      return;
    }
    setOidcSaving(true);
    try {
      // OIDC values live alongside other system settings; the v1.11.0a
      // wizard uses the same PATCH /admin/system surface as the
      // /admin/system page. We send only the OIDC keys; the backend
      // merges into OrgCapabilities. types.gen lags behind the
      // admin/system OIDC fields so the PATCH path arg needs a
      // suppression — same workaround pattern other admin routes use.
      // @ts-expect-error — admin/system schema not yet in types.gen
      const { response } = await client.PATCH("/admin/system", {
        body: {
          oidc: {
            issuer: oidcIssuer.trim(),
            clientId: oidcClientId.trim(),
            clientSecret: oidcClientSecret,
          },
        } as never,
      });
      if (!response.ok) {
        toast.error(`Failed to save OIDC (HTTP ${response.status})`);
        return;
      }
      toast.success("OIDC configured");
      goNext();
    } catch (err) {
      toast.error((err as Error).message || "Failed to save OIDC");
    } finally {
      setOidcSaving(false);
    }
  };

  const handleInviteSubmit = async () => {
    if (!inviteUsername.trim()) {
      goNext();
      return;
    }
    setInviteSubmitting(true);
    try {
      // POST /admin/users is in types.gen; the inviteOnly body field
      // came in via v1.3.0d and isn't in the schema, so we cast the
      // body to `never` (matching the same pattern /admin/users/new
      // already uses).
      const { data: result, response } = await client.POST("/admin/users", {
        body: { username: inviteUsername.trim(), inviteOnly: true } as never,
      });
      if (!response.ok) {
        toast.error(`Failed to create invite (HTTP ${response.status})`);
        return;
      }
      const r = result as { invite?: { token?: string } };
      if (r?.invite?.token) {
        setInviteLink(`${window.location.origin}/auth/oidc/redeem/${r.invite.token}`);
      } else {
        toast.success("User invited");
        goNext();
      }
    } catch (err) {
      toast.error((err as Error).message || "Failed to create invite");
    } finally {
      setInviteSubmitting(false);
    }
  };

  return (
    <div className="space-y-6 max-w-3xl mx-auto">
      <Stepper currentIdx={stepIdx} />

      {step === "welcome" && (
        <StepCard title="Welcome to basement!">
          <p className="text-sm text-muted-foreground">
            One pane of glass for Garage, MinIO, OpenMaxIO, and AWS S3.
            This quick walkthrough sets up your first cluster and (optionally)
            single sign-on plus a team invite. You can skip any step.
          </p>
          <div className="flex flex-col sm:flex-row gap-2 pt-2">
            <Button onClick={goNext} className={PRIMARY_BTN}>
              Get started
            </Button>
            <Button variant="outline" onClick={() => dismiss()} className={PRIMARY_BTN}>
              I&apos;ll set up later
            </Button>
          </div>
        </StepCard>
      )}

      {step === "cluster" && (
        <StepCard title="Add your first cluster">
          <p className="text-sm text-muted-foreground">
            Register a storage backend so basement can show you buckets,
            keys, and layout from here. Pick a driver and fill in the
            connection details.
          </p>

          <div className="grid gap-2">
            <Label htmlFor="firstrun-label">Label *</Label>
            <Input
              id="firstrun-label"
              value={clusterLabel}
              onChange={(e) => setClusterLabel(e.target.value)}
              placeholder="e.g. work-prod, home-lab"
              autoFocus
            />
          </div>

          <div className="grid gap-2">
            <Label>Driver</Label>
            <RadioGroup
              value={driver}
              onValueChange={(v) => setDriver(v as Driver)}
              className="grid gap-2 sm:grid-cols-2"
            >
              {DRIVER_OPTIONS.map((opt) => (
                <label
                  key={opt.value}
                  className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${
                    driver === opt.value ? "border-ring" : ""
                  }`}
                >
                  <RadioGroupItem value={opt.value} className="mt-1" />
                  <div>
                    <div className="font-medium text-sm">{opt.label}</div>
                    <p className="text-xs text-muted-foreground mt-0.5">{opt.hint}</p>
                  </div>
                </label>
              ))}
            </RadioGroup>
          </div>

          {(driver === "garage-v1" || driver === "garage") && (
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="firstrun-admin-url">Admin URL *</Label>
                <Input
                  id="firstrun-admin-url"
                  value={adminUrl}
                  onChange={(e) => setAdminUrl(e.target.value)}
                  placeholder="http://garage-host:3903"
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="firstrun-admin-token">Admin Token *</Label>
                <Input
                  id="firstrun-admin-token"
                  type="password"
                  value={adminToken}
                  onChange={(e) => setAdminToken(e.target.value)}
                />
              </div>
            </div>
          )}

          {(driver === "aws-s3" || driver === "minio") && (
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="firstrun-endpoint">
                  {driver === "aws-s3" ? "Endpoint (optional)" : "Endpoint *"}
                </Label>
                <Input
                  id="firstrun-endpoint"
                  value={adminUrl}
                  onChange={(e) => setAdminUrl(e.target.value)}
                  placeholder={driver === "aws-s3" ? "https://s3.us-east-1.amazonaws.com" : "http://minio-host:9000"}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="firstrun-region">Region</Label>
                <Input
                  id="firstrun-region"
                  value={s3Region}
                  onChange={(e) => setS3Region(e.target.value)}
                  placeholder="us-east-1"
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="firstrun-akey">Access Key *</Label>
                <Input
                  id="firstrun-akey"
                  value={s3AccessKey}
                  onChange={(e) => setS3AccessKey(e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="firstrun-skey">Secret Key *</Label>
                <Input
                  id="firstrun-skey"
                  type="password"
                  value={s3SecretKey}
                  onChange={(e) => setS3SecretKey(e.target.value)}
                />
              </div>
            </div>
          )}

          <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-2 pt-2">
            <Button
              variant="outline"
              onClick={handleTestConnection}
              disabled={testState === "testing" || !clusterLabel.trim()}
              className={PRIMARY_BTN}
            >
              {testState === "testing" ? "Testing…" : "Test connection"}
            </Button>
            {testState === "ok" && (
              <span className="text-sm text-green-600 dark:text-green-400">
                ✓ {testMessage}
              </span>
            )}
            {testState === "fail" && (
              <span className="text-sm text-red-600 dark:text-red-400" role="alert">
                {testMessage}
              </span>
            )}
          </div>

          <WizardNav
            onBack={goBack}
            onSkip={goNext}
            onNext={handleClusterNext}
            nextDisabled={createCluster.isPending}
            nextLabel={testState === "ok" ? "Next" : "Save & test"}
          />
        </StepCard>
      )}

      {step === "oidc" && (
        <StepCard title="Configure single sign-on (optional)">
          <p className="text-sm text-muted-foreground">
            Skip this step if you don&apos;t use OIDC — basement&apos;s
            local password login works on its own. You can add OIDC any
            time from <Link to="/admin/system" className="underline">Settings</Link>.
          </p>

          <div className="grid gap-2">
            <Label htmlFor="firstrun-oidc-issuer">Issuer URL</Label>
            <Input
              id="firstrun-oidc-issuer"
              value={oidcIssuer}
              onChange={(e) => setOidcIssuer(e.target.value)}
              placeholder="https://auth.example.com"
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="firstrun-oidc-client">Client ID</Label>
              <Input
                id="firstrun-oidc-client"
                value={oidcClientId}
                onChange={(e) => setOidcClientId(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="firstrun-oidc-secret">Client Secret</Label>
              <Input
                id="firstrun-oidc-secret"
                type="password"
                value={oidcClientSecret}
                onChange={(e) => setOidcClientSecret(e.target.value)}
              />
            </div>
          </div>

          <WizardNav
            onBack={goBack}
            onSkip={goNext}
            onNext={handleOidcSave}
            nextDisabled={oidcSaving}
            nextLabel={oidcSaving ? "Saving…" : "Save & continue"}
            skipProminent
          />
        </StepCard>
      )}

      {step === "user" && (
        <StepCard title="Invite a team member (optional)">
          <p className="text-sm text-muted-foreground">
            Generate an invite link to send to your first teammate. You
            can always add more from <Link to="/admin/users" className="underline">/admin/users</Link> later.
          </p>

          {!inviteLink && (
            <div className="grid gap-2">
              <Label htmlFor="firstrun-invite-username">Username</Label>
              <Input
                id="firstrun-invite-username"
                value={inviteUsername}
                onChange={(e) => setInviteUsername(e.target.value)}
                placeholder="alice"
              />
            </div>
          )}

          {inviteLink && (
            <div className="rounded-md border bg-card p-4 space-y-3">
              <Label className="text-xs uppercase tracking-wider text-muted-foreground">
                Invite link
              </Label>
              <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-2">
                <code className="flex-1 rounded bg-muted px-3 py-2 text-xs font-mono break-all">
                  {inviteLink}
                </code>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => navigator.clipboard.writeText(inviteLink)}
                  className={PRIMARY_BTN}
                >
                  Copy
                </Button>
              </div>
            </div>
          )}

          <WizardNav
            onBack={goBack}
            onSkip={goNext}
            onNext={inviteLink ? goNext : handleInviteSubmit}
            nextDisabled={inviteSubmitting}
            nextLabel={inviteLink ? "Continue" : inviteSubmitting ? "Generating…" : "Generate invite"}
            skipProminent
          />
        </StepCard>
      )}

      {step === "done" && (
        <StepCard title="Setup complete">
          <p className="text-sm text-muted-foreground">
            Nice. basement is ready to use. Here&apos;s where to go next:
          </p>
          <div className="grid gap-3 sm:grid-cols-2">
            <DoneCard
              title="Clusters"
              description="Manage your storage backends — buckets, keys, and layout."
              to="/admin/clusters"
            />
            <DoneCard
              title="My Regions"
              description="The user-side view: browse buckets via your access keys."
              to="/files"
            />
            <DoneCard
              title="System settings"
              description="Org-wide policies, gateways, OIDC, and session timeouts."
              to="/admin/system"
            />
            <DoneCard
              title="Getting started"
              description="Read the docs to learn more about what basement can do."
              href="https://github.com/mattjackson/basement#getting-started"
            />
          </div>
          <div className="flex justify-end pt-2">
            <Button onClick={() => dismiss()} className={PRIMARY_BTN}>
              Finish
            </Button>
          </div>
        </StepCard>
      )}
    </div>
  );
}

function Stepper({ currentIdx }: { currentIdx: number }) {
  return (
    <nav aria-label="Onboarding progress" className="rounded-lg border bg-card px-4 py-3">
      <ol className="flex flex-wrap items-center gap-2 sm:gap-3 text-xs sm:text-sm">
        {STEPS.map((s, i) => {
          const isActive = i === currentIdx;
          const isDone = i < currentIdx;
          return (
            <li key={s.key} className="flex items-center gap-2">
              <span
                className={`inline-flex h-7 w-7 items-center justify-center rounded-full border text-xs font-semibold ${
                  isActive
                    ? "border-ring bg-primary text-primary-foreground"
                    : isDone
                      ? "border-green-500/40 bg-green-500/10 text-green-700 dark:text-green-300"
                      : "border-border bg-muted text-muted-foreground"
                }`}
                aria-current={isActive ? "step" : undefined}
              >
                {i + 1}
              </span>
              <span
                className={
                  isActive
                    ? "font-medium text-foreground"
                    : "text-muted-foreground"
                }
              >
                {s.label}
              </span>
              {i < STEPS.length - 1 && (
                <span className="hidden sm:inline text-muted-foreground/40">·</span>
              )}
            </li>
          );
        })}
      </ol>
      <p className="text-xs text-muted-foreground mt-2">
        Step {currentIdx + 1} of {STEPS.length}
      </p>
    </nav>
  );
}

function StepCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-4 rounded-lg border bg-card p-6">
      <h1 className="text-xl sm:text-2xl font-semibold tracking-tight">{title}</h1>
      {children}
    </section>
  );
}

function WizardNav({
  onBack,
  onSkip,
  onNext,
  nextLabel,
  nextDisabled,
  skipProminent,
}: {
  onBack: () => void;
  onSkip: () => void;
  onNext: () => void | Promise<void>;
  nextLabel: string;
  nextDisabled?: boolean;
  // skipProminent renders Skip as an outline button alongside Next so
  // optional steps (OIDC, invite) feel like a real choice instead of a
  // hidden escape hatch.
  skipProminent?: boolean;
}) {
  return (
    <div className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-2 pt-2">
      <Button variant="ghost" onClick={onBack} className={PRIMARY_BTN}>
        ← Back
      </Button>
      <div className="flex flex-col sm:flex-row items-stretch gap-2">
        <Button
          variant={skipProminent ? "outline" : "ghost"}
          onClick={onSkip}
          className={PRIMARY_BTN}
        >
          Skip
        </Button>
        <Button onClick={onNext} disabled={nextDisabled} className={PRIMARY_BTN}>
          {nextLabel}
        </Button>
      </div>
    </div>
  );
}

function DoneCard({
  title,
  description,
  to,
  href,
}: {
  title: string;
  description: string;
  to?: string;
  href?: string;
}) {
  const className =
    "block rounded-md border bg-background p-4 hover:bg-muted/30 transition-colors";
  if (href) {
    return (
      <a href={href} target="_blank" rel="noreferrer" className={className}>
        <div className="font-medium text-sm">{title} →</div>
        <p className="text-xs text-muted-foreground mt-1">{description}</p>
      </a>
    );
  }
  return (
    <Link to={to as never} className={className}>
      <div className="font-medium text-sm">{title} →</div>
      <p className="text-xs text-muted-foreground mt-1">{description}</p>
    </Link>
  );
}

export default FirstRunWizard;
