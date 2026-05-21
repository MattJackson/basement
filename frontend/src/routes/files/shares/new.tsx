import { useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Checkbox } from "@/components/ui/checkbox";
import { useCreateUserShare, useRevokeUserShare, useUserClusters, useUserClusterBuckets } from "@/shared/api/queries";

// NOTE: do NOT wrap in userPage() — parent layout (shares.tsx)
// already wraps the Outlet in UserShell. Double-wrap → double-header
// (caught visually on v0.9.0a screenshot pass).
export const Route = createFileRoute("/files/shares/new")({
  component: NewSharePage,
});

function NewSharePage() {
  const navigate = useNavigate();
  const { data: clusters } = useUserClusters();

  const [connectionId, setConnectionId] = useState("");
  const [bucketId, setBucketId] = useState("");
  const [shareType, setShareType] = useState<"prefix" | "object">("prefix");
  const [prefix, setPrefix] = useState("");
  const [key, setKey] = useState("");
  const [expiresIn, setExpiresIn] = useState<"24h" | "7d" | "30d" | "never">("7d");
  const [downloadLimit, setDownloadLimit] = useState<string>("");
  const [hasPassword, setHasPassword] = useState(false);
  const [password, setPassword] = useState("");

  const { data: buckets } = useUserClusterBuckets(connectionId);
  const createShare = useCreateUserShare();
  const revokeShare = useRevokeUserShare();

  const handleSave = () => {
    if (!connectionId || !bucketId) return;
    const payload: {
      connectionId: string;
      bucketId: string;
      prefix?: string;
      key?: string;
      expiresAt?: string;
      downloadLimit?: number;
      password?: string;
    } = { connectionId, bucketId };

    if (shareType === "prefix") payload.prefix = prefix || "";
    else payload.key = key || "";

    const now = new Date();
    if (expiresIn === "24h") payload.expiresAt = new Date(now.getTime() + 24 * 3600 * 1000).toISOString();
    else if (expiresIn === "7d") payload.expiresAt = new Date(now.getTime() + 7 * 24 * 3600 * 1000).toISOString();
    else if (expiresIn === "30d") payload.expiresAt = new Date(now.getTime() + 30 * 24 * 3600 * 1000).toISOString();

    if (downloadLimit.trim()) {
      const lim = parseInt(downloadLimit, 10);
      if (!isNaN(lim) && lim > 0) payload.downloadLimit = lim;
    }
    if (hasPassword && password.length >= 8) payload.password = password;

    createShare.mutate(payload);
  };

  const generatedLink = createShare.data ? `${window.location.origin}/share/${createShare.data.token}` : null;
  const copyLink = async () => { if (generatedLink) await navigator.clipboard.writeText(generatedLink); };
  const handleRevoke = () => {
    if (!createShare.data) return;
    if (!confirm("Revoke this share? Recipients will lose access.")) return;
    revokeShare.mutate(createShare.data.token, { onSuccess: () => navigate({ to: "/files/shares" }) });
  };

  const isCreateDisabled = !connectionId || !bucketId || (shareType === "prefix" && !prefix) || (shareType === "object" && !key) || (hasPassword && password.length < 8) || createShare.isPending;

  // Post-creation: show generated link + revoke option
  if (generatedLink) {
    return (
      <div className="space-y-6 max-w-3xl">
        <BackLink />
        <header>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Share link created</h1>
          <p className="text-sm text-muted-foreground mt-1">This URL is shown only once. Copy it now.</p>
        </header>

        <section className="rounded-lg border bg-card p-6 space-y-4">
          <div>
            <Label className="text-xs uppercase tracking-wider text-muted-foreground">Share URL</Label>
            <div className="flex items-center gap-2 mt-2">
              <code className="flex-1 rounded bg-muted px-3 py-2 text-sm font-mono break-all">{generatedLink}</code>
              <Button variant="outline" size="sm" onClick={copyLink}>Copy</Button>
            </div>
          </div>

          {createShare.data?.passwordHash && (
            <div className="rounded-md border border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950/30 p-3 text-sm">
              <p className="font-medium">Password-protected</p>
              <p className="text-muted-foreground mt-1">Send the password separately to recipients.</p>
            </div>
          )}
          {createShare.data?.expiresAt && (
            <p className="text-sm text-muted-foreground">Expires {new Date(createShare.data.expiresAt).toLocaleString()}.</p>
          )}
          {createShare.data?.downloadLimit && (
            <p className="text-sm text-muted-foreground">Capped at {createShare.data.downloadLimit} downloads.</p>
          )}
        </section>

        <div className="flex items-center gap-2">
          <Button onClick={() => navigate({ to: "/files/shares" })}>Done</Button>
          <Button variant="outline" onClick={handleRevoke} disabled={revokeShare.isPending}>
            {revokeShare.isPending ? "Revoking…" : "Revoke this link"}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6 max-w-3xl">
      <BackLink />

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">New share</h1>
          <p className="text-sm text-muted-foreground mt-1">Create a public link to a bucket prefix or single object.</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => navigate({ to: "/files/shares" })} disabled={createShare.isPending}>Cancel</Button>
          <Button onClick={handleSave} disabled={isCreateDisabled}>
            {createShare.isPending ? "Creating…" : "Create link"}
          </Button>
        </div>
      </header>

      {createShare.error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {String(createShare.error.message ?? "Failed to create share")}
        </div>
      )}

      <section className="space-y-4 rounded-lg border bg-card p-6">
        <h2 className="text-sm font-medium text-muted-foreground">Target</h2>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid gap-2">
            <Label htmlFor="cluster">Cluster *</Label>
            <select
              id="cluster"
              value={connectionId}
              onChange={(e) => { setConnectionId(e.target.value); setBucketId(""); }}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm"
            >
              <option value="">— pick a cluster —</option>
              {clusters?.map((c) => (<option key={c.id} value={c.id}>{c.label}</option>))}
            </select>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="bucket">Bucket *</Label>
            <select
              id="bucket"
              value={bucketId}
              onChange={(e) => setBucketId(e.target.value)}
              disabled={!connectionId}
              className="w-full rounded-md border bg-background px-3 py-2 text-sm disabled:opacity-50"
            >
              <option value="">— pick a bucket —</option>
              {buckets?.map((b) => (<option key={b.id} value={b.id}>{b.aliases?.[0] || b.id.slice(0,12)}</option>))}
            </select>
          </div>
        </div>

        <RadioGroup value={shareType} onValueChange={(v) => setShareType(v as "prefix" | "object")} className="grid gap-2">
          <label className={`flex items-center gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${shareType === "prefix" ? "border-ring" : ""}`}>
            <RadioGroupItem value="prefix" />
            <span className="text-sm font-medium">Share a folder (prefix)</span>
          </label>
          <label className={`flex items-center gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${shareType === "object" ? "border-ring" : ""}`}>
            <RadioGroupItem value="object" />
            <span className="text-sm font-medium">Share a single object</span>
          </label>
        </RadioGroup>

        {shareType === "prefix" ? (
          <div className="grid gap-2">
            <Label htmlFor="prefix">Prefix path *</Label>
            <Input id="prefix" value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="path/to/folder/" />
          </div>
        ) : (
          <div className="grid gap-2">
            <Label htmlFor="key">Object key *</Label>
            <Input id="key" value={key} onChange={(e) => setKey(e.target.value)} placeholder="path/to/file.txt" />
          </div>
        )}
      </section>

      <section className="space-y-4 rounded-lg border bg-card p-6">
        <h2 className="text-sm font-medium text-muted-foreground">Options</h2>
        <div className="grid gap-2">
          <Label>Expiration</Label>
          <RadioGroup value={expiresIn} onValueChange={(v) => setExpiresIn(v as typeof expiresIn)} className="grid gap-2 sm:grid-cols-2">
            {[["24h","24 hours"],["7d","7 days"],["30d","30 days"],["never","Never expire"]].map(([v,label]) => (
              <label key={v} className={`flex items-center gap-2 rounded-md border p-2 cursor-pointer hover:bg-muted/30 ${expiresIn === v ? "border-ring" : ""}`}>
                <RadioGroupItem value={v} />
                <span className="text-sm">{label}</span>
              </label>
            ))}
          </RadioGroup>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="grid gap-2">
            <Label htmlFor="downloadLimit">Download limit (optional)</Label>
            <Input id="downloadLimit" type="number" min="1" value={downloadLimit} onChange={(e) => setDownloadLimit(e.target.value)} placeholder="unlimited" />
          </div>
          <div>
            <label className="flex items-center gap-2 cursor-pointer mt-7">
              <Checkbox checked={hasPassword} onCheckedChange={(c) => setHasPassword(c === true)} />
              <span className="text-sm font-medium">Protect with password</span>
            </label>
          </div>
        </div>

        {hasPassword && (
          <div className="grid gap-2">
            <Label htmlFor="password">Password (8+ characters)</Label>
            <Input id="password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="••••••••" />
          </div>
        )}
      </section>
    </div>
  );
}

function BackLink() {
  return (
    <Link to="/files/shares" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
      ← Back to shares
    </Link>
  );
}
