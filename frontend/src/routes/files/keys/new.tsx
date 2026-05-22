import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useCreateUserKey, useDriverDefaults, type DriverDefaults } from "@/shared/api/queries";

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
  const { data: driverDefaults } = useDriverDefaults();

  const [alias, setAlias] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [region, setRegion] = useState("us-east-1");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [regionTouched, setRegionTouched] = useState(false);
  const [showCommonEndpoints, setShowCommonEndpoints] = useState(false);

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
  const applyExample = (d: DriverDefaults) => {
    if (d.s3Endpoint) setEndpoint(d.s3Endpoint);
    if (d.regionLabel) {
      setRegion(d.regionLabel);
      setRegionTouched(true);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMessage(null);

    if (!alias.trim() || !endpoint.trim() || !accessKeyId.trim() || !secretKey) {
      setErrorMessage("All required fields must be filled in.");
      return;
    }

    try {
      await createKey.mutateAsync({
        alias: alias.trim(),
        endpoint: endpoint.trim(),
        accessKeyId: accessKeyId.trim(),
        secretKey,
        region: region.trim() || "us-east-1",
      });
      navigate({ to: "/files" });
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : "Add key failed");
    }
  };

  const canSubmit =
    !!alias.trim() && !!endpoint.trim() && !!accessKeyId.trim() && !!secretKey && !submitting;

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

      {driverDefaults && driverDefaults.length > 0 && (
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

      <form onSubmit={handleSubmit} className="space-y-4 max-w-2xl">
        <div className="space-y-2">
          <label htmlFor="alias" className="text-sm font-medium">
            Key alias *
          </label>
          <input
            id="alias"
            type="text"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
            placeholder="home"
            autoComplete="off"
            className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
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
            onChange={(e) => handleEndpointChange(e.target.value)}
            placeholder="https://s3.example.com"
            autoComplete="off"
            className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
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
              onChange={(e) => setAccessKeyId(e.target.value)}
              placeholder="GK..."
              autoComplete="off"
              spellCheck={false}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
          </div>

          <div className="space-y-2">
            <label htmlFor="secretKey" className="text-sm font-medium">
              Secret Access Key *
            </label>
            <input
              id="secretKey"
              type="password"
              value={secretKey}
              onChange={(e) => setSecretKey(e.target.value)}
              placeholder={"••••••••"}
              autoComplete="new-password"
              spellCheck={false}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
            />
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

        {errorMessage && (
          <div
            data-testid="add-key-error"
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
    </div>
  );
}

export default AddKeyPage;
