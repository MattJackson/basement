import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useUpdateCluster } from "@/shared/api/mutations";
import type { components } from "@/shared/api/types.gen";

type Driver = "garage-v1" | "garage" | "aws-s3";

const DRIVER_OPTIONS: { value: Driver; label: string }[] = [
  { value: "garage-v1", label: "Garage v1" },
  { value: "garage", label: "Garage" },
  { value: "aws-s3", label: "AWS S3" },
  // { value: "minio", label: "MinIO" }, // lands with DRV.MINIO.A
];

const COLOR_SWATCHES = ["#C9874B", "#10B981", "#3B82F6", "#EF4444", "#8B5CF6", "#F59E0B", "#EC4899", "#06B6D4"];

interface EditClusterDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  cluster: components["schemas"]["Connection"];
}

export function EditClusterDialog({ open, onOpenChange, cluster }: EditClusterDialogProps) {
  const [label, setLabel] = useState(cluster.label);
  const [color, setColor] = useState(cluster.color ?? "#C9874B");
  const [driver, setDriver] = useState<Driver>(cluster.driver as Driver);
  const [adminUrl, setAdminUrl] = useState(cluster.config.adminUrl || "");
  const [adminToken, setAdminToken] = useState(cluster.config.adminToken || "");
  const [s3Url, setS3Url] = useState(cluster.config.s3Url || "");
  const [s3Region, setS3Region] = useState(cluster.config.s3Region ?? "garage");
  const [s3AccessKey, setS3AccessKey] = useState(cluster.config.s3AccessKey || "");
  const [s3SecretKey, setS3SecretKey] = useState(cluster.config.s3SecretKey || "");

  // Reset form to the cluster's current values whenever the dialog
  // (re)opens. Done inline rather than via useEffect to avoid the
  // cascading-render lint — matches the pattern in DeleteBucketConfirm.
  const [lastSeenKey, setLastSeenKey] = useState<string | null>(null);
  const key = open ? `${cluster.id}::open` : null;
  if (key !== null && key !== lastSeenKey) {
    setLastSeenKey(key);
    setLabel(cluster.label);
    setColor(cluster.color ?? "#C9874B");
    setDriver(cluster.driver as Driver);
    setAdminUrl(cluster.config.adminUrl || "");
    setAdminToken(cluster.config.adminToken || "");
    setS3Url(cluster.config.s3Url || "");
    setS3Region(cluster.config.s3Region ?? "garage");
    setS3AccessKey(cluster.config.s3AccessKey || "");
    setS3SecretKey(cluster.config.s3SecretKey || "");
  }
  if (key === null && lastSeenKey !== null) setLastSeenKey(null);

  const updateCluster = useUpdateCluster(cluster.id);

  const handleSave = () => {
    if (!label.trim() || label.length < 1 || label.length > 64) return;

    const config: Record<string, string> = {};
    
    if (driver === "garage-v1" || driver === "garage") {
      config.adminUrl = adminUrl;
      config.adminToken = adminToken;
      if (s3Url) config.s3Url = s3Url;
      config.s3Region = s3Region;
      if (s3AccessKey) config.s3AccessKey = s3AccessKey;
      if (s3SecretKey) config.s3SecretKey = s3SecretKey;
    } else if (driver === "aws-s3") {
      config.region = s3Region;
      config.accessKey = s3AccessKey;
      config.secretKey = s3SecretKey;
      if (adminUrl) config.endpoint = adminUrl;
    }
    // MinIO branch lands with DRV.MINIO.A.

    const update: components["schemas"]["ConnectionUpdate"] = {
      label: label.trim(),
      driver,
      config,
      color: color || undefined,
    };

    updateCluster.mutate(update, {
      onSuccess: () => {
        onOpenChange(false);
      },
    });
  };

  const isSaveDisabled = !label.trim() || label.length < 1 || label.length > 64 || updateCluster.isPending;

  return (
    <Dialog open={open} onOpenChange={(isOpen) => {
      if (!isOpen) onOpenChange(false);
    }}>
      <DialogContent className="sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>Edit cluster</DialogTitle>
          <DialogDescription>
            Update the cluster label, color, or configuration.
          </DialogDescription>
        </DialogHeader>

        {updateCluster.error && (
          <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {String(updateCluster.error.message ?? "Failed to update cluster")}
          </div>
        )}

        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Label htmlFor="label">Label *</Label>
            <Input
              id="label"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              disabled={updateCluster.isPending}
              autoFocus
            />
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

          <div className="grid gap-2">
            <Label>Driver</Label>
            <div className="mt-2">
              <span className="text-sm text-muted-foreground">{DRIVER_OPTIONS.find(o => o.value === driver)?.label}</span>
            </div>
            <p className="text-xs text-muted-foreground">
              Driver cannot be changed. To use a different backend, delete and recreate the cluster.
            </p>
          </div>

          {(driver === "garage-v1" || driver === "garage") && (
            <div className="space-y-4 rounded-lg border p-4 bg-muted/30">
              <h4 className="text-sm font-medium">Garage Configuration</h4>
              <div className="grid gap-3">
                <div className="grid gap-2">
                  <Label htmlFor="adminUrl">Admin URL *</Label>
                  <Input
                    id="adminUrl"
                    value={adminUrl}
                    onChange={(e) => setAdminUrl(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="adminToken">Admin Token *</Label>
                  <Input
                    id="adminToken"
                    value={adminToken}
                    onChange={(e) => setAdminToken(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3Url">S3 URL (optional)</Label>
                  <Input
                    id="s3Url"
                    value={s3Url}
                    onChange={(e) => setS3Url(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3Region">S3 Region</Label>
                  <Input
                    id="s3Region"
                    value={s3Region}
                    onChange={(e) => setS3Region(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3AccessKey">S3 Access Key (optional)</Label>
                  <Input
                    id="s3AccessKey"
                    value={s3AccessKey}
                    onChange={(e) => setS3AccessKey(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3SecretKey">S3 Secret Key (optional)</Label>
                  <Input
                    id="s3SecretKey"
                    type="password"
                    value={s3SecretKey}
                    onChange={(e) => setS3SecretKey(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
              </div>
            </div>
          )}

          {driver === "aws-s3" && (
            <div className="space-y-4 rounded-lg border p-4 bg-muted/30">
              <h4 className="text-sm font-medium">AWS S3 Configuration</h4>
              <div className="grid gap-3">
                <div className="grid gap-2">
                  <Label htmlFor="awsRegion">Region *</Label>
                  <Input
                    id="awsRegion"
                    value={s3Region}
                    onChange={(e) => setS3Region(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="awsAccessKey">Access Key *</Label>
                  <Input
                    id="awsAccessKey"
                    value={s3AccessKey}
                    onChange={(e) => setS3AccessKey(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="awsSecretKey">Secret Key *</Label>
                  <Input
                    id="awsSecretKey"
                    type="password"
                    value={s3SecretKey}
                    onChange={(e) => setS3SecretKey(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="awsEndpoint">Endpoint (optional)</Label>
                  <Input
                    id="awsEndpoint"
                    value={adminUrl}
                    onChange={(e) => setAdminUrl(e.target.value)}
                    disabled={updateCluster.isPending}
                  />
                </div>
              </div>
            </div>
          )}

          {/* MinIO form section lands with DRV.MINIO.A in v0.3.0. */}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={updateCluster.isPending}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={isSaveDisabled}>
            {updateCluster.isPending ? "Saving…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
