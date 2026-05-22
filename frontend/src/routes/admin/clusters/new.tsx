import { useState } from "react";
import { createFileRoute, useNavigate, Link } from "@tanstack/react-router";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { useCreateCluster } from "@/shared/api/mutations";
import { useDriverDefaults, type DriverDefaults } from "@/shared/api/queries";
import { adminPage } from "@/shared/layout/adminPage";
import type { components } from "@/shared/api/types.gen";

type Driver = "garage-v1" | "garage" | "aws-s3" | "minio";

const DRIVER_OPTIONS: { value: Driver; label: string; hint: string }[] = [
  { value: "garage-v1", label: "Garage v1", hint: "Garage 1.x admin API. Operator's prod target." },
  { value: "garage", label: "Garage v2", hint: "Garage 2.x admin API (upstream beta; first UI to support)." },
  { value: "aws-s3", label: "AWS S3", hint: "Native AWS S3 with IAM credentials." },
  { value: "minio", label: "MinIO / OpenMaxIO", hint: "S3-compatible local + OpenMaxIO fork." },
];

const COLOR_SWATCHES = ["#C9874B", "#10B981", "#3B82F6", "#EF4444", "#8B5CF6", "#F59E0B", "#EC4899", "#06B6D4"];

// Operator design rule: popups for 1-2 fields max. Add Cluster has 9
// fields → page, not dialog. Old AddClusterDialog stays for the user
// persona (/files/clusters/new) for now; admin path moved here.
export const Route = createFileRoute("/admin/clusters/new")({
  component: adminPage(AddClusterPage),
});

function AddClusterPage() {
  const navigate = useNavigate();
  const createCluster = useCreateCluster();
  const { data: driverDefaults } = useDriverDefaults();

  const [label, setLabel] = useState("");
  const [color, setColor] = useState("#C9874B");
  const [driver, setDriver] = useState<Driver>("garage-v1");
  const [adminUrl, setAdminUrl] = useState("");
  const [adminToken, setAdminToken] = useState("");
  const [s3Url, setS3Url] = useState("");
  const [s3Region, setS3Region] = useState("garage");
  const [s3AccessKey, setS3AccessKey] = useState("");
  const [s3SecretKey, setS3SecretKey] = useState("");

  // v1.3.0b: pluck the EndpointDefaults for the selected driver so the
  // form can render placeholder text + one-line hints instead of empty
  // inputs the operator has to remember the format of. Falls back to
  // an undefined entry when the catalogue hasn't loaded yet — the JSX
  // tolerates that via the `?.` chain on every field.
  const defaultsFor = (d: Driver): DriverDefaults | undefined =>
    driverDefaults?.find((entry) => entry.driver === d);
  const dDefaults = defaultsFor(driver);

  const handleSave = () => {
    if (!label.trim() || label.length < 1 || label.length > 64) return;

    const config: Record<string, string> = {};
    if (driver === "garage-v1" || driver === "garage") {
      config.admin_url = adminUrl;
      config.admin_token = adminToken;
      if (s3Url) config.s3_endpoint = s3Url;
      // ADR-0001 (v0.9.0d): per-user S3 creds (access_key_id +
      // secret_key) no longer live on the cluster — they're Grants
      // minted per user × bucket. Add form intentionally omits them.
    } else if (driver === "aws-s3") {
      config.region = s3Region;
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
      label: label.trim(),
      driver,
      config,
      color: color || undefined,
    };

    createCluster.mutate(spec, {
      onSuccess: (created) => {
        navigate({ to: "/admin/clusters/$cid", params: { cid: created.id } });
      },
    });
  };

  // ADR-0001 (v0.9.0d): the s3_endpoint+key tri-state guard is gone
  // because user-tier creds (access_key_id, secret_key) moved out of
  // the cluster Connection and into per-user Grants. The cluster can
  // now safely store just the s3_endpoint as a presign destination.
  const isSaveDisabled =
    !label.trim() ||
    label.length < 1 ||
    label.length > 64 ||
    createCluster.isPending;

  return (
    <div className="space-y-6 max-w-3xl">
      <Link to="/admin/clusters" className="inline-flex items-center gap-1 text-sm font-medium hover:underline text-muted-foreground">
        ← Back to clusters
      </Link>

      <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
        <div>
          <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">Add cluster</h1>
          <p className="text-sm text-muted-foreground mt-1">
            Register a new storage backend. Pick a driver, fill in the connection details.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" onClick={() => navigate({ to: "/admin/clusters" })} disabled={createCluster.isPending}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={isSaveDisabled}>
            {createCluster.isPending ? "Creating…" : "Create cluster"}
          </Button>
        </div>
      </header>

      {createCluster.error && (
        <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {String(createCluster.error.message ?? "Failed to create cluster")}
        </div>
      )}

      <section className="space-y-4 rounded-lg border bg-card p-6">
        <h2 className="text-sm font-medium text-muted-foreground">General</h2>
        <div className="grid gap-2">
          <Label htmlFor="label">Label *</Label>
          <Input id="label" value={label} onChange={(e) => setLabel(e.target.value)} disabled={createCluster.isPending} placeholder="e.g. work-prod, home-lab" autoFocus />
        </div>
        <div className="grid gap-2">
          <Label>Color</Label>
          <div className="flex flex-wrap gap-2">
            {COLOR_SWATCHES.map((swatch) => (
              <button
                key={swatch}
                type="button"
                onClick={() => setColor(swatch)}
                className={`h-8 w-8 rounded-full border-2 ${color === swatch ? "border-ring" : "border-transparent"} hover:opacity-80`}
                style={{ backgroundColor: swatch }}
                aria-label={`Select color ${swatch}`}
              />
            ))}
          </div>
        </div>
      </section>

      <section className="space-y-4 rounded-lg border bg-card p-6">
        <h2 className="text-sm font-medium text-muted-foreground">Driver</h2>
        <RadioGroup value={driver} onValueChange={(v) => setDriver(v as Driver)} className="grid gap-2 sm:grid-cols-2">
          {DRIVER_OPTIONS.map((opt) => (
            <label key={opt.value} className={`flex items-start gap-3 rounded-md border p-3 cursor-pointer hover:bg-muted/30 ${driver === opt.value ? "border-ring" : ""}`}>
              <RadioGroupItem value={opt.value} className="mt-1" />
              <div>
                <div className="font-medium text-sm">{opt.label}</div>
                <p className="text-xs text-muted-foreground mt-0.5">{opt.hint}</p>
              </div>
            </label>
          ))}
        </RadioGroup>
      </section>

      {(driver === "garage-v1" || driver === "garage") && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">Garage admin</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="adminUrl">Admin URL *</Label>
              <Input id="adminUrl" value={adminUrl} onChange={(e) => setAdminUrl(e.target.value)} disabled={createCluster.isPending} placeholder={dDefaults?.adminUrl || "http://garage-host:3903"} />
              {dDefaults?.adminUrlHint && (
                <p className="text-xs text-muted-foreground">{dDefaults.adminUrlHint}</p>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="adminToken">Admin Token *</Label>
              <Input id="adminToken" type="password" value={adminToken} onChange={(e) => setAdminToken(e.target.value)} disabled={createCluster.isPending} />
              {dDefaults?.secretUrl && (
                <a href={dDefaults.secretUrl} target="_blank" rel="noreferrer" className="text-xs text-muted-foreground underline hover:no-underline">
                  Where to find your admin token →
                </a>
              )}
            </div>
          </div>
        </section>
      )}

      {(driver === "garage-v1" || driver === "garage") && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <div>
            <h2 className="text-sm font-medium text-muted-foreground">Garage S3 plane (optional)</h2>
            <p className="text-xs text-muted-foreground mt-1">
              S3 endpoint URL where Garage's S3 API listens (default :3902). Required for presign + user-side object browsing. <strong>Per-user S3 credentials now live as Grants — see <Link to="/admin/users" className="underline hover:no-underline">/admin/users</Link>.</strong>
            </p>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="s3Url">S3 URL</Label>
              <Input id="s3Url" value={s3Url} onChange={(e) => setS3Url(e.target.value)} disabled={createCluster.isPending} placeholder={dDefaults?.s3Endpoint || "http://garage-host:3902"} />
              {dDefaults?.s3EndpointHint && (
                <p className="text-xs text-muted-foreground">{dDefaults.s3EndpointHint}</p>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="s3Region">S3 Region</Label>
              <Input id="s3Region" value={s3Region} onChange={(e) => setS3Region(e.target.value)} disabled={createCluster.isPending} placeholder={dDefaults?.regionLabel || "garage"} />
            </div>
          </div>
        </section>
      )}

      {driver === "aws-s3" && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">AWS S3</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="awsRegion">Region *</Label>
              <Input id="awsRegion" value={s3Region} onChange={(e) => setS3Region(e.target.value)} disabled={createCluster.isPending} placeholder={dDefaults?.regionLabel || "us-east-1"} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="awsAccessKey">Access Key *</Label>
              <Input id="awsAccessKey" value={s3AccessKey} onChange={(e) => setS3AccessKey(e.target.value)} disabled={createCluster.isPending} placeholder="AKIA…" />
              {dDefaults?.secretUrl && (
                <a href={dDefaults.secretUrl} target="_blank" rel="noreferrer" className="text-xs text-muted-foreground underline hover:no-underline">
                  Where to find your access key →
                </a>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="awsSecretKey">Secret Key *</Label>
              <Input id="awsSecretKey" type="password" value={s3SecretKey} onChange={(e) => setS3SecretKey(e.target.value)} disabled={createCluster.isPending} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="awsEndpoint">Endpoint (optional)</Label>
              <Input id="awsEndpoint" value={adminUrl} onChange={(e) => setAdminUrl(e.target.value)} disabled={createCluster.isPending} placeholder={dDefaults?.s3Endpoint || "https://s3.us-east-1.amazonaws.com"} />
              {dDefaults?.s3EndpointHint && (
                <p className="text-xs text-muted-foreground">{dDefaults.s3EndpointHint}</p>
              )}
            </div>
          </div>
        </section>
      )}

      {driver === "minio" && (
        <section className="space-y-4 rounded-lg border bg-card p-6">
          <h2 className="text-sm font-medium text-muted-foreground">MinIO / OpenMaxIO</h2>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="minioEndpoint">Endpoint *</Label>
              <Input id="minioEndpoint" value={adminUrl} onChange={(e) => setAdminUrl(e.target.value)} disabled={createCluster.isPending} placeholder={dDefaults?.s3Endpoint || "http://minio-host:9000"} />
              {dDefaults?.s3EndpointHint && (
                <p className="text-xs text-muted-foreground">{dDefaults.s3EndpointHint}</p>
              )}
              {dDefaults?.adminUrlHint && (
                <p className="text-xs text-muted-foreground">{dDefaults.adminUrlHint}</p>
              )}
            </div>
            <div className="grid gap-2">
              <Label htmlFor="minioRegion">Region</Label>
              <Input id="minioRegion" value={s3Region} onChange={(e) => setS3Region(e.target.value)} disabled={createCluster.isPending} placeholder={dDefaults?.regionLabel || "us-east-1"} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="minioAccessKey">Access Key *</Label>
              <Input id="minioAccessKey" value={s3AccessKey} onChange={(e) => setS3AccessKey(e.target.value)} disabled={createCluster.isPending} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="minioSecretKey">Secret Key *</Label>
              <Input id="minioSecretKey" type="password" value={s3SecretKey} onChange={(e) => setS3SecretKey(e.target.value)} disabled={createCluster.isPending} />
            </div>
          </div>
        </section>
      )}
    </div>
  );
}
