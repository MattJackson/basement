import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  useBucketVersioning,
  usePutBucketVersioning,
  type VersioningStatus,
} from "@/shared/api/queries";

/**
 * VersioningSection renders the per-bucket versioning card on the
 * bucket detail page (v1.10.0b). Three branches:
 *
 *   - capabilities.supported === false → static "Not supported by this
 *     backend driver" notice with controls disabled. Garage v1 / v2
 *     land here; AWS S3 + MinIO advertise true. The backend's GET
 *     versioning endpoint folds the capability flag into the same
 *     payload as the status so we get both in one round trip.
 *
 *   - supported, status="disabled" → status select offers Enabled
 *     (Suspended is hidden because there's nothing to suspend yet).
 *
 *   - supported, status in {enabled, suspended} → select offers
 *     Enabled + Suspended. S3 has no "go back to never-enabled" —
 *     disabled is observable only on buckets that were never enabled
 *     in the first place, so it's not a settable option after the
 *     first enable.
 *
 * The Save button stays disabled until the operator changes the
 * select away from the current backend state — saves a "clicked Save
 * without changing anything" round trip and avoids re-auditing a
 * no-op transition.
 */
export function VersioningSection({
  regionId,
  bucket,
}: {
  regionId: string;
  bucket: string;
}) {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useBucketVersioning(regionId, bucket);
  const putMutation = usePutBucketVersioning(regionId, bucket);

  // Local "pending" state for the select. Initialised from the wire
  // status whenever it changes (e.g. a successful PUT echoes the new
  // state back); the operator can then flip it and hit Save to commit.
  const [pending, setPending] = useState<VersioningStatus | null>(null);
  useEffect(() => {
    if (data?.status) setPending(data.status);
  }, [data?.status]);

  if (isLoading) {
    return (
      <section className="space-y-3" data-testid="versioning-section">
        <SectionHeader />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-muted-foreground">Loading versioning state…</p>
        </div>
      </section>
    );
  }

  if (error || !data) {
    return (
      <section className="space-y-3" data-testid="versioning-section">
        <SectionHeader />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-destructive">
            Couldn't load versioning state.
          </p>
        </div>
      </section>
    );
  }

  if (!data.supported) {
    return (
      <section className="space-y-3" data-testid="versioning-section">
        <SectionHeader />
        <div
          className="rounded-lg border bg-card p-6 flex items-center gap-3"
          data-testid="versioning-unsupported"
        >
          <Badge variant="secondary" className="text-xs">
            Unsupported
          </Badge>
          <p className="text-sm text-muted-foreground">
            Object versioning isn't supported by this backend driver.
          </p>
        </div>
      </section>
    );
  }

  const current = data.status;
  const selected = pending ?? current;
  const dirty = selected !== current;

  // "Disabled" is observable only on buckets that have never had
  // versioning turned on — the S3 contract doesn't let us flip back
  // to it once enabled, so we never render it as a settable option.
  // When current is "disabled" we offer Enabled only; once the
  // operator enables, the next render carries enabled+suspended.
  const options: VersioningStatus[] =
    current === "disabled" ? ["enabled"] : ["enabled", "suspended"];

  const handleSave = () => {
    if (!dirty || selected === "disabled") return;
    putMutation.mutate(
      { status: selected as "enabled" | "suspended" },
      {
        onSuccess: () => {
          // Refresh the versioning fetch so the badge + select snap to
          // the echoed-back state immediately. The bucket browser also
          // reads this same key when rendering its "show all versions"
          // toggle hint, so the invalidate keeps both in sync.
          queryClient.invalidateQueries({
            queryKey: ["user", "regions", regionId, "buckets", bucket, "versioning"],
          });
        },
      },
    );
  };

  return (
    <section className="space-y-3" data-testid="versioning-section">
      <SectionHeader />
      <div className="rounded-lg border bg-card p-6 space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center gap-3">
          <label
            htmlFor="versioning-status-select"
            className="text-sm font-medium min-w-[5rem]"
          >
            Status
          </label>
          <select
            id="versioning-status-select"
            data-testid="versioning-status-select"
            className="rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
            value={selected}
            disabled={putMutation.isPending}
            onChange={(e) => setPending(e.target.value as VersioningStatus)}
          >
            {current === "disabled" && (
              <option value="disabled" disabled>
                Disabled (never enabled)
              </option>
            )}
            {options.map((opt) => (
              <option key={opt} value={opt}>
                {opt === "enabled" ? "Enabled" : "Suspended"}
              </option>
            ))}
          </select>
          <CurrentBadge status={current} />
          <div className="sm:ml-auto">
            <Button
              type="button"
              size="sm"
              onClick={handleSave}
              disabled={!dirty || putMutation.isPending || selected === "disabled"}
              data-testid="versioning-save"
            >
              {putMutation.isPending ? "Saving…" : "Save"}
            </Button>
          </div>
        </div>

        <p className="text-xs text-muted-foreground">
          Versioning keeps previous versions of overwritten or deleted objects.
          Required for Object Lock.
        </p>

        {putMutation.isError && (
          <p
            className="text-xs text-destructive"
            data-testid="versioning-error"
          >
            {putMutation.error?.message ?? "Failed to update versioning."}
          </p>
        )}
      </div>
    </section>
  );
}

function SectionHeader() {
  return (
    <div className="flex items-center justify-between">
      <h2 className="text-sm font-medium text-muted-foreground">Versioning</h2>
    </div>
  );
}

function CurrentBadge({ status }: { status: VersioningStatus }) {
  if (status === "enabled") {
    return (
      <Badge variant="default" className="text-xs" data-testid="versioning-current-badge">
        Currently enabled
      </Badge>
    );
  }
  if (status === "suspended") {
    return (
      <Badge variant="secondary" className="text-xs" data-testid="versioning-current-badge">
        Currently suspended
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="text-xs" data-testid="versioning-current-badge">
      Never enabled
    </Badge>
  );
}
