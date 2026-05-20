import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { useCreateCluster } from "@/shared/api/mutations";
import type { components } from "@/shared/api/types.gen";

type Driver = "garage-v1" | "garage" | "aws-s3";

const DRIVER_OPTIONS: { value: Driver; label: string }[] = [
  { value: "garage-v1", label: "Garage v1" },
  { value: "garage", label: "Garage" },
  { value: "aws-s3", label: "AWS S3" },
  // { value: "minio", label: "MinIO" }, // lands with DRV.MINIO.A in v0.3.0
];

const COLOR_SWATCHES = ["#C9874B", "#10B981", "#3B82F6", "#EF4444", "#8B5CF6", "#F59E0B", "#EC4899", "#06B6D4"];

interface AddClusterDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function AddClusterDialog({ open, onOpenChange }: AddClusterDialogProps) {
  const [label, setLabel] = useState("");
  const [color, setColor] = useState("#C9874B");
  const [driver, setDriver] = useState<Driver>("garage-v1");
  const [adminUrl, setAdminUrl] = useState("");
  const [adminToken, setAdminToken] = useState("");
  const [s3Url, setS3Url] = useState("");
  const [s3Region, setS3Region] = useState("garage");
  const [s3AccessKey, setS3AccessKey] = useState("");
  const [s3SecretKey, setS3SecretKey] = useState("");

  const createCluster = useCreateCluster();

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
    // MinIO branch lands with DRV.MINIO.A in v0.3.0.

    const spec: components["schemas"]["ConnectionSpec"] = {
      label: label.trim(),
      driver,
      config,
      color: color || undefined,
    };

    createCluster.mutate(spec, {
      onSuccess: () => {
        resetForm();
        onOpenChange(false);
      },
    });
  };

  const resetForm = () => {
    setLabel("");
    setColor("#C9874B");
    setDriver("garage-v1");
    setAdminUrl("");
    setAdminToken("");
    setS3Url("");
    setS3Region("garage");
    setS3AccessKey("");
    setS3SecretKey("");
  };

  const isSaveDisabled = !label.trim() || label.length < 1 || label.length > 64 || createCluster.isPending;

  return (
    <Dialog open={open} onOpenChange={(isOpen) => {
      if (!isOpen) resetForm();
      onOpenChange(isOpen);
    }}>
      <DialogContent className="sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>Add cluster</DialogTitle>
          <DialogDescription>
            Configure a new Garage or S3-compatible storage backend.
          </DialogDescription>
        </DialogHeader>

        {createCluster.error && (
          <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
            {String(createCluster.error.message ?? "Failed to create cluster")}
          </div>
        )}

        <div className="grid gap-4 py-4">
          <div className="grid gap-2">
            <Label htmlFor="label">Label *</Label>
            <Input
              id="label"
              placeholder="My Garage Cluster"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              disabled={createCluster.isPending}
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
            <Label>Driver *</Label>
            <RadioGroup
              value={driver}
              onValueChange={(v: string) => setDriver(v as Driver)}
              disabled={createCluster.isPending}
            >
              <div className="grid grid-cols-2 gap-3 mt-2">
                {DRIVER_OPTIONS.map((option) => (
                  <div key={option.value} className="flex items-center space-x-2 rounded-lg border p-3 hover:bg-muted/50 cursor-pointer">
                    <RadioGroupItem value={option.value} id={option.value} />
                    <Label htmlFor={option.value} className="font-normal cursor-pointer">
                      {option.label}
                    </Label>
                  </div>
                ))}
              </div>
            </RadioGroup>
          </div>

          {(driver === "garage-v1" || driver === "garage") && (
            <div className="space-y-4 rounded-lg border p-4 bg-muted/30">
              <h4 className="text-sm font-medium">Garage Configuration</h4>
              <div className="grid gap-3">
                <div className="grid gap-2">
                  <Label htmlFor="adminUrl">Admin URL *</Label>
                  <Input
                    id="adminUrl"
                    placeholder="https://garage.example.com:3971"
                    value={adminUrl}
                    onChange={(e) => setAdminUrl(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="adminToken">Admin Token *</Label>
                  <Input
                    id="adminToken"
                    placeholder="garage-admin-secret"
                    value={adminToken}
                    onChange={(e) => setAdminToken(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3Url">S3 URL (optional)</Label>
                  <Input
                    id="s3Url"
                    placeholder="https://s3.example.com:3972"
                    value={s3Url}
                    onChange={(e) => setS3Url(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3Region">S3 Region</Label>
                  <Input
                    id="s3Region"
                    value={s3Region}
                    onChange={(e) => setS3Region(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3AccessKey">S3 Access Key (optional)</Label>
                  <Input
                    id="s3AccessKey"
                    placeholder="AKIAIOSFODNN7EXAMPLE"
                    value={s3AccessKey}
                    onChange={(e) => setS3AccessKey(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="s3SecretKey">S3 Secret Key (optional)</Label>
                  <Input
                    id="s3SecretKey"
                    type="password"
                    placeholder="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
                    value={s3SecretKey}
                    onChange={(e) => setS3SecretKey(e.target.value)}
                    disabled={createCluster.isPending}
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
                    placeholder="us-east-1"
                    value={s3Region}
                    onChange={(e) => setS3Region(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="awsAccessKey">Access Key *</Label>
                  <Input
                    id="awsAccessKey"
                    placeholder="AKIAIOSFODNN7EXAMPLE"
                    value={s3AccessKey}
                    onChange={(e) => setS3AccessKey(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="awsSecretKey">Secret Key *</Label>
                  <Input
                    id="awsSecretKey"
                    type="password"
                    placeholder="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
                    value={s3SecretKey}
                    onChange={(e) => setS3SecretKey(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="awsEndpoint">Endpoint (optional)</Label>
                  <Input
                    id="awsEndpoint"
                    placeholder="https://s3.amazonaws.com"
                    value={adminUrl}
                    onChange={(e) => setAdminUrl(e.target.value)}
                    disabled={createCluster.isPending}
                  />
                </div>
              </div>
            </div>
          )}

          {/* MinIO form section lands with DRV.MINIO.A in v0.3.0. */}

          <div className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800">
            Note: Test connection is only available after saving the cluster. Use the detail page to test connectivity.
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={createCluster.isPending}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={isSaveDisabled}>
            {createCluster.isPending ? "Creating…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
