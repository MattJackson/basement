import { useState, useEffect } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { toast } from "sonner";
import { adminPage } from "@/shared/layout/adminPage";
import {
  usePolicies,
  useOIDCGroupMappings,
  useUpdateOIDCGroupMappings,
  useGatewaysRegistry,
  type OIDCGroupMapping,
  type PolicyRole,
  type GatewayInfo,
  type GatewayCapabilities,
  type GatewayStatus,
} from "@/shared/api/queries";

export const Route = createFileRoute("/admin/system")({
  component: adminPage(OrgCapabilitiesPage),
});

type WebDAVSettings = {
  enabled?: boolean;
  baseUrl?: string;
};

// v1.9.0d generic per-protocol config — keyed by Gateway.Name().
// Mirrors store.GatewayConfig. Reads cross-mirror with the legacy
// `webdav` field so v1.9.0b clients stay in sync; writes from this
// card mutate BOTH shapes to satisfy the backend's "legacy wins on
// divergence" tie-break (the only safe rule given a v1.9.0b client
// could still be writing the legacy path concurrently).
type GatewayConfig = {
  enabled?: boolean;
  baseUrl?: string;
  options?: Record<string, string>;
};

type GatewaySettings = {
  webdav?: WebDAVSettings;
  protocols?: Record<string, GatewayConfig>;
};

type OrgCapabilities = {
  signupMode?: "closed" | "invite" | "open";
  enabledDrivers?: string[];
  allowUserBackends?: boolean;
  userBackendDrivers?: string[];
  oidcOnly?: boolean;
  /**
   * ADR-0003 v1.3.0a.4 amendment: operator-configurable admin session
   * TTL in seconds. Default 900 (15 min). Range 60-86400 (1 min - 24 h).
   * Backend clamps on save so any value outside the range gets snapped.
   */
  adminSessionTtlSec?: number;
  /**
   * v1.9.0b GATEWAYS: per-protocol gateway toggles + overrides. WebDAV
   * is the only configurable gateway today; SMB is documented as
   * not-supported-natively elsewhere on the page.
   */
  gateways?: GatewaySettings;
};

// TTL preset choices for the dropdown. Each entry's `value` is in
// seconds; the "Custom" option toggles a number input so the operator
// can type any value inside the [60, 86400] range.
const TTL_PRESETS: Array<{ label: string; value: number }> = [
  { label: "5 minutes", value: 300 },
  { label: "15 minutes (default)", value: 900 },
  { label: "30 minutes", value: 1800 },
  { label: "1 hour", value: 3600 },
  { label: "2 hours", value: 7200 },
  { label: "8 hours", value: 28800 },
];
const TTL_MIN_SEC = 60;
const TTL_MAX_SEC = 86400;

function OrgCapabilitiesPage() {
  const navigate = useNavigate();
  const [loading, setLoading] = useState(true);
  const [data, setData] = useState<OrgCapabilities | null>(null);

  useEffect(() => {
    fetchCapabilities();
  }, []);

  async function fetchCapabilities() {
    try {
      // @ts-ignore - API types not generated yet
      const { data: caps } = await client.GET("/admin/system", {});
      if (caps) {
        setData(caps as OrgCapabilities);
      }
    } catch (error) {
      toast.error("Failed to load system capabilities");
      navigate({ to: "/files" });
    } finally {
      setLoading(false);
    }
  }

  async function handleSave() {
    if (!data) return;

    try {
      // @ts-ignore - API types not generated yet
      await client.PATCH("/admin/system", {
        body: data,
      });
      toast.success("System capabilities updated");
    } catch (error) {
      toast.error("Failed to update system capabilities");
    }
  }

  if (loading) {
    return <div className="flex items-center justify-center min-h-[400px]">Loading...</div>;
  }

  if (!data) {
    return null;
  }

  function setEnabledDrivers(newEnabled: string[]) {
    // @ts-ignore - type compatibility issue with optional fields
    setData({ enabledDrivers: newEnabled });
  }

  function setUserBackendDrivers(newUserDrivers: string[]) {
    // @ts-ignore - type compatibility issue with optional fields
    setData({ userBackendDrivers: newUserDrivers });
  }

  const AVAILABLE_DRIVERS = ["garage", "garage-v1", "aws-s3", "minio"];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">System Configuration</h1>
        <p className="text-muted-foreground mt-1">
          Configure org-level capabilities and feature flags.
        </p>
      </div>

      <div className="space-y-4">
        {/* Signup Mode */}
        <div className="space-y-2">
          <label className="text-sm font-medium">Signup Mode</label>
          <select
            value={data.signupMode ?? "invite"}
            onChange={(e) => setData({ ...data, signupMode: e.target.value as OrgCapabilities["signupMode"] })}
            className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            <option value="closed">Closed (admin only)</option>
            <option value="invite">Invite only</option>
            <option value="open">Open (anyone can sign up)</option>
          </select>
        </div>

        {/* Enabled Drivers */}
        <div className="space-y-2">
          <label className="text-sm font-medium">Enabled Drivers</label>
          <div className="flex flex-wrap gap-2">
            {AVAILABLE_DRIVERS.map((driver) => (
              <button
                key={driver}
                onClick={() => {
                  const currentDrivers = data.enabledDrivers ?? [];
                  const newEnabled = currentDrivers.includes(driver)
                    ? currentDrivers.filter((d) => d !== driver)
                    : [...currentDrivers, driver];
                  setEnabledDrivers(newEnabled);
                }}
                className={`px-3 py-1.5 rounded-md text-sm font-medium border ${
                  (data.enabledDrivers ?? []).includes(driver)
                    ? "bg-primary text-primary-foreground border-primary"
                    : "bg-background text-muted-foreground border-input"
                }`}
              >
                {driver}
              </button>
            ))}
          </div>
        </div>

        {/* Allow User Backends */}
        <div className="space-y-2">
          <label className="text-sm font-medium flex items-center gap-2">
            <input
              type="checkbox"
              checked={data.allowUserBackends ?? false}
              onChange={(e) => setData({ ...data, allowUserBackends: e.target.checked })}
              className="h-4 w-4 rounded border-gray-300"
            />
            Allow User Backends
          </label>
        </div>

        {/* User Backend Drivers */}
        {data.allowUserBackends && (
          <div className="space-y-2">
            <label className="text-sm font-medium">User Backend Drivers</label>
            <div className="flex flex-wrap gap-2">
              {(data.enabledDrivers ?? []).map((driver) => (
                <button
                  key={driver}
                  onClick={() => {
                    const currentUsers = data.userBackendDrivers ?? [];
                    const newUserDrivers = currentUsers.includes(driver)
                      ? currentUsers.filter((d) => d !== driver)
                      : [...currentUsers, driver];
                    setUserBackendDrivers(newUserDrivers);
                  }}
                  className={`px-3 py-1.5 rounded-md text-sm font-medium border ${
                    (data.userBackendDrivers ?? []).includes(driver)
                      ? "bg-primary text-primary-foreground border-primary"
                      : "bg-background text-muted-foreground border-input"
                  }`}
                >
                  {driver}
                </button>
              ))}
            </div>
          </div>
        )}

        {/* OIDC Only */}
        <div className="space-y-2">
          <label className="text-sm font-medium flex items-center gap-2">
            <input
              type="checkbox"
              checked={data.oidcOnly ?? false}
              onChange={(e) => setData({ ...data, oidcOnly: e.target.checked })}
              className="h-4 w-4 rounded border-gray-300"
            />
            OIDC Only Mode (hide local password login)
          </label>
        </div>

        <Button onClick={handleSave}>Save Changes</Button>
      </div>

      {/* v1.9.0d GATEWAYS — registry-driven, one row per registered */}
      {/* Gateway. WebDAV ships in v1.9.0a/b and has a live Enable */}
      {/* toggle; SMB / NFS / FTP / S3 register as stubs in v1.9.0c */}
      {/* and render with a "coming soon" badge driven by the */}
      {/* Implemented() flag in the registry response. */}
      <GatewaysCard
        gateways={data.gateways ?? { webdav: { enabled: true } }}
        onChange={(next) =>
          setData((prev) => (prev ? { ...prev, gateways: next } : prev))
        }
        onSave={async () => {
          await handleSave();
        }}
      />

      {/* ADR-0003 v1.3.0a.4 amendment — operator-configurable admin */}
      {/* session TTL. Card lives below the Save Changes button on its */}
      {/* own row because it's a separate concern from feature flags + */}
      {/* drivers; the auth-gating side of the system. */}
      <AdminSessionTTLCard
        value={data.adminSessionTtlSec ?? 900}
        onChange={(next) =>
          setData((prev) =>
            prev ? { ...prev, adminSessionTtlSec: next } : prev,
          )
        }
        onSave={async () => {
          await handleSave();
        }}
      />

      {/* OIDC group-claim -> role auto-mapping (v1.3.0a). */}
      <OIDCGroupMappingsSection />

      {/* Usage overview link card — OBS.USAGE v0.9.0k surfaces the */}
      {/* storage snapshot from the System page so the operator's */}
      {/* "configure the deployment" landing pad also points at the */}
      {/* "what is the deployment doing right now" view. */}
      <Card>
        <CardHeader>
          <CardTitle>Usage Overview</CardTitle>
          <CardDescription>
            Snapshot of storage usage across every cluster: totals, per-cluster
            breakdown, and the top buckets by size and object count.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Link
            to="/admin/usage"
            className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline"
          >
            Open the usage overview &rarr;
          </Link>
        </CardContent>
      </Card>

      {/* Policies link card — ADR-0001 v0.9.0g surfaces the matrix */}
      {/* editor from the System page too, not just the persona menu. */}
      <Card>
        <CardHeader>
          <CardTitle>Policies</CardTitle>
          <CardDescription>
            Roles, capabilities, and per-user assignments. The matrix editor
            controls who can do what, at which scope.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Link
            to="/admin/policies"
            className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline"
          >
            Open the policy matrix editor &rarr;
          </Link>
        </CardContent>
      </Card>

      {/* Audit log link card — v1.0.0c surfaces the append-only event */}
      {/* log so incident response has one click from the system page. */}
      <Card>
        <CardHeader>
          <CardTitle>Audit log</CardTitle>
          <CardDescription>
            Append-only record of every mutating action across basement.
            Who did what, when, against which resource, success or failure.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Link
            to="/admin/audit"
            className="inline-flex items-center gap-1 text-sm font-medium text-primary underline-offset-4 hover:underline"
          >
            Open the audit log &rarr;
          </Link>
        </CardContent>
      </Card>
    </div>
  );
}

// AdminSessionTTLCard — ADR-0003 v1.3.0a.4 amendment.
//
// Lets a host admin pick how long an /auth/elevate session stays in
// ADMIN mode before it falls back to USER. Dropdown of common presets
// (5 min / 15 min / 30 min / 1 h / 2 h / 8 h) plus a Custom option that
// reveals a number input for any value in [60, 86400]. The backend
// clamps on save so out-of-band values get snapped silently, but we
// validate on the FE too so the operator sees the floor/ceiling
// inline rather than discovering it after the round-trip.
function AdminSessionTTLCard({
  value,
  onChange,
  onSave,
}: {
  value: number;
  onChange: (next: number) => void;
  onSave: () => Promise<void>;
}) {
  const matchedPreset = TTL_PRESETS.find((p) => p.value === value);
  const [isCustom, setIsCustom] = useState(matchedPreset === undefined);
  const [customStr, setCustomStr] = useState(String(value));
  const [saving, setSaving] = useState(false);

  // Keep customStr in sync when the parent changes value externally
  // (e.g. on initial fetch).
  useEffect(() => {
    setCustomStr(String(value));
    setIsCustom(TTL_PRESETS.find((p) => p.value === value) === undefined);
  }, [value]);

  const customError = (() => {
    if (!isCustom) return null;
    const n = parseInt(customStr, 10);
    if (!Number.isFinite(n)) return "Enter a number of seconds";
    if (n < TTL_MIN_SEC) return `Minimum ${TTL_MIN_SEC} seconds (1 minute)`;
    if (n > TTL_MAX_SEC) return `Maximum ${TTL_MAX_SEC} seconds (24 hours)`;
    return null;
  })();

  const humanReadable = (() => {
    if (value <= 0) return "—";
    if (value < 60) return `${value} s`;
    if (value < 3600) return `${Math.round(value / 60)} min`;
    if (value < 86400)
      return `${(value / 3600).toFixed(value % 3600 === 0 ? 0 : 1)} h`;
    return `${Math.round(value / 86400)} d`;
  })();

  const handlePresetChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const v = e.target.value;
    if (v === "custom") {
      setIsCustom(true);
      return;
    }
    const n = parseInt(v, 10);
    if (Number.isFinite(n)) {
      setIsCustom(false);
      onChange(n);
    }
  };

  const handleCustomBlur = () => {
    const n = parseInt(customStr, 10);
    if (!Number.isFinite(n)) return;
    const clamped = Math.min(TTL_MAX_SEC, Math.max(TTL_MIN_SEC, n));
    setCustomStr(String(clamped));
    onChange(clamped);
  };

  const handleSave = async () => {
    if (customError) return;
    setSaving(true);
    try {
      await onSave();
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card data-testid="admin-session-ttl-card">
      <CardHeader>
        <CardTitle>Admin session timeout</CardTitle>
        <CardDescription>
          How long an elevated admin session stays active before
          dropping back to user mode. Shorter values are safer; longer
          values reduce friction for long admin tasks. Operators with
          an expired session see a banner at the top of admin pages
          inviting them to re-elevate &mdash; their work isn&rsquo;t
          lost.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="space-y-2">
          <label className="text-sm font-medium" htmlFor="admin-ttl-preset">
            Timeout
          </label>
          <select
            id="admin-ttl-preset"
            data-testid="admin-ttl-preset"
            value={isCustom ? "custom" : String(value)}
            onChange={handlePresetChange}
            className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            {TTL_PRESETS.map((p) => (
              <option key={p.value} value={String(p.value)}>
                {p.label}
              </option>
            ))}
            <option value="custom">Custom&hellip;</option>
          </select>
        </div>

        {isCustom && (
          <div className="space-y-1">
            <label
              className="text-sm font-medium"
              htmlFor="admin-ttl-custom-sec"
            >
              Custom value (seconds)
            </label>
            <Input
              id="admin-ttl-custom-sec"
              data-testid="admin-ttl-custom-sec"
              type="number"
              min={TTL_MIN_SEC}
              max={TTL_MAX_SEC}
              step={1}
              value={customStr}
              onChange={(e) => setCustomStr(e.target.value)}
              onBlur={handleCustomBlur}
            />
            <p className="text-xs text-muted-foreground">
              Range: {TTL_MIN_SEC}&ndash;{TTL_MAX_SEC} seconds. Values
              outside this range are clamped automatically.
            </p>
            {customError && (
              <p
                className="text-xs text-destructive"
                data-testid="admin-ttl-custom-error"
              >
                {customError}
              </p>
            )}
          </div>
        )}

        <div className="flex items-center justify-between">
          <p
            className="text-xs text-muted-foreground"
            data-testid="admin-ttl-human"
          >
            Current setting: <span className="font-mono">{humanReadable}</span>
          </p>
          <Button
            onClick={handleSave}
            disabled={saving || customError !== null}
            data-testid="admin-ttl-save"
          >
            {saving ? "Saving…" : "Save timeout"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}

// v1.9.0d GATEWAYS card — registry-driven roster.
//
// Replaces the v1.9.0b WebDAV-hardcoded card with a generic one that
// renders whatever GET /api/v1/admin/gateways returns. Each row:
//
//   - DisplayName + capability chips (read / write / delete / move /
//     lock / basic-auth / bearer-auth / sigv4-auth)
//   - Implementation badge (live "Implemented" or muted "Coming soon")
//   - Live status (running, active connections, last activity, total
//     requests) when implemented; "not implemented yet" when not
//   - Mount URL or listen address when applicable
//   - Per-platform connect hints (collapsed details)
//   - Enable toggle when Implemented(); hidden otherwise — the FE
//     never offers an enable affordance for a stub
//
// The card writes both the legacy `webdav` field AND the generic
// `protocols.webdav` entry so the v1.9.0b kill-switch contract
// continues to work (backend tie-breaks toward the legacy field on
// divergence — keeping the two in sync from the FE is what makes the
// "legacy wins" rule safe).
//
// While the network call is in flight the card renders a loading
// row; if /admin/gateways 503s (registry not wired) we degrade to a
// banner pointing at the operator log rather than rendering an empty
// card that looks like a config error on the user's side.
export function GatewaysCard({
  gateways,
  onChange,
  onSave,
}: {
  gateways: GatewaySettings;
  onChange: (next: GatewaySettings) => void;
  onSave: () => Promise<void>;
}) {
  const registry = useGatewaysRegistry();
  const [savingName, setSavingName] = useState<string | null>(null);

  async function handleSave(name: string) {
    setSavingName(name);
    try {
      await onSave();
    } finally {
      setSavingName(null);
    }
  }

  // setProtocolConfig writes both halves of the back-compat shape:
  //   - protocols[name] for the v1.9.0d generic map (the new
  //     canonical wire shape), AND
  //   - the legacy `webdav` field when name === "webdav", because
  //     the backend tie-breaks toward that field on divergence.
  function setProtocolConfig(name: string, next: GatewayConfig) {
    const protocols = { ...(gateways.protocols ?? {}) };
    protocols[name] = { ...(protocols[name] ?? {}), ...next };
    const merged: GatewaySettings = { ...gateways, protocols };
    if (name === "webdav") {
      merged.webdav = {
        ...(gateways.webdav ?? {}),
        ...(next.enabled !== undefined ? { enabled: next.enabled } : {}),
        ...(next.baseUrl !== undefined ? { baseUrl: next.baseUrl } : {}),
      };
    }
    onChange(merged);
  }

  return (
    <Card data-testid="gateways-card">
      <CardHeader>
        <CardTitle>Gateways</CardTitle>
        <CardDescription>
          Mount your buckets as a network drive on Mac, Windows, Linux,
          iOS, and Android. WebDAV is available today and works with any
          client that speaks the protocol &mdash; Finder&apos;s Connect to
          Server, Windows Explorer&apos;s Map Network Drive, or your file
          manager of choice. Additional protocols are placeholders for
          when implementations are contributed.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {registry.isLoading ? (
          <p
            className="text-sm text-muted-foreground"
            data-testid="gateways-loading"
          >
            Loading gateway registry&hellip;
          </p>
        ) : registry.isError ? (
          <div
            className="rounded-md border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
            data-testid="gateways-error"
          >
            Gateway registry unavailable: {registry.error.message}. Check
            the basement server log; the registry wires up at boot.
          </div>
        ) : (
          // v1.11.0.19: surface implemented gateways first so the operator
          // sees the live row (WebDAV today) before scrolling past four
          // "coming soon" stubs. Each group stays alphabetical so the
          // order is still deterministic as new gateways land. Backend
          // Registry.All() stays alpha — this is a presentation tweak.
          [...(registry.data ?? [])]
            .sort((a, b) => {
              if (a.implemented !== b.implemented)
                return a.implemented ? -1 : 1;
              return a.name.localeCompare(b.name);
            })
            .map((g, idx) => (
            <div key={g.name}>
              {idx > 0 && <hr className="my-4 border-input" />}
              <GatewayRow
                gateway={g}
                config={
                  gateways.protocols?.[g.name] ??
                  (g.name === "webdav"
                    ? {
                        enabled: gateways.webdav?.enabled ?? true,
                        baseUrl: gateways.webdav?.baseUrl,
                      }
                    : { enabled: g.enabled })
                }
                onChange={(next) => setProtocolConfig(g.name, next)}
                onSave={() => handleSave(g.name)}
                saving={savingName === g.name}
              />
            </div>
          ))
        )}
      </CardContent>
    </Card>
  );
}

// GatewayRow renders one gateway in the registry-driven card. The
// shape is uniform across protocols — capability chips, status block,
// optional mount URL, optional connect hints, optional enable toggle
// — so adding a new gateway in v1.10+ requires no UI changes.
//
// Implementation status drives two affordances:
//   - The badge color (Implemented = primary; not = muted "coming soon")
//   - Whether the enable toggle renders at all (hidden for stubs; the
//     spec is explicit that stubs show "—" in place of a toggle)
function GatewayRow({
  gateway,
  config,
  onChange,
  onSave,
  saving,
}: {
  gateway: GatewayInfo;
  config: GatewayConfig;
  onChange: (next: GatewayConfig) => void;
  onSave: () => Promise<void>;
  saving: boolean;
}) {
  const implemented = gateway.implemented;
  const mountURL = mountURLFor(gateway, config);
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    if (!mountURL) return;
    try {
      if (typeof navigator !== "undefined" && navigator.clipboard) {
        await navigator.clipboard.writeText(mountURL);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      }
    } catch {
      // Clipboard write can fail under permissions; operators can
      // still select-and-copy the URL from the read-only field.
    }
  }

  return (
    <section
      className="space-y-3"
      data-testid={`gateways-${gateway.name}-section`}
      aria-labelledby={`gateways-${gateway.name}-heading`}
    >
      <div className="flex items-start justify-between gap-4">
        <div className="flex-1">
          <div className="flex items-center gap-2">
            <h3
              id={`gateways-${gateway.name}-heading`}
              className="text-base font-medium"
            >
              {gateway.displayName}
            </h3>
            <ImplementationBadge implemented={implemented} />
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {gateway.description}
          </p>
        </div>
        {implemented ? (
          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              data-testid={`gateways-${gateway.name}-enabled`}
              checked={config.enabled ?? true}
              onChange={(e) => onChange({ enabled: e.target.checked })}
              className="h-4 w-4 rounded border-gray-300"
            />
            Enabled
          </label>
        ) : (
          <span
            className="text-sm text-muted-foreground"
            data-testid={`gateways-${gateway.name}-no-toggle`}
            aria-label="Enable toggle hidden for unimplemented gateway"
          >
            &mdash;
          </span>
        )}
      </div>

      <CapabilityChips
        gatewayName={gateway.name}
        capabilities={gateway.capabilities}
      />

      <StatusBlock
        gatewayName={gateway.name}
        implemented={implemented}
        status={gateway.status}
        listenAddress={gateway.listenAddress}
      />

      {implemented && mountURL && (
        <div className="space-y-1">
          <label
            className="text-sm font-medium"
            htmlFor={`gateway-${gateway.name}-mount-url`}
          >
            Mount URL
          </label>
          <div className="flex items-center gap-2">
            <Input
              id={`gateway-${gateway.name}-mount-url`}
              data-testid={`gateways-${gateway.name}-mount-url`}
              readOnly
              value={mountURL}
              className="font-mono text-xs"
            />
            <Button
              type="button"
              variant="ghost"
              onClick={handleCopy}
              data-testid={`gateways-${gateway.name}-copy`}
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </div>
      )}

      {implemented && (
        <div className="space-y-1">
          <label
            className="text-sm font-medium"
            htmlFor={`gateway-${gateway.name}-base-url`}
          >
            Base URL override (optional)
          </label>
          <Input
            id={`gateway-${gateway.name}-base-url`}
            data-testid={`gateways-${gateway.name}-base-url`}
            placeholder={defaultMountURL(gateway.name)}
            value={config.baseUrl ?? ""}
            onChange={(e) => onChange({ baseUrl: e.target.value })}
          />
          <p className="text-xs text-muted-foreground">
            Use when basement sits behind a reverse proxy that exposes
            this gateway on a different host. Leave blank to use the
            current origin.
          </p>
        </div>
      )}

      {implemented && <ConnectHints gatewayName={gateway.name} />}

      <DocsLink gatewayName={gateway.name} implemented={implemented} />

      {implemented && (
        <div className="flex justify-end">
          <Button
            onClick={onSave}
            disabled={saving}
            data-testid={`gateways-${gateway.name}-save`}
          >
            {saving ? "Saving…" : `Save ${gateway.displayName} settings`}
          </Button>
        </div>
      )}
    </section>
  );
}

// ImplementationBadge renders the per-row status pill. Two states:
// "Implemented" (primary) and "Coming soon" (muted). The
// distinction drives every conditional in GatewayRow above; the
// badge is the visible marker that tells the operator at a glance
// which gateways are real today.
function ImplementationBadge({ implemented }: { implemented: boolean }) {
  if (implemented) {
    return (
      <span
        className="rounded-full bg-primary/15 px-2 py-0.5 text-xs font-medium text-primary"
        data-testid="gateway-badge-implemented"
      >
        Implemented
      </span>
    );
  }
  return (
    <span
      className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground"
      data-testid="gateway-badge-coming-soon"
    >
      Coming soon
    </span>
  );
}

// CapabilityChips renders the protocol's advertised surface (read /
// write / delete / move / lock / per-auth methods). Each chip is
// rendered only when the capability is set, so a protocol with no
// auth methods gets no auth chips — keeps the row honest.
function CapabilityChips({
  gatewayName,
  capabilities,
}: {
  gatewayName: string;
  capabilities: GatewayCapabilities;
}) {
  const chips: string[] = [];
  if (capabilities.read) chips.push("read");
  if (capabilities.write) chips.push("write");
  if (capabilities.delete) chips.push("delete");
  if (capabilities.move) chips.push("move");
  if (capabilities.lock) chips.push("lock");
  if (capabilities.basicAuth) chips.push("basic-auth");
  if (capabilities.bearerAuth) chips.push("bearer-auth");
  if (capabilities.sigV4Auth) chips.push("sigv4-auth");

  if (chips.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        Capabilities: <span className="italic">unspecified</span>
      </p>
    );
  }

  return (
    <div
      className="flex flex-wrap items-center gap-1.5 text-xs"
      data-testid={`gateways-${gatewayName}-capabilities`}
    >
      <span className="text-muted-foreground">Capabilities:</span>
      {chips.map((c) => (
        <span
          key={c}
          className="rounded-md border border-input bg-muted/30 px-1.5 py-0.5 font-mono"
        >
          {c}
        </span>
      ))}
    </div>
  );
}

// StatusBlock surfaces the runtime stats /admin/gateways returns.
// Stub gateways short-circuit to "not implemented yet"; live ones
// render running + connection + activity + counters with em-dashes
// for absent values so the operator sees "—" rather than "0" for
// counters the gateway doesn't track.
function StatusBlock({
  gatewayName,
  implemented,
  status,
  listenAddress,
}: {
  gatewayName: string;
  implemented: boolean;
  status: GatewayStatus;
  listenAddress?: string;
}) {
  if (!implemented) {
    return (
      <p
        className="text-xs text-muted-foreground"
        data-testid={`gateways-${gatewayName}-status`}
      >
        Status: <span className="italic">not implemented yet</span>
      </p>
    );
  }

  const runningLabel = status.running ? "running" : "stopped";
  const activeConnections = status.activeConnections;
  const totalRequests = status.totalRequests;
  const lastActivity = status.lastActivity
    ? humanRelative(status.lastActivity)
    : "—";

  return (
    <div
      className="text-xs text-muted-foreground"
      data-testid={`gateways-${gatewayName}-status`}
    >
      <p>
        Status: <span className="font-mono">{runningLabel}</span>,{" "}
        {activeConnections !== undefined
          ? `${activeConnections} active connection${
              activeConnections === 1 ? "" : "s"
            }`
          : "active connections —"}
        , last activity:{" "}
        <span className="font-mono">{lastActivity}</span>
        {totalRequests !== undefined && (
          <>
            , total requests: <span className="font-mono">{totalRequests}</span>
          </>
        )}
        {listenAddress && (
          <>
            , listen: <span className="font-mono">{listenAddress}</span>
          </>
        )}
      </p>
      {status.lastError && (
        <p className="mt-1 text-destructive" data-testid={`gateways-${gatewayName}-last-error`}>
          Last error: {status.lastError}
        </p>
      )}
    </div>
  );
}

// ConnectHints renders the collapsed per-platform mount instructions.
// Today only WebDAV has bespoke per-platform copy; other implemented
// gateways will gain their own clauses here as they land. Stubs
// don't get connect hints (the DocsLink card carries the pointer
// to the implementation tracking instead).
function ConnectHints({ gatewayName }: { gatewayName: string }) {
  if (gatewayName === "webdav") {
    return (
      <details className="rounded-md border border-input bg-muted/30 p-3">
        <summary className="cursor-pointer text-sm font-medium">
          Connect from your platform
        </summary>
        <ul className="mt-3 space-y-2 text-sm">
          <li>
            <strong>macOS Finder:</strong> press &#8984;K, paste the mount
            URL, then enter your basement username and password.
          </li>
          <li>
            <strong>Windows Explorer:</strong> This PC &rarr; Map network
            drive, paste the URL into the Folder field, and authenticate
            with basement credentials.
          </li>
          <li>
            <strong>Linux (Nautilus):</strong> Connect to Server, paste
            the URL with the <code>davs://</code> scheme (e.g.{" "}
            <code>davs://basement.example/webdav</code>).
          </li>
          <li>
            <strong>iOS Files:</strong> Browse tab &rarr; &hellip; menu
            &rarr; Connect to Server, paste the URL, choose Registered
            User, enter credentials.
          </li>
        </ul>
        <p className="mt-3 text-xs text-muted-foreground">
          For automation, use a service account from{" "}
          <Link to="/admin/service-accounts" className="underline">
            /admin/service-accounts
          </Link>{" "}
          &mdash; the <code>BMNT&hellip;</code> key goes in the username
          field, the shown-once secret in the password field.
        </p>
      </details>
    );
  }
  return null;
}

// DocsLink renders the per-protocol docs pointer. Live gateways link
// at their full integration guide; stubs link at the implementation-
// tracking placeholder doc so the operator has a single jump-off
// point for "when's this going to ship".
function DocsLink({
  gatewayName,
  implemented,
}: {
  gatewayName: string;
  implemented: boolean;
}) {
  const href = `/docs/integrations/${gatewayName}.md`;
  const label = implemented
    ? `${gatewayName.toUpperCase()} integration guide`
    : `${gatewayName.toUpperCase()} implementation tracking`;
  return (
    <p className="text-xs">
      <a
        href={href}
        className="font-medium text-primary underline-offset-4 hover:underline"
        target="_blank"
        rel="noreferrer"
        data-testid={`gateways-${gatewayName}-docs`}
      >
        {label} &rarr;
      </a>
    </p>
  );
}

// mountURLFor returns the operator-facing URL the row's Copy button
// hands to the clipboard. Operator override wins; otherwise the
// defaultMountURL helper picks a sensible per-protocol shape. typeof
// check keeps SSR / vitest happy when window isn't defined yet.
function mountURLFor(
  gateway: GatewayInfo,
  config: GatewayConfig,
): string {
  if (config.baseUrl && config.baseUrl.trim() !== "") {
    return config.baseUrl.trim();
  }
  return defaultMountURL(gateway.name);
}

function defaultMountURL(name: string): string {
  const origin =
    typeof window !== "undefined"
      ? window.location.origin
      : "https://basement.example";
  // HTTP-mounted gateways live at /{name} on the same origin. The
  // port-bound ones (SMB / NFS / FTP — none implemented today) won't
  // call this helper because GatewayRow only shows the Mount URL
  // field for implemented gateways.
  return `${origin}/${name}`;
}

// humanRelative converts an ISO timestamp into a compact "Xm ago"
// string. Kept inline because the codebase doesn't yet have a
// shared helper and the formatting is one-shot for this card.
function humanRelative(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "—";
  const diff = Date.now() - t;
  if (diff < 0) return "in the future";
  const sec = Math.round(diff / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.round(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.round(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const days = Math.round(hr / 24);
  return `${days}d ago`;
}

// v1.3.0a — OIDC Group -> Role mapping editor.
//
// Declares "if this OIDC claim contains this value, grant this role at
// this scope" rules. On every OIDC login basement reads the user's
// ID-token claims and auto-creates matching role assignments (and
// revokes stale auto-assignments). Manually-granted roles are sacred
// — never touched by sync.
//
// Per the popups-max-2-fields memory the add modal stays a modal
// (claim + value + role-dropdown + scope = 4 fields) because adding a
// mapping is a low-frequency one-off — a dedicated route would be
// overkill. The table itself is inline and that's where operators
// spend their reading time.
function OIDCGroupMappingsSection() {
  const queryClient = useQueryClient();
  const { data: mappingsData, isLoading } = useOIDCGroupMappings();
  const { data: policiesData } = usePolicies();
  const updateMappings = useUpdateOIDCGroupMappings();
  const [addOpen, setAddOpen] = useState(false);

  const mappings = mappingsData?.mappings ?? [];
  const roles: PolicyRole[] = policiesData?.roles ?? [];

  async function saveMappings(next: OIDCGroupMapping[]) {
    try {
      await updateMappings.mutateAsync(next);
      await queryClient.invalidateQueries({ queryKey: ["admin", "oidc-group-mappings"] });
      toast.success("OIDC mappings updated");
    } catch (err) {
      toast.error(`Failed to update mappings: ${(err as Error).message}`);
    }
  }

  async function handleDelete(idx: number) {
    const next = mappings.filter((_, i) => i !== idx);
    await saveMappings(next);
  }

  async function handleAdd(m: OIDCGroupMapping) {
    const next = [...mappings, m];
    await saveMappings(next);
    setAddOpen(false);
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>OIDC Group &rarr; Role Mapping</CardTitle>
        <CardDescription>
          Declare which OIDC claim values grant which basement roles. Applied
          on every OIDC login: matching mappings are auto-assigned, stale
          ones are revoked. Manually-granted roles are never touched. Changes
          take effect at each user&rsquo;s next sign-in.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {isLoading ? (
          <p className="text-sm text-muted-foreground">Loading mappings&hellip;</p>
        ) : mappings.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No mappings configured. Without any mappings, OIDC users sign in
            with no role assignments &mdash; you&rsquo;ll need to assign roles
            manually at /admin/policies.
          </p>
        ) : (
          <div className="overflow-x-auto rounded-md border border-input">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-left">
                <tr>
                  <th className="px-3 py-2 font-medium">Claim</th>
                  <th className="px-3 py-2 font-medium">Value</th>
                  <th className="px-3 py-2 font-medium">Role</th>
                  <th className="px-3 py-2 font-medium">Scope</th>
                  <th className="px-3 py-2 font-medium text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {mappings.map((m, idx) => (
                  <tr key={`${m.claim}-${m.claimValue}-${m.roleId}-${m.scope}-${idx}`} className="border-t border-input">
                    <td className="px-3 py-2 font-mono text-xs">{m.claim}</td>
                    <td className="px-3 py-2 font-mono text-xs">{m.claimValue}</td>
                    <td className="px-3 py-2">{m.roleId}</td>
                    <td className="px-3 py-2 font-mono text-xs">{m.scope}</td>
                    <td className="px-3 py-2 text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleDelete(idx)}
                        disabled={updateMappings.isPending}
                      >
                        Delete
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        <div className="flex items-center justify-between">
          <p className="text-xs text-muted-foreground">
            Test a mapping by logging the affected user out and back in &mdash;
            existing sessions are unaffected until they re-authenticate.
          </p>
          <Button onClick={() => setAddOpen(true)} disabled={updateMappings.isPending}>
            Add mapping
          </Button>
        </div>
      </CardContent>

      <AddMappingDialog
        open={addOpen}
        onOpenChange={setAddOpen}
        roles={roles}
        onSubmit={handleAdd}
        submitting={updateMappings.isPending}
      />
    </Card>
  );
}

function AddMappingDialog({
  open,
  onOpenChange,
  roles,
  onSubmit,
  submitting,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  roles: PolicyRole[];
  onSubmit: (m: OIDCGroupMapping) => void;
  submitting: boolean;
}) {
  const [claim, setClaim] = useState("groups");
  const [claimValue, setClaimValue] = useState("");
  const [roleId, setRoleId] = useState("");
  const [scope, setScope] = useState("host:*");

  useEffect(() => {
    if (open) {
      setClaim("groups");
      setClaimValue("");
      setRoleId(roles[0]?.id ?? "");
      setScope("host:*");
    }
  }, [open, roles]);

  const canSubmit =
    claim.trim() !== "" &&
    claimValue.trim() !== "" &&
    roleId.trim() !== "" &&
    scope.trim() !== "" &&
    !submitting;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    onSubmit({
      claim: claim.trim(),
      claimValue: claimValue.trim(),
      roleId: roleId.trim(),
      scope: scope.trim(),
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={handleSubmit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>Add OIDC group mapping</DialogTitle>
            <DialogDescription>
              When the named claim contains the value, the user is granted
              the selected role at the given scope on next OIDC sign-in.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="space-y-1">
              <label className="text-sm font-medium" htmlFor="oidc-mapping-claim">Claim name</label>
              <Input
                id="oidc-mapping-claim"
                value={claim}
                onChange={(e) => setClaim(e.target.value)}
                placeholder="groups"
              />
              <p className="text-xs text-muted-foreground">
                Common values: <code>groups</code> (Authentik / Keycloak), <code>roles</code> (Pocket-ID).
              </p>
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium" htmlFor="oidc-mapping-value">Claim value</label>
              <Input
                id="oidc-mapping-value"
                value={claimValue}
                onChange={(e) => setClaimValue(e.target.value)}
                placeholder="platform-admins"
              />
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium" htmlFor="oidc-mapping-role">Role</label>
              <select
                id="oidc-mapping-role"
                value={roleId}
                onChange={(e) => setRoleId(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              >
                {roles.length === 0 ? (
                  <option value="">No roles defined</option>
                ) : (
                  roles.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.label || r.id}
                    </option>
                  ))
                )}
              </select>
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium" htmlFor="oidc-mapping-scope">Scope</label>
              <Input
                id="oidc-mapping-scope"
                value={scope}
                onChange={(e) => setScope(e.target.value)}
                placeholder="host:*"
              />
              <p className="text-xs text-muted-foreground">
                Use the ADR-0001 grammar: <code>host:*</code>, <code>cluster:*</code>, <code>cluster:&#123;cid&#125;</code>, etc.
              </p>
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canSubmit}>
              {submitting ? "Saving…" : "Add mapping"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
