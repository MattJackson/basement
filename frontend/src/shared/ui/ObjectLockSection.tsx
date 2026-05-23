import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useObjectLockConfig,
  usePutObjectLockConfig,
  type ObjectLockMode,
  type VersioningStatusResponse,
} from "@/shared/api/queries";

/**
 * ObjectLockSection renders the per-bucket Object Lock card on the
 * bucket detail page (v1.10.0c). Layered on top of versioning per
 * the S3 spec — Object Lock REQUIRES versioning enabled, so this
 * card surfaces three distinct branches:
 *
 *   - capabilities.supported === false → static "Not supported by
 *     this backend driver" notice with controls disabled. Garage
 *     v1 / v2 land here; AWS S3 + MinIO advertise true.
 *
 *   - supported, but versioning not enabled → notice telling the
 *     operator to enable versioning first. Controls disabled.
 *
 *   - supported + versioning enabled → full editor.
 *
 * Once Object Lock is enabled on a bucket, the S3 contract is
 * one-way: it cannot be disabled. The UI surfaces this distinctly
 * — once enabled, the disable affordance is gone and the operator
 * can only adjust default retention.
 */
export function ObjectLockSection({
  regionId,
  bucket,
  versioningState,
}: {
  regionId: string;
  bucket: string;
  versioningState: VersioningStatusResponse | undefined;
}) {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useObjectLockConfig(regionId, bucket);
  const putMutation = usePutObjectLockConfig(regionId, bucket);

  // Local form state. Initialised from the backend whenever the
  // returned config changes — e.g. after a successful PUT the
  // mutation echoes the new state back and the form snaps to it.
  const [defaultRetentionEnabled, setDefaultRetentionEnabled] = useState(false);
  const [mode, setMode] = useState<ObjectLockMode>("GOVERNANCE");
  const [days, setDays] = useState<number>(30);

  useEffect(() => {
    if (!data) return;
    if (data.defaultRetention) {
      setDefaultRetentionEnabled(true);
      setMode(data.defaultRetention.mode);
      const date = new Date(data.defaultRetention.retainUntilDate);
      const ms = date.getTime() - Date.now();
      const computed = Math.max(1, Math.round(ms / (24 * 60 * 60 * 1000)));
      setDays(computed);
    } else {
      setDefaultRetentionEnabled(false);
    }
  }, [data]);

  if (isLoading) {
    return (
      <section className="space-y-3" data-testid="object-lock-section">
        <SectionHeader />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-muted-foreground">Loading Object Lock state…</p>
        </div>
      </section>
    );
  }

  if (error || !data) {
    return (
      <section className="space-y-3" data-testid="object-lock-section">
        <SectionHeader />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-destructive">
            Couldn't load Object Lock state.
          </p>
        </div>
      </section>
    );
  }

  if (!data.supported) {
    return (
      <section className="space-y-3" data-testid="object-lock-section">
        <SectionHeader />
        <div
          className="rounded-lg border bg-card p-6 flex items-center gap-3"
          data-testid="object-lock-unsupported"
        >
          <Badge variant="secondary" className="text-xs">
            Unsupported
          </Badge>
          <p className="text-sm text-muted-foreground">
            Object Lock isn't supported by this backend driver.
          </p>
        </div>
      </section>
    );
  }

  const versioningEnabled =
    !!versioningState?.supported && versioningState.status === "enabled";

  // Object Lock REQUIRES versioning per the S3 spec. Surface this
  // distinctly so the operator knows what to do — don't render a
  // disabled enable button without explanation.
  if (!versioningEnabled) {
    return (
      <section className="space-y-3" data-testid="object-lock-section">
        <SectionHeader />
        <div
          className="rounded-lg border bg-card p-6 flex items-center gap-3"
          data-testid="object-lock-needs-versioning"
        >
          <Badge variant="outline" className="text-xs">
            Versioning required
          </Badge>
          <p className="text-sm text-muted-foreground">
            Object Lock requires versioning to be enabled. Enable
            versioning above first, then return here.
          </p>
        </div>
      </section>
    );
  }

  const handleSave = () => {
    const retention =
      defaultRetentionEnabled && days > 0
        ? {
            mode,
            retainUntilDate: new Date(
              Date.now() + days * 24 * 60 * 60 * 1000,
            ).toISOString(),
          }
        : undefined;
    putMutation.mutate(
      {
        enabled: true,
        defaultRetention: retention,
      },
      {
        onSuccess: () => {
          queryClient.invalidateQueries({
            queryKey: ["user", "regions", regionId, "buckets", bucket, "object-lock"],
          });
        },
      },
    );
  };

  return (
    <section className="space-y-3" data-testid="object-lock-section">
      <SectionHeader />
      <div className="rounded-lg border bg-card p-6 space-y-5">
        <div className="flex items-center gap-3">
          <CurrentStatusBadge enabled={data.enabled} />
          {data.enabled && (
            <p className="text-xs text-muted-foreground">
              Once enabled, Object Lock cannot be disabled (S3 contract).
            </p>
          )}
        </div>

        <div className="space-y-3">
          <label className="flex items-center gap-2 text-sm font-medium">
            <input
              type="checkbox"
              data-testid="object-lock-default-retention-toggle"
              className="h-4 w-4"
              checked={defaultRetentionEnabled}
              onChange={(e) => setDefaultRetentionEnabled(e.target.checked)}
              disabled={putMutation.isPending}
            />
            Apply a default retention to new objects
          </label>

          {defaultRetentionEnabled && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pl-6">
              <div className="space-y-1">
                <label
                  htmlFor="object-lock-mode-select"
                  className="text-xs font-medium text-muted-foreground"
                >
                  Mode
                </label>
                <select
                  id="object-lock-mode-select"
                  data-testid="object-lock-mode-select"
                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
                  value={mode}
                  onChange={(e) => setMode(e.target.value as ObjectLockMode)}
                  disabled={putMutation.isPending}
                >
                  <option value="GOVERNANCE">Governance</option>
                  <option value="COMPLIANCE">Compliance</option>
                </select>
                <p className="text-xs text-muted-foreground">
                  {mode === "COMPLIANCE"
                    ? "Strict: cannot be reduced or removed until expiry, even by the bucket owner."
                    : "Override allowed with bypass permission."}
                </p>
              </div>
              <div className="space-y-1">
                <label
                  htmlFor="object-lock-days-input"
                  className="text-xs font-medium text-muted-foreground"
                >
                  Days
                </label>
                <Input
                  id="object-lock-days-input"
                  data-testid="object-lock-days-input"
                  type="number"
                  min={1}
                  value={days}
                  onChange={(e) => setDays(Math.max(1, parseInt(e.target.value, 10) || 1))}
                  disabled={putMutation.isPending}
                />
                <p className="text-xs text-muted-foreground">
                  New objects get a retention period of this many days from PUT time.
                </p>
              </div>
            </div>
          )}
        </div>

        <div className="flex items-center gap-3">
          <Button
            type="button"
            size="sm"
            onClick={handleSave}
            disabled={putMutation.isPending}
            data-testid="object-lock-save"
          >
            {putMutation.isPending
              ? "Saving…"
              : data.enabled
                ? "Update default retention"
                : "Enable Object Lock"}
          </Button>
          {putMutation.isSuccess && !putMutation.isPending && (
            <span
              className="text-xs text-muted-foreground"
              data-testid="object-lock-saved-notice"
            >
              Saved.
            </span>
          )}
        </div>

        <p className="text-xs text-muted-foreground">
          Object Lock prevents object versions from being deleted or
          overwritten for a configurable period. Use Governance for
          flexible workflows; Compliance for regulator-mandated
          retention.
        </p>

        {putMutation.isError && (
          <p
            className="text-xs text-destructive"
            data-testid="object-lock-error"
          >
            {putMutation.error?.message ?? "Failed to update Object Lock."}
          </p>
        )}
      </div>
    </section>
  );
}

function SectionHeader() {
  return (
    <div className="flex items-center justify-between">
      <h2 className="text-sm font-medium text-muted-foreground">Object Lock</h2>
    </div>
  );
}

function CurrentStatusBadge({ enabled }: { enabled: boolean }) {
  if (enabled) {
    return (
      <Badge variant="default" className="text-xs" data-testid="object-lock-status-badge">
        Enabled
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-xs" data-testid="object-lock-status-badge">
      Disabled
    </Badge>
  );
}
