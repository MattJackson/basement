import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { userPage } from "@/shared/layout/userPage";

// Connect-a-bucket route (ADR-0001, v0.9.0e). Replaces the old
// /files/clusters/new "Add cluster" page on the user side.
//
// The mental shift per ADR-0001: users don't add clusters — they bring
// S3 credentials for a single bucket they're already entitled to.
// basement resolves or creates a Connection behind the scenes and
// stores the user's per-bucket key as a Grant.
export const Route = createFileRoute("/files/buckets/connect")({
  component: userPage(ConnectBucketPage),
});

type ConnectBody = {
  alias: string;
  s3Endpoint: string;
  accessKeyId: string;
  secretKey: string;
  region: string;
};

type ConnectResponse = {
  connectionId: string;
  bucketId: string;
  alias: string;
};

type ApiError = {
  error?: { code?: string; message?: string };
};

function ConnectBucketPage() {
  const navigate = useNavigate();

  const [alias, setAlias] = useState("");
  const [s3Endpoint, setS3Endpoint] = useState("");
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [region, setRegion] = useState("us-east-1");

  const [submitting, setSubmitting] = useState(false);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMessage(null);

    if (!alias.trim() || !s3Endpoint.trim() || !accessKeyId.trim() || !secretKey.trim()) {
      setErrorMessage("All required fields must be filled in.");
      return;
    }

    setSubmitting(true);
    try {
      const body: ConnectBody = {
        alias: alias.trim(),
        s3Endpoint: s3Endpoint.trim(),
        accessKeyId: accessKeyId.trim(),
        secretKey: secretKey,
        region: region.trim() || "us-east-1",
      };

      const res = await fetch("/api/v1/user/buckets/connect", {
        method: "POST",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        body: JSON.stringify(body),
      });

      if (!res.ok) {
        let parsed: ApiError | undefined;
        try {
          parsed = (await res.json()) as ApiError;
        } catch {
          // fall through to generic message
        }
        const code = parsed?.error?.code ?? `HTTP_${res.status}`;
        const msg = parsed?.error?.message ?? `Connect failed (HTTP ${res.status})`;
        setErrorMessage(`${code}: ${msg}`);
        return;
      }

      // 201 with { connectionId, bucketId, alias } — we don't use it
      // beyond the navigate, but parsing validates the contract.
      (await res.json()) as ConnectResponse;
      navigate({ to: "/files" });
    } catch (err) {
      setErrorMessage(err instanceof Error ? err.message : "Connect failed");
    } finally {
      setSubmitting(false);
    }
  };

  const canSubmit =
    !!alias.trim() && !!s3Endpoint.trim() && !!accessKeyId.trim() && !!secretKey.trim() && !submitting;

  return (
    <div className="space-y-6">
      <header>
        <a href="/files" className="text-sm text-muted-foreground hover:underline">
          {"← Back to my buckets"}
        </a>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight mt-2">Connect a bucket</h1>
        <p className="text-sm text-muted-foreground mt-1">
          You have S3 credentials for a bucket. Add it here and you{"’"}ll see it in your files.
        </p>
      </header>

      <form onSubmit={handleSubmit} className="space-y-4 max-w-2xl">
        <div className="space-y-2">
          <label htmlFor="alias" className="text-sm font-medium">
            Bucket alias *
          </label>
          <input
            id="alias"
            type="text"
            value={alias}
            onChange={(e) => setAlias(e.target.value)}
            placeholder="lsi"
            autoComplete="off"
            className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
          <p className="text-xs text-muted-foreground">
            The bucket name your operator uses {"—"} shown in your files list.
          </p>
        </div>

        <div className="space-y-2">
          <label htmlFor="s3Endpoint" className="text-sm font-medium">
            S3 endpoint URL *
          </label>
          <input
            id="s3Endpoint"
            type="url"
            value={s3Endpoint}
            onChange={(e) => setS3Endpoint(e.target.value)}
            placeholder="https://s3.example.com"
            autoComplete="off"
            className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
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
              placeholder="••••••••"
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
            onChange={(e) => setRegion(e.target.value)}
            placeholder="us-east-1"
            autoComplete="off"
            spellCheck={false}
            className="w-full rounded-md border bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
          />
          <p className="text-xs text-muted-foreground">Defaults to us-east-1 if your endpoint doesn{"’"}t care.</p>
        </div>

        {errorMessage && (
          <div className="rounded-md border border-red-500/50 bg-red-500/5 px-3 py-2 text-sm text-red-700 dark:text-red-300">
            {errorMessage}
          </div>
        )}

        <div className="flex items-center gap-3">
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          >
            {submitting ? "Connecting..." : "Connect bucket"}
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

export default ConnectBucketPage;
