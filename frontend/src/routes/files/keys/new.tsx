import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useMemo, useState } from "react";
import {
  useBulkCreateUserKeys,
  useCreateUserKey,
  useDriverDefaults,
  type BulkCreateUserKeyResponse,
  type DriverDefaults,
} from "@/shared/api/queries";
import {
  parseBulkKeys,
  type BulkKeyFormat,
  type ParsedKeyRow,
} from "@/shared/lib/bulkKeyParse";

// detectDriverFromEndpoint returns the heuristic match for an endpoint
// URL the operator just pasted. Used to auto-suggest the region label
// without overwriting any value the operator has already typed.
//
// v1.3.0b: pattern-only detection — no network probe. Matches the
// hints we curate in internal/driver/defaults.go:
//   - "amazonaws.com" → aws-s3
//   - hostnames containing "garage" → garage-v1
//   - hostnames containing "minio" → minio
// Returns null when nothing matches; caller treats null as "leave
// region alone".
function detectDriverFromEndpoint(endpoint: string): string | null {
  if (!endpoint) return null;
  const lower = endpoint.toLowerCase();
  if (lower.includes("amazonaws.com")) return "aws-s3";
  if (lower.includes("garage")) return "garage-v1";
  if (lower.includes("minio")) return "minio";
  return null;
}

// endpointHostIsIP mirrors the server-side driver.EndpointHostIsIP
// helper (Go) so the Advanced toggle can disable virtual-host
// addressing when there's no DNS hostname to attach wildcard records
// to. Matches IPv4 (with optional port) and bracketed IPv6 in URLs.
// Returns false on empty / unparseable input — the toggle just stays
// enabled rather than misleadingly disabled.
//
// Why mirror server logic here: the smart default lives server-side
// anyway (BuildS3Client coerces IP-host virtual-host requests back to
// path-style), but disabling the toggle in the FE gives operators
// targeted feedback BEFORE submit — "this can't work with this
// endpoint" instead of "your toggle was silently ignored."
function endpointHostIsIP(endpoint: string): boolean {
  if (!endpoint) return false;
  let host: string;
  try {
    host = new URL(endpoint).hostname;
  } catch {
    return false;
  }
  if (!host) return false;
  // IPv6 in URL.hostname has the brackets stripped already.
  if (host.includes(":")) {
    // bracketed IPv6 — at least one colon means it's not a DNS label.
    return true;
  }
  // IPv4: four dot-separated 0-255 octets.
  return /^(\d{1,3}\.){3}\d{1,3}$/.test(host);
}

// Canonical "Add a key" form (ADR-0002, v1.2.0d). Replaces the old
// "Connect a region" copy. The legacy URL /files/regions/new now
// loader-redirects here.
//
// Mental shift per v1.2.0d: each access key is the primary user noun.
// A user may register multiple keys against the same endpoint with
// different aliases ("Work S3", "Personal S3") — the backend allows it
// as long as the (endpoint, alias) tuple is unique per user.
//
// The form fields are unchanged from the v1.1.0c version — alias,
// endpoint, accessKeyId, secret, region label — only the labels +
// page chrome shift to the "key" framing.
export const Route = createFileRoute("/files/keys/new")({
  component: AddKeyPage,
});

function AddKeyPage() {
  const navigate = useNavigate();
  const createKey = useCreateUserKey();
  const bulkCreate = useBulkCreateUserKeys();
  const { data: driverDefaults } = useDriverDefaults();

  // v1.3.0d: "Bulk import" toggle. When on, the form swaps to a
  // paste-area that accepts CSV / TSV / aws-cli credentials format,
  // shows a parsed-preview table, and submits via the bulk endpoint.
  const [bulkMode, setBulkMode] = useState(false);

  const [alias, setAlias] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [region, setRegion] = useState("us-east-1");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [regionTouched, setRegionTouched] = useState(false);
  // v1.10.0.1 — surfaced field-level validation. Pre-fix the submit
  // button was simply `disabled` when any required field was blank, so
  // an operator clicking Submit on a pristine form got no feedback at
  // all (smoke flagged this as "blank submit silently does nothing").
  // Now we let the submit attempt fire, record which required fields
  // are still empty, render inline `role="alert"` messages + flip
  // `aria-invalid` on the offending inputs, and keep the user on the
  // page until they fix things.
  const [fieldErrors, setFieldErrors] = useState<{
    alias?: string;
    endpoint?: string;
    accessKeyId?: string;
    secretKey?: string;
  }>({});
  const [showCommonEndpoints, setShowCommonEndpoints] = useState(false);
  // v1.3.0c: per-region S3 addressing toggle. Default off (path-style)
  // — the safe choice for every backend (Garage, IP-addressed MinIO,
  // self-hosted S3 with no wildcard DNS). Operators with wildcard DNS
  // for the endpoint host flip it on either via the AWS row in
  // "Common endpoints" (auto-checks because AWS prefers virtual-host
  // for tooling compat) or via the Advanced expandable below.
  const [virtualHost, setVirtualHost] = useState(false);
  const [showAdvanced, setShowAdvanced] = useState(false);

  // Smart default mirror: if the endpoint host is an IP literal,
  // virtual-host addressing can't possibly work (no wildcard DNS for
  // a bare IP) so we disable the toggle AND force the underlying state
  // back to path-style. Server-side BuildS3Client also enforces this,
  // but disabling client-side gives the operator a clear inline hint
  // before they submit.
  const endpointIsIP = endpointHostIsIP(endpoint);
  const effectiveVirtualHost = virtualHost && !endpointIsIP;

  const submitting = createKey.isPending;

  // v1.3.0b: auto-suggest the region label when the operator pastes an
  // endpoint that matches a known driver pattern — but ONLY if the
  // operator hasn't already typed a custom region. Never overwrite
  // operator-entered values silently.
  const handleEndpointChange = (next: string) => {
    setEndpoint(next);
    if (regionTouched) return;
    const detected = detectDriverFromEndpoint(next);
    if (!detected) return;
    const match = driverDefaults?.find((d) => d.driver === detected);
    if (match?.regionLabel) {
      setRegion(match.regionLabel);
    }
  };

  // applyExample fills both the endpoint AND region from a curated
  // DriverDefaults row. Triggered ONLY by an explicit "Use this" click
  // — per cycle constraint, never auto-overwrite. Marks the region as
  // touched so subsequent endpoint typing doesn't unsettle it.
  //
  // v1.3.0c: AWS rows auto-check the virtual-host toggle because AWS
  // S3 prefers virtual-host addressing for S3-tool compatibility
  // (boto3 / aws-cli default). Self-hosted backends (Garage, MinIO)
  // keep the toggle off — path-style is the safe choice for them.
  const applyExample = (d: DriverDefaults) => {
    if (d.s3Endpoint) setEndpoint(d.s3Endpoint);
    if (d.regionLabel) {
      setRegion(d.regionLabel);
      setRegionTouched(true);
    }
    if (d.driver === "aws-s3") {
      setVirtualHost(true);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMessage(null);

    // v1.10.0.1 — collect per-field errors first so we can render
    // inline cues next to each offending input. Anything blank gets a
    // "Required" message; the submit short-circuits if anything is
    // blank.
    const nextFieldErrors: {
      alias?: string;
      endpoint?: string;
      accessKeyId?: string;
      secretKey?: string;
    } = {};
    if (!alias.trim()) nextFieldErrors.alias = "Required";
    if (!endpoint.trim()) nextFieldErrors.endpoint = "Required";
    if (!accessKeyId.trim()) nextFieldErrors.accessKeyId = "Required";
    if (!secretKey) nextFieldErrors.secretKey = "Required";
    setFieldErrors(nextFieldErrors);
    if (Object.keys(nextFieldErrors).length > 0) {
      setErrorMessage("Please fill in the required fields.");
      return;
    }

    try {
      await createKey.mutateAsync({
        alias: alias.trim(),
        endpoint: endpoint.trim(),
        accessKeyId: accessKeyId.trim(),
        secretKey,
        region: region.trim() || "us-east-1",
        // v1.3.0c: only send the toggle when virtual-host was actually
        // checked AND the endpoint can support it. Omitting the field
        // hits the server's default ("path"), matching pre-v1.3.0c
        // behaviour for every key created without flipping the toggle.
        addressingStyle: effectiveVirtualHost ? "virtual_host" : "path",
      });
      navigate({ to: "/files" });
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : "Add key failed");
    }
  };

  // v1.10.0.1 — the button is no longer disabled-on-blank; we want
  // the operator to be able to click Submit and see inline validation
  // fire, not stare at a greyed-out button with no explanation. The
  // only disabled state now is during an in-flight mutation.
  const canSubmit = !submitting;

  return (
    <div className="space-y-6">
      <header>
        <a href="/files" className="text-sm text-muted-foreground hover:underline">
          {"← Back to my regions"}
        </a>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight mt-2">Add a key</h1>
        <p className="text-sm text-muted-foreground mt-1">
          Your cluster admin handed you an S3 key. Add the endpoint plus the key here, and every
          bucket that key can see will show up in your files. You can add multiple keys against the
          same endpoint — give each one a distinct alias.
        </p>
      </header>

      {/* v1.3.0d — Bulk-import toggle. */}
      <div className="max-w-2xl flex items-center gap-3">
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={bulkMode}
            onChange={(e) => setBulkMode(e.target.checked)}
            data-testid="bulk-mode-toggle"
          />
          <span className="font-medium">Bulk import (paste multiple keys)</span>
        </label>
        <span className="text-xs text-muted-foreground">
          Accepts CSV, TSV, or aws-cli credentials profiles
        </span>
      </div>

      {bulkMode && (
        <BulkImportPanel
          onDone={() => navigate({ to: "/files" })}
          submit={async (rows) => bulkCreate.mutateAsync(rows)}
          submitting={bulkCreate.isPending}
        />
      )}

      {!bulkMode && driverDefaults && driverDefaults.length > 0 && (
        <div className="max-w-2xl rounded-md border bg-card">
          <button
            type="button"
            onClick={() => setShowCommonEndpoints((v) => !v)}
            className="flex w-full items-center justify-between gap-2 px-4 py-3 text-left text-sm font-medium hover:bg-muted/40"
            aria-expanded={showCommonEndpoints}
            aria-controls="common-endpoints-panel"
          >
            <span>Common endpoints</span>
            <span aria-hidden="true" className="text-muted-foreground">
              {showCommonEndpoints ? "−" : "+"}
            </span>
          </button>
          {showCommonEndpoints && (
            <div id="common-endpoints-panel" className="border-t px-4 py-3 space-y-2">
              <p className="text-xs text-muted-foreground">
                Quick-fill the endpoint + region for a known driver. Replace the host with yours.
              </p>
              <ul className="divide-y">
                {driverDefaults
                  .filter((d) => !!d.s3Endpoint)
                  .map((d) => (
                    <li
                      key={d.driver}
                      className="flex flex-wrap items-center justify-between gap-2 py-2"
                    >
                      <div className="min-w-0 flex-1">
                        <div className="text-sm font-medium">{d.displayName}</div>
                        <div className="font-mono text-xs text-muted-foreground break-all">
                          {d.s3Endpoint}
                          {d.regionLabel ? ` · region: ${d.regionLabel}` : ""}
                        </div>
                        {d.s3EndpointHint && (
                          <div className="text-xs text-muted-foreground">{d.s3EndpointHint}</div>
                        )}
                      </div>
                      <button
                        type="button"
                        onClick={() => applyExample(d)}
                        className="rounded-md border px-2 py-1 text-xs font-medium hover:bg-muted/40"
                        data-testid={`use-endpoint-${d.driver}`}
                      >
                        Use this
                      </button>
                    </li>
                  ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {!bulkMode && (
      <form onSubmit={handleSubmit} className="space-y-4 max-w-2xl">
        <div className="space-y-2">
          <label htmlFor="alias" className="text-sm font-medium">
            Key alias *
          </label>
          <input
            id="alias"
            type="text"
            value={alias}
            onChange={(e) => {
              setAlias(e.target.value);
              if (fieldErrors.alias && e.target.value.trim()) {
                setFieldErrors((prev) => ({ ...prev, alias: undefined }));
              }
            }}
            placeholder="home"
            autoComplete="off"
            aria-invalid={fieldErrors.alias ? true : undefined}
            aria-describedby={fieldErrors.alias ? "alias-error" : undefined}
            className={`w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring ${fieldErrors.alias ? "border-red-500" : ""}`}
          />
          {fieldErrors.alias && (
            <p
              id="alias-error"
              role="alert"
              className="text-xs text-red-600 dark:text-red-400"
            >
              {fieldErrors.alias}
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            A short label so you can tell this key apart from any others on your keychain
            (e.g. &quot;Work S3&quot;, &quot;Personal S3&quot;).
          </p>
        </div>

        <div className="space-y-2">
          <label htmlFor="endpoint" className="text-sm font-medium">
            S3 endpoint URL *
          </label>
          <input
            id="endpoint"
            type="url"
            value={endpoint}
            onChange={(e) => {
              handleEndpointChange(e.target.value);
              if (fieldErrors.endpoint && e.target.value.trim()) {
                setFieldErrors((prev) => ({ ...prev, endpoint: undefined }));
              }
            }}
            placeholder="https://s3.example.com"
            autoComplete="off"
            aria-invalid={fieldErrors.endpoint ? true : undefined}
            aria-describedby={fieldErrors.endpoint ? "endpoint-error" : undefined}
            className={`w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring ${fieldErrors.endpoint ? "border-red-500" : ""}`}
          />
          {fieldErrors.endpoint && (
            <p
              id="endpoint-error"
              role="alert"
              className="text-xs text-red-600 dark:text-red-400"
            >
              {fieldErrors.endpoint}
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            Paste an endpoint with &quot;amazonaws.com&quot;, &quot;garage&quot;, or &quot;minio&quot; in the
            URL and we&apos;ll suggest a region label.
          </p>
        </div>

        <div className="grid gap-4 md:grid-cols-2">
          <div className="space-y-2">
            <label htmlFor="accessKeyId" className="text-sm font-medium">
              Access Key ID *
            </label>
            <input
              id="accessKeyId"
              type="text"
              value={accessKeyId}
              onChange={(e) => {
                setAccessKeyId(e.target.value);
                if (fieldErrors.accessKeyId && e.target.value.trim()) {
                  setFieldErrors((prev) => ({ ...prev, accessKeyId: undefined }));
                }
              }}
              placeholder="GK..."
              autoComplete="off"
              spellCheck={false}
              aria-invalid={fieldErrors.accessKeyId ? true : undefined}
              aria-describedby={fieldErrors.accessKeyId ? "accessKeyId-error" : undefined}
              className={`w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring ${fieldErrors.accessKeyId ? "border-red-500" : ""}`}
            />
            {fieldErrors.accessKeyId && (
              <p
                id="accessKeyId-error"
                role="alert"
                className="text-xs text-red-600 dark:text-red-400"
              >
                {fieldErrors.accessKeyId}
              </p>
            )}
          </div>

          <div className="space-y-2">
            <label htmlFor="secretKey" className="text-sm font-medium">
              Secret Access Key *
            </label>
            <input
              id="secretKey"
              type="password"
              value={secretKey}
              onChange={(e) => {
                setSecretKey(e.target.value);
                if (fieldErrors.secretKey && e.target.value) {
                  setFieldErrors((prev) => ({ ...prev, secretKey: undefined }));
                }
              }}
              placeholder={"••••••••"}
              autoComplete="new-password"
              spellCheck={false}
              aria-invalid={fieldErrors.secretKey ? true : undefined}
              aria-describedby={fieldErrors.secretKey ? "secretKey-error" : undefined}
              className={`w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring ${fieldErrors.secretKey ? "border-red-500" : ""}`}
            />
            {fieldErrors.secretKey && (
              <p
                id="secretKey-error"
                role="alert"
                className="text-xs text-red-600 dark:text-red-400"
              >
                {fieldErrors.secretKey}
              </p>
            )}
          </div>
        </div>

        <div className="space-y-2 max-w-xs">
          <label htmlFor="region" className="text-sm font-medium">
            S3 region
          </label>
          <input
            id="region"
            type="text"
            value={region}
            onChange={(e) => {
              setRegion(e.target.value);
              setRegionTouched(true);
            }}
            placeholder="us-east-1"
            autoComplete="off"
            spellCheck={false}
            className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
          <p className="text-xs text-muted-foreground">
            Defaults to us-east-1 if your endpoint doesn{"’"}t care.
          </p>
        </div>

        <div className="rounded-md border bg-card">
          <button
            type="button"
            onClick={() => setShowAdvanced((v) => !v)}
            className="flex w-full items-center justify-between gap-2 px-4 py-3 text-left text-sm font-medium hover:bg-muted/40"
            aria-expanded={showAdvanced}
            aria-controls="advanced-panel"
            data-testid="toggle-advanced"
          >
            <span>Advanced</span>
            <span aria-hidden="true" className="text-muted-foreground">
              {showAdvanced ? "−" : "+"}
            </span>
          </button>
          {showAdvanced && (
            <div id="advanced-panel" className="border-t px-4 py-3 space-y-2">
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={effectiveVirtualHost}
                  disabled={endpointIsIP}
                  onChange={(e) => setVirtualHost(e.target.checked)}
                  className="mt-1"
                  data-testid="virtual-host-toggle"
                />
                <span>
                  <span className="font-medium">
                    Use virtual-host addressing
                  </span>
                  <span className="block text-xs text-muted-foreground">
                    {endpointIsIP
                      ? "Virtual-host requires a DNS hostname — disabled for IP endpoints."
                      : "Requires DNS for bucket subdomains (e.g. *.s3.example.com wildcard). Leave unchecked for self-hosted Garage / IP-addressed MinIO."}
                  </span>
                </span>
              </label>
            </div>
          )}
        </div>

        {errorMessage && (
          <div
            data-testid="add-key-error"
            role="alert"
            className="rounded-md border border-red-500/50 bg-red-500/5 px-3 py-2 text-sm text-red-700 dark:text-red-300"
          >
            {errorMessage}
          </div>
        )}

        <div className="flex items-center gap-3">
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {submitting ? "Adding..." : "Add key"}
          </button>
          <a
            href="/files"
            className="rounded-md px-3 py-2 text-sm text-muted-foreground hover:underline"
          >
            Cancel
          </a>
        </div>
      </form>
      )}
    </div>
  );
}

// BulkImportPanel — v1.3.0d. Renders a textarea + paste-time format
// detector + preview table + submit-to-bulk-endpoint. Per-row errors
// surface inline in the table; per-row server errors land on the same
// row after submit. The panel is intentionally separate from the
// single-key form because it lives behind a toggle — operators not
// using bulk import never render any of this.
function BulkImportPanel({
  onDone,
  submit,
  submitting,
}: {
  onDone: () => void;
  submit: (rows: Array<{
    alias: string;
    endpoint: string;
    accessKeyId: string;
    secretKey: string;
    region?: string;
    addressingStyle?: "path" | "virtual_host";
  }>) => Promise<BulkCreateUserKeyResponse>;
  submitting: boolean;
}) {
  const [raw, setRaw] = useState("");
  const [result, setResult] = useState<BulkCreateUserKeyResponse | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);

  // Parse the raw paste on every keystroke. Cheap (string operations
  // only), and gives the operator immediate format-detected feedback
  // + per-row validation in the preview table.
  const { format, rows } = useMemo<{ format: BulkKeyFormat; rows: ParsedKeyRow[] }>(
    () => parseBulkKeys(raw),
    [raw],
  );

  const validRowCount = rows.filter((r) => r.rowErrors.length === 0).length;
  const anyRowInvalid = rows.some((r) => r.rowErrors.length > 0);
  const canSubmit = !submitting && rows.length > 0 && !anyRowInvalid && !result;

  const handleSubmit = async () => {
    setSubmitError(null);
    try {
      const payload = rows.map((r) => ({
        alias: r.alias,
        endpoint: r.endpoint,
        accessKeyId: r.accessKeyId,
        secretKey: r.secretKey,
        region: r.region,
        addressingStyle: r.addressingStyle,
      }));
      const res = await submit(payload);
      setResult(res);
    } catch (e) {
      setSubmitError(e instanceof Error ? e.message : "Bulk import failed");
    }
  };

  return (
    <div className="max-w-3xl space-y-4">
      <div className="space-y-2">
        <label htmlFor="bulk-input" className="text-sm font-medium">
          Paste keys
        </label>
        <textarea
          id="bulk-input"
          value={raw}
          onChange={(e) => setRaw(e.target.value)}
          placeholder={`alias,endpoint,accessKeyId,secretKey,region
home,https://s3.example.com,GK...,SECRET,us-east-1

# or aws-cli credentials format:
[work]
aws_access_key_id = GK...
aws_secret_access_key = ...
endpoint_url = https://s3.example.com
region = us-east-1`}
          rows={10}
          spellCheck={false}
          className="w-full rounded-md border bg-background px-3 py-2 text-sm font-mono outline-none focus:ring-2 focus:ring-ring"
          data-testid="bulk-input"
        />
        <p className="text-xs text-muted-foreground">
          Detected format:{" "}
          <span data-testid="bulk-detected-format" className="font-medium">
            {format === "unknown" ? "—" : format}
          </span>
          {rows.length > 0 && (
            <>
              {" · "}
              <span data-testid="bulk-row-count">
                {validRowCount} of {rows.length} rows valid
              </span>
            </>
          )}
        </p>
      </div>

      {rows.length > 0 && (
        <div className="rounded-md border bg-card overflow-x-auto" data-testid="bulk-preview">
          <table className="min-w-full text-sm">
            <thead className="bg-muted/50">
              <tr>
                <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">#</th>
                <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">Alias</th>
                <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">Endpoint</th>
                <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">Access key</th>
                <th className="px-3 py-2 text-left text-xs font-medium uppercase text-muted-foreground">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.map((r, i) => {
                const serverErr = result?.errors.find((e) => e.index === i);
                const createdHit = result && !serverErr && i < rows.length;
                const status = serverErr
                  ? { kind: "error" as const, text: `${serverErr.error}: ${serverErr.message}` }
                  : r.rowErrors.length > 0
                  ? { kind: "error" as const, text: r.rowErrors.join("; ") }
                  : result && createdHit
                  ? { kind: "success" as const, text: "Created" }
                  : { kind: "ready" as const, text: "Ready" };
                return (
                  <tr key={i} data-testid={`bulk-row-${i}`}>
                    <td className="px-3 py-2 text-xs text-muted-foreground">{i + 1}</td>
                    <td className="px-3 py-2 font-medium">{r.alias || "—"}</td>
                    <td className="px-3 py-2 font-mono text-xs">{r.endpoint || "—"}</td>
                    <td className="px-3 py-2 font-mono text-xs">{r.accessKeyId || "—"}</td>
                    <td className="px-3 py-2 text-xs">
                      <span
                        className={
                          status.kind === "error"
                            ? "text-red-700 dark:text-red-300"
                            : status.kind === "success"
                            ? "text-green-700 dark:text-green-300"
                            : "text-muted-foreground"
                        }
                      >
                        {status.text}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {submitError && (
        <div
          data-testid="bulk-submit-error"
          className="rounded-md border border-red-500/50 bg-red-500/5 px-3 py-2 text-sm text-red-700 dark:text-red-300"
        >
          {submitError}
        </div>
      )}

      <div className="flex items-center gap-3">
        {!result ? (
          <button
            type="button"
            onClick={handleSubmit}
            disabled={!canSubmit}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
            data-testid="bulk-submit"
          >
            {submitting ? "Importing..." : `Import ${validRowCount} key${validRowCount === 1 ? "" : "s"}`}
          </button>
        ) : (
          <button
            type="button"
            onClick={onDone}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90"
            data-testid="bulk-done"
          >
            Done — back to my keys
          </button>
        )}
        {!result && (
          <a
            href="/files"
            className="rounded-md px-3 py-2 text-sm text-muted-foreground hover:underline"
          >
            Cancel
          </a>
        )}
      </div>
    </div>
  );
}

export default AddKeyPage;
