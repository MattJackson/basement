import { useState, useEffect, useRef } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { client } from "@/shared/api/client";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Checkbox } from "@/components/ui/checkbox";
import { 
  Select, 
  SelectContent, 
  SelectItem, 
  SelectTrigger, 
  SelectValue 
} from "@/components/ui/select";
import { toast } from "sonner";
import { adminPage } from "@/shared/layout/adminPage";
import {
  usePolicies,
  useOIDCGroupMappings,
  useUpdateOIDCGroupMappings,
  useGatewaysRegistry,
  useOrgCapabilities,
  type OIDCGroupMapping,
  type PolicyRole,
  type GatewayInfo,
  type GatewayCapabilities,
  type GatewayStatus,
  type Skin,
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
      // @ts-ignore - path not in generated API types
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

  // v1.11.0.23: these MUST spread `data` first — without the spread, setData
  // overwrites the entire OrgCapabilities object with only the one field,
  // wiping signupMode / allowUserBackends / oidcOnly / gateways / etc. The
  // earlier @ts-ignore was papering over that error instead of fixing it.
  function setEnabledDrivers(newEnabled: string[]) {
    setData({ ...data, enabledDrivers: newEnabled });
  }

  function setUserBackendDrivers(newUserDrivers: string[]) {
    setData({ ...data, userBackendDrivers: newUserDrivers });
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
          <label htmlFor="signup-mode" className="text-sm font-medium">Signup Mode</label>
          <select
            id="signup-mode"
            value={data.signupMode || "invite"}
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

      {/* v1.13.1: Skin manager with three-control layout */}
      <SkinsManager />

      {/* v1.13.33: removed the trailing Usage Overview / Policies /
          Audit log "shortcut" cards. They duplicate the UserMenu
          admin-nav entries (also Usage / Policies / Audit) and the
          operator flagged the duplication as muddying the System
          Settings page. Nav for those pages lives in the dropdown. */}
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
          <label htmlFor="admin-ttl-preset" className="text-sm font-medium">
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
      <CardContent className="space-y-4">
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
          //
          // v1.11.0.21: hero/stub split — implemented rows render as the
          // hero (full chrome, prominent mount URL), stubs render in a
          // muted single-line treatment so the operator's eye lands on
          // the live row first. No <hr/> between rows: the visual weight
          // contrast carries the grouping on its own.
          [...(registry.data ?? [])]
            .sort((a, b) => {
              if (a.implemented !== b.implemented)
                return a.implemented ? -1 : 1;
              return a.name.localeCompare(b.name);
            })
            .map((g) => (
              <GatewayRow
                key={g.name}
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
            ))
        )}
      </CardContent>
    </Card>
  );
}

// GatewayRow renders one gateway in the registry-driven card. There
// are two visual treatments driven by Implemented(): the hero (full
// chrome — mount URL, copy button, connect hints, enable toggle, save)
// and the muted stub (single-line description + chips, no affordances).
//
// v1.11.0.21 split the row into two sub-components so each shape can
// be tuned independently — the implemented row carries the operator's
// attention, the stubs sit quietly until they grow real implementations.
function GatewayRow(props: {
  gateway: GatewayInfo;
  config: GatewayConfig;
  onChange: (next: GatewayConfig) => void;
  onSave: () => Promise<void>;
  saving: boolean;
}) {
  if (props.gateway.implemented) {
    return <ImplementedGatewayRow {...props} />;
  }
  return <StubGatewayRow gateway={props.gateway} />;
}

// ImplementedGatewayRow is the hero treatment — the operator's
// landing pad for the live gateway. Heading, status pill, mount URL
// with prominent copy, per-platform connect hints, and the save
// button live here. Renders inside a bordered panel so it visually
// stands apart from the muted stub rows below.
function ImplementedGatewayRow({
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
      className="rounded-lg border border-input bg-muted/20 p-4 space-y-4"
      data-testid={`gateways-${gateway.name}-section`}
      aria-labelledby={`gateways-${gateway.name}-heading`}
    >
      {/* Header: title + status pill on one line; toggle stacks below */}
      {/* on phones so the row collapses cleanly. */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between sm:gap-4">
        <div className="flex items-center gap-2 flex-wrap">
          <h2
            id={`gateways-${gateway.name}-heading`}
            className="text-base font-semibold"
          >
            {gateway.displayName}
          </h2>
          <ImplementationBadge implemented={true} />
          <StatusPill
            gatewayName={gateway.name}
            status={gateway.status}
          />
        </div>
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
      </div>

      {gateway.status.lastError && (
        <p
          className="text-xs text-destructive"
          data-testid={`gateways-${gateway.name}-last-error`}
        >
          Last error: {gateway.status.lastError}
        </p>
      )}

      {mountURL && (
        <div className="space-y-1.5">
          <label
            className="text-sm font-medium"
            htmlFor={`gateway-${gateway.name}-mount-url`}
          >
            Mount URL
          </label>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <Input
              id={`gateway-${gateway.name}-mount-url`}
              data-testid={`gateways-${gateway.name}-mount-url`}
              readOnly
              value={mountURL}
              className="font-mono text-xs"
              onFocus={(e) => e.currentTarget.select()}
            />
            <Button
              type="button"
              variant="outline"
              onClick={handleCopy}
              data-testid={`gateways-${gateway.name}-copy`}
              className="sm:w-24"
            >
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </div>
      )}

      <ConnectHints gatewayName={gateway.name} />

      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">Advanced</summary>
        <div className="mt-2 space-y-1.5">
          <label
            className="text-xs font-medium text-foreground"
            htmlFor={`gateway-${gateway.name}-base-url`}
          >
            Base URL override
          </label>
          <Input
            id={`gateway-${gateway.name}-base-url`}
            data-testid={`gateways-${gateway.name}-base-url`}
            placeholder={defaultMountURL(gateway.name)}
            value={config.baseUrl ?? ""}
            onChange={(e) => onChange({ baseUrl: e.target.value })}
            className="font-mono text-xs"
          />
          <p>
            Set this when basement is behind a reverse proxy on a
            different host. Leave blank to use the current origin.
          </p>
        </div>
      </details>

      <CapabilityChips
        gatewayName={gateway.name}
        capabilities={gateway.capabilities}
        muted={false}
      />

      {/* v1.11.0.20: docs link only renders for implemented gateways. */}
      {/* Per feedback_dont_announce_v2 stub rows do not link to docs */}
      {/* that implicitly commit to a delivery window. */}
      <div className="flex flex-col-reverse items-stretch justify-between gap-2 sm:flex-row sm:items-center">
        <DocsLink gatewayName={gateway.name} implemented={true} />
        <Button
          onClick={onSave}
          disabled={saving}
          data-testid={`gateways-${gateway.name}-save`}
        >
          {saving ? "Saving…" : "Save"}
        </Button>
      </div>
    </section>
  );
}

// StubGatewayRow renders the muted "coming soon" treatment — single
// row with display name, badge, capability chips inline, no
// affordances. The lower opacity + condensed layout signals that
// these are placeholder protocols without screaming "not done yet"
// at every visit.
function StubGatewayRow({ gateway }: { gateway: GatewayInfo }) {
  return (
    <section
      className="flex flex-col gap-2 rounded-lg border border-dashed border-input/60 p-3 opacity-70 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
      data-testid={`gateways-${gateway.name}-section`}
      aria-labelledby={`gateways-${gateway.name}-heading`}
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <h2
            id={`gateways-${gateway.name}-heading`}
            className="text-sm font-medium"
          >
            {gateway.displayName}
          </h2>
          <ImplementationBadge implemented={false} />
        </div>
        <p className="mt-0.5 text-xs text-muted-foreground">
          {gateway.description}
        </p>
        <div className="mt-1.5">
          <CapabilityChips
            gatewayName={gateway.name}
            capabilities={gateway.capabilities}
            muted={true}
          />
        </div>
      </div>
      <div className="flex items-center gap-3 self-start sm:self-center">
        {/* Stubs still surface the canonical "no toggle" placeholder */}
        {/* — the spec relies on the em-dash to mark unimplemented */}
        {/* affordance positions, and the test pins the testid. */}
        <span
          className="hidden text-sm text-muted-foreground sm:inline"
          data-testid={`gateways-${gateway.name}-no-toggle`}
          aria-label="Enable toggle hidden for unimplemented gateway"
        >
          &mdash;
        </span>
        <span
          className="text-xs text-muted-foreground italic"
          data-testid={`gateways-${gateway.name}-status`}
        >
          not implemented yet
        </span>
      </div>
    </section>
  );
}

// StatusPill is v1.11.0.21's simplification of the v1.9.0d
// StatusBlock. The old block listed running flag + active connections
// + last activity + total requests + listen address in one prose
// run-on; the operator's takeaway is binary: is the gateway up, and
// when did something last touch it. The pill carries those two
// signals; the rest moves into a tooltip-style title attribute for
// power users who hover over it.
function StatusPill({
  gatewayName,
  status,
}: {
  gatewayName: string;
  status: GatewayStatus;
}) {
  const lastActivity = status.lastActivity
    ? humanRelative(status.lastActivity)
    : null;

  if (!status.running) {
    return (
      <span
        className="inline-flex items-center gap-1 rounded-full bg-destructive/10 px-2 py-0.5 text-xs font-medium text-destructive"
        data-testid={`gateways-${gatewayName}-status`}
        title="Gateway is not currently accepting connections"
      >
        <span aria-hidden="true" className="size-1.5 rounded-full bg-destructive" />
        Stopped
      </span>
    );
  }

  const titleParts: string[] = ["Running"];
  if (status.activeConnections !== undefined) {
    titleParts.push(
      `${status.activeConnections} active connection${status.activeConnections === 1 ? "" : "s"}`,
    );
  }
  if (status.totalRequests !== undefined) {
    titleParts.push(`${status.totalRequests} total requests`);
  }

  return (
    <span
      className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:text-emerald-400"
      data-testid={`gateways-${gatewayName}-status`}
      title={titleParts.join(" · ")}
    >
      <span aria-hidden="true" className="size-1.5 rounded-full bg-emerald-500" />
      Active
      {lastActivity && (
        <span className="font-normal text-muted-foreground">
          &middot; {lastActivity}
        </span>
      )}
    </span>
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
//
// v1.11.0.21 added the `muted` prop so stub rows can render the chips
// at lower visual weight (no border, no monospace) while implemented
// rows keep the bordered + monospace treatment that pairs well with
// the rest of the hero panel.
function CapabilityChips({
  gatewayName,
  capabilities,
  muted = false,
}: {
  gatewayName: string;
  capabilities: GatewayCapabilities;
  muted?: boolean;
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
      <p
        className="text-xs text-muted-foreground italic"
        data-testid={`gateways-${gatewayName}-capabilities`}
      >
        capabilities unspecified
      </p>
    );
  }

  if (muted) {
    return (
      <div
        className="flex flex-wrap items-center gap-1 text-[11px] text-muted-foreground"
        data-testid={`gateways-${gatewayName}-capabilities`}
      >
        {chips.map((c, i) => (
          <span key={c}>
            {c}
            {i < chips.length - 1 && (
              <span aria-hidden="true" className="mx-1 opacity-60">
                &middot;
              </span>
            )}
          </span>
        ))}
      </div>
    );
  }

  return (
    <div
      className="flex flex-wrap items-center gap-1.5 text-xs"
      data-testid={`gateways-${gatewayName}-capabilities`}
    >
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

// ConnectHints renders the collapsed per-platform mount instructions.
// Today only WebDAV has bespoke per-platform copy; other implemented
// gateways will gain their own clauses here as they land. Stubs
// don't get connect hints &mdash; the coming-soon badge + capability
// chips carry the message and no docs link is rendered (v1.11.0.20).
//
// v1.11.0.21 tightened each platform clause to a single imperative
// sentence — the goal is "what do I tap" not "what is this protocol".
function ConnectHints({ gatewayName }: { gatewayName: string }) {
  if (gatewayName === "webdav") {
    return (
      <details className="text-sm">
        <summary className="cursor-pointer font-medium">
          Connect from your platform
        </summary>
        <ul className="mt-3 space-y-1.5 text-sm">
          <li>
            <strong>macOS:</strong> Finder &rsaquo; &#8984;K &rsaquo;
            paste mount URL.
          </li>
          <li>
            <strong>Windows:</strong> Explorer &rsaquo; Map network drive
            &rsaquo; paste mount URL.
          </li>
          <li>
            <strong>Linux:</strong> Nautilus &rsaquo; Connect to Server
            &rsaquo; replace <code>https://</code> with <code>davs://</code>.
          </li>
          <li>
            <strong>iOS:</strong> Files &rsaquo; Browse &rsaquo; &hellip;
            &rsaquo; Connect to Server.
          </li>
        </ul>
        <p className="mt-2 text-xs text-muted-foreground">
          Sign in with basement credentials, or a{" "}
          <Link to="/admin/service-accounts" className="underline">
            service-account key
          </Link>{" "}
          (<code>BMNT&hellip;</code>) for automation.
        </p>
      </details>
    );
  }
  return null;
}

// DocsLink renders the per-protocol docs pointer. Only invoked for
// implemented gateways (per v1.11.0.20: stubs no longer link to a
// tracking doc, since version-committing copy is being scrubbed
// across the project — see feedback_dont_announce_v2). The
// `implemented` prop is retained for callsite clarity even though
// the unimplemented branch is now unreachable.
//
// v1.11.0.21 shortened the label from "<NAME> integration guide ->"
// to "Docs ->" — the row's own title already carries the protocol
// name, so repeating it in the link is chrome we can drop.
function DocsLink({
  gatewayName,
  implemented: _implemented,
}: {
  gatewayName: string;
  implemented: boolean;
}) {
  const href = `/docs/integrations/${gatewayName}.md`;
  return (
    <a
      href={href}
      className="text-xs font-medium text-primary underline-offset-4 hover:underline"
      target="_blank"
      rel="noreferrer"
      data-testid={`gateways-${gatewayName}-docs`}
    >
      Docs &rarr;
    </a>
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

// v1.13.1: Skin manager with three-control layout (default skin, user overridable toggle, available skins list)
function SkinsManager() {
  const queryClient = useQueryClient();
  const [skins, setSkins] = useState<Array<{ name: string; displayName: string; swatch?: string; builtIn?: boolean }>>([]);
  const [loading, setLoading] = useState(true);
  const caps = useOrgCapabilities();

  useEffect(() => {
    fetchSkins();
  }, []);

  async function fetchSkins() {
    try {
      // v1.13.14: was hitting /api/v1/skins (user endpoint), which is
      // a different shape and doesn't include built-ins in the way the
      // admin list expects — result was an empty installed list. Admin
      // page must call /api/v1/admin/skins which returns the full set
      // with policy info.
      const res = await fetch("/api/v1/admin/skins", { credentials: "include" });
      if (!res.ok) {
        throw new Error(`/admin/skins ${res.status}`);
      }
      const data = await res.json();
      if (Array.isArray(data)) {
        setSkins(data as Skin[]);
      }
    } catch (error) {
      toast.error("Failed to load skins");
    } finally {
      setLoading(false);
    }
  }

  const [defaultSkin, setDefaultSkin] = useState<string>("basement-default");
  const [userOverridable, setUserOverridable] = useState<boolean>(false);
  const [allowedUserSkins, setAllowedUserSkins] = useState<string[]>([]);

  // Sync local state with query data when org-capabilities refetches after skin change
  useEffect(() => {
    if (caps.data) {
      setDefaultSkin(caps.data.activeSkin || "basement-default");
      setUserOverridable(caps.data.userOverridableSkin || false);
      setAllowedUserSkins(caps.data.allowedUserSkins || []);
    }
  }, [caps.data]);

  // Debounced save on change - only trigger after initial mount to avoid race conditions
  const mountedRef = useRef(false);
  
  useEffect(() => {
    if (!mountedRef.current) {
      mountedRef.current = true;
      return;
    }
    
    const timer = setTimeout(async () => {
      try {
        // @ts-ignore - admin/system path not in generated types yet
        await client.PATCH("/admin/system", {
          body: {
            activeSkin: defaultSkin,
            userOverridableSkin: userOverridable,
            allowedUserSkins: allowedUserSkins,
          },
        });
        toast.success("Saved");
        queryClient.invalidateQueries({ queryKey: ["org-capabilities"] });
        queryClient.invalidateQueries({ queryKey: ["skin", "active"] });
      } catch (error) {
        toast.error("Failed to save skin settings");
      }
    }, 500);

    return () => clearTimeout(timer);
  }, [defaultSkin, userOverridable, allowedUserSkins, queryClient]);

  if (loading) {
    return <Card><CardContent className="text-sm text-muted-foreground">Loading skins...</CardContent></Card>;
  }

  const availableForSelection = userOverridable && allowedUserSkins.length > 0 ? allowedUserSkins : skins.map(s => s.name);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Skins</CardTitle>
        <CardDescription>
          Configure the default skin and user override settings. Changes apply immediately.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Default Skin */}
        <div className="space-y-2">
          <label className="text-sm font-medium" htmlFor="default-skin-select">Default Skin</label>
          <Select value={defaultSkin} onValueChange={setDefaultSkin}>
            <SelectTrigger id="default-skin-select">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {skins.map((skin) => (
                <SelectItem key={skin.name} value={skin.name}>
                  <div className="flex items-center gap-2">
                    <div 
                      className="w-4 h-4 rounded-full" 
                      style={{ backgroundColor: skin.swatch || "#000" }} 
                    />
                    {skin.displayName}
                  </div>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {/* User Overridable */}
        <div className="space-y-2">
          <label className="text-sm font-medium flex items-center gap-2">
            <Switch 
              checked={userOverridable} 
              onCheckedChange={setUserOverridable}
            />
            User Overridable
          </label>
        </div>

        {/* Available Skins (hidden when overridable=false) */}
        {userOverridable && (
          <div className="space-y-2">
            <label className="text-sm font-medium">Available Skins</label>
            <div className="grid grid-cols-2 gap-2">
              {skins.map((skin) => (
                <label key={skin.name} className="flex items-center gap-2 text-sm p-2 rounded-md border hover:bg-muted/50 cursor-pointer">
                  <Checkbox 
                    checked={availableForSelection.includes(skin.name)}
                    onCheckedChange={(checked) => {
                      if (checked && userOverridable) {
                        setAllowedUserSkins([...allowedUserSkins, skin.name]);
                      } else if (!checked) {
                        setAllowedUserSkins(allowedUserSkins.filter(n => n !== skin.name));
                      }
                    }}
                  />
                  <div 
                    className="w-4 h-4 rounded-full" 
                    style={{ backgroundColor: skin.swatch || "#000" }} 
                  />
                  {skin.displayName}
                </label>
              ))}
            </div>
          </div>
        )}

        {/* Installed Skins List */}
        <section className="space-y-3 pt-4 border-t">
          <h2 className="text-sm font-medium">Installed Skins</h2>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {skins.map((skin) => (
              <div key={skin.name} className="rounded-lg border p-3 flex items-center justify-between">
                <div className="flex items-center gap-2 min-w-0">
                  <div 
                    className="w-6 h-6 rounded-full shrink-0" 
                    style={{ backgroundColor: skin.swatch || "#000" }} 
                  />
                  <div className="min-w-0">
                    <p className="text-sm font-medium truncate">{skin.displayName}</p>
                    {skin.builtIn ? (
                      <Badge variant="secondary" className="text-[10px]">Built-in</Badge>
                    ) : (
                      <Badge variant="outline" className="text-[10px]">Custom</Badge>
                    )}
                  </div>
                </div>
                {!skin.builtIn && (
                  <Button 
                    size="sm" 
                    variant="ghost" 
                    className="text-destructive hover:text-destructive h-8 px-2"
                    onClick={() => {
                      if (confirm(`Delete skin "${skin.displayName}"? This cannot be undone.`)) {
                        // @ts-ignore - API types not generated yet
                        client.DELETE("/admin/skins/{id}", { params: { path: { id: skin.name } } })
                          .then(() => {
                            toast.success(`Deleted ${skin.displayName}`);
                            fetchSkins();
                          })
                          .catch(() => toast.error("Failed to delete skin"));
                      }
                    }}
                  >
                    Delete
                  </Button>
                )}
              </div>
            ))}
          </div>
        </section>
      </CardContent>
    </Card>
  );
}
 