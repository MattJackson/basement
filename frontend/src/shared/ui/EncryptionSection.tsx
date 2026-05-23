import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  useBucketEncryption,
  usePutBucketEncryption,
  useDeleteBucketEncryption,
  type SSEAlgorithm,
} from "@/shared/api/queries";

/**
 * EncryptionSection renders the per-bucket default server-side
 * encryption card on the bucket detail page (v1.10.0d). Four branches
 * the section handles internally so the parent can mount it
 * unconditionally:
 *
 *   - both supportedS3 + supportedKms false → "Not supported by this
 *     backend driver" notice. Garage v1 / v2 land here.
 *
 *   - only supportedS3 true → show the enable/disable toggle without
 *     the algorithm radio (SSE-S3 is the only option).
 *
 *   - both true → full editor: enable toggle + algorithm radio
 *     (SSE-S3 vs SSE-KMS) + KMS key ID input + bucket-key checkbox
 *     when SSE-KMS is selected.
 *
 *   - enabled buckets: Save updates the config; a separate "Disable"
 *     button calls DELETE.
 *
 * SSE-S3 is "backend manages the key end-to-end" — operator just flips
 * it on. SSE-KMS hands key control to an external KMS; the operator
 * must supply the key ARN/ID and the backend's IAM role needs
 * permission to call kms:Encrypt / kms:Decrypt with it. BucketKey is
 * the SSE-KMS cost optimization (one KMS call per ~5min instead of
 * one per object).
 */
export function EncryptionSection({
  regionId,
  bucket,
}: {
  regionId: string;
  bucket: string;
}) {
  const queryClient = useQueryClient();
  const { data, isLoading, error } = useBucketEncryption(regionId, bucket);
  const putMutation = usePutBucketEncryption(regionId, bucket);
  const deleteMutation = useDeleteBucketEncryption(regionId, bucket);

  // Local form state. Initialised from the backend whenever the
  // returned config changes — e.g. after a successful PUT the
  // mutation echoes the new state back and the form snaps to it.
  const [algorithm, setAlgorithm] = useState<SSEAlgorithm>("AES256");
  const [kmsKeyId, setKmsKeyId] = useState("");
  const [bucketKey, setBucketKey] = useState(false);

  useEffect(() => {
    if (!data) return;
    if (data.enabled && data.algorithm) {
      setAlgorithm(data.algorithm);
      setKmsKeyId(data.kmsKeyId ?? "");
      setBucketKey(!!data.bucketKey);
    } else {
      // Disabled state — default the form to SSE-S3 (the simpler path)
      // so an operator clicking Enable doesn't have to pick an
      // algorithm if they don't want to think about it.
      setAlgorithm(data.supportedS3 ? "AES256" : "aws:kms");
      setKmsKeyId("");
      setBucketKey(false);
    }
  }, [data]);

  if (isLoading) {
    return (
      <section className="space-y-3" data-testid="encryption-section">
        <SectionHeader />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-muted-foreground">
            Loading encryption state…
          </p>
        </div>
      </section>
    );
  }

  if (error || !data) {
    return (
      <section className="space-y-3" data-testid="encryption-section">
        <SectionHeader />
        <div className="rounded-lg border bg-card p-6">
          <p className="text-sm text-destructive">
            Couldn't load encryption state.
          </p>
        </div>
      </section>
    );
  }

  if (!data.supportedS3 && !data.supportedKms) {
    return (
      <section className="space-y-3" data-testid="encryption-section">
        <SectionHeader />
        <div
          className="rounded-lg border bg-card p-6 flex items-center gap-3"
          data-testid="encryption-unsupported"
        >
          <Badge variant="secondary" className="text-xs">
            Unsupported
          </Badge>
          <p className="text-sm text-muted-foreground">
            Default encryption isn't supported by this backend driver.
          </p>
        </div>
      </section>
    );
  }

  const busy = putMutation.isPending || deleteMutation.isPending;
  const invalidate = () => {
    queryClient.invalidateQueries({
      queryKey: ["user", "regions", regionId, "buckets", bucket, "encryption"],
    });
  };

  const handleSave = () => {
    putMutation.mutate(
      {
        algorithm,
        kmsKeyId: algorithm === "aws:kms" ? kmsKeyId.trim() : undefined,
        bucketKey: algorithm === "aws:kms" ? bucketKey : undefined,
      },
      { onSuccess: invalidate },
    );
  };

  const handleDisable = () => {
    deleteMutation.mutate(undefined, { onSuccess: invalidate });
  };

  // SSE-KMS save needs a non-empty key ID; the API rejects empty
  // anyway, but a client-side guard avoids the wasted round-trip.
  const saveDisabled =
    busy || (algorithm === "aws:kms" && kmsKeyId.trim() === "");

  return (
    <section className="space-y-3" data-testid="encryption-section">
      <SectionHeader />
      <div className="rounded-lg border bg-card p-6 space-y-5">
        <div className="flex items-center gap-3">
          <CurrentStatusBadge enabled={data.enabled} />
          {data.enabled && data.algorithm && (
            <p
              className="text-xs text-muted-foreground"
              data-testid="encryption-current-algorithm"
            >
              Currently using{" "}
              <span className="font-medium">
                {algorithmLabel(data.algorithm)}
              </span>
              .
            </p>
          )}
        </div>

        {/* Algorithm radio — only when both axes are supported.
            Otherwise show a static "SSE-S3 only" notice so the operator
            knows which path they're on. */}
        {data.supportedS3 && data.supportedKms ? (
          <fieldset className="space-y-2">
            <legend className="text-xs font-medium text-muted-foreground">
              Algorithm
            </legend>
            <label className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="sse-algorithm"
                value="AES256"
                checked={algorithm === "AES256"}
                onChange={() => setAlgorithm("AES256")}
                disabled={busy}
                data-testid="encryption-radio-s3"
                className="mt-1 h-4 w-4"
              />
              <div className="space-y-0.5">
                <div className="font-medium">SSE-S3 (backend keys)</div>
                <div className="text-xs text-muted-foreground">
                  The backend manages encryption keys for you. Transparent
                  to clients — no key material to track.
                </div>
              </div>
            </label>
            <label className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="sse-algorithm"
                value="aws:kms"
                checked={algorithm === "aws:kms"}
                onChange={() => setAlgorithm("aws:kms")}
                disabled={busy}
                data-testid="encryption-radio-kms"
                className="mt-1 h-4 w-4"
              />
              <div className="space-y-0.5">
                <div className="font-medium">SSE-KMS (operator key)</div>
                <div className="text-xs text-muted-foreground">
                  You control the key via an external KMS. The backend
                  can't decrypt without KMS still granting access.
                </div>
              </div>
            </label>
          </fieldset>
        ) : (
          <p
            className="text-xs text-muted-foreground"
            data-testid="encryption-only-s3-notice"
          >
            This backend supports SSE-S3 only.
          </p>
        )}

        {algorithm === "aws:kms" && (
          <div className="space-y-3 pl-6">
            <div className="space-y-1">
              <label
                htmlFor="encryption-kms-key-input"
                className="text-xs font-medium text-muted-foreground"
              >
                KMS Key ID or ARN
              </label>
              <Input
                id="encryption-kms-key-input"
                data-testid="encryption-kms-key-input"
                type="text"
                placeholder="arn:aws:kms:us-east-1:111122223333:key/abcd-1234"
                value={kmsKeyId}
                onChange={(e) => setKmsKeyId(e.target.value)}
                disabled={busy}
              />
              <p className="text-xs text-muted-foreground">
                The backend's IAM role must have kms:Encrypt /
                kms:Decrypt permission on this key.
              </p>
            </div>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                data-testid="encryption-bucket-key-toggle"
                checked={bucketKey}
                onChange={(e) => setBucketKey(e.target.checked)}
                disabled={busy}
                className="h-4 w-4"
              />
              <span>
                Use S3 Bucket Key{" "}
                <span className="text-xs text-muted-foreground">
                  (reduces KMS call costs on write-heavy buckets)
                </span>
              </span>
            </label>
          </div>
        )}

        <div className="flex items-center gap-3">
          <Button
            type="button"
            size="sm"
            onClick={handleSave}
            disabled={saveDisabled}
            data-testid="encryption-save"
          >
            {putMutation.isPending
              ? "Saving…"
              : data.enabled
                ? "Update encryption"
                : "Enable encryption"}
          </Button>
          {data.enabled && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={handleDisable}
              disabled={busy}
              data-testid="encryption-disable"
            >
              {deleteMutation.isPending ? "Disabling…" : "Disable"}
            </Button>
          )}
          {(putMutation.isSuccess || deleteMutation.isSuccess) && !busy && (
            <span
              className="text-xs text-muted-foreground"
              data-testid="encryption-saved-notice"
            >
              Saved.
            </span>
          )}
        </div>

        <p className="text-xs text-muted-foreground">
          Default encryption applies to new objects written to this
          bucket. Objects PUT before enabling encryption keep whatever
          state they had at PUT time.
        </p>

        {(putMutation.isError || deleteMutation.isError) && (
          <p
            className="text-xs text-destructive"
            data-testid="encryption-error"
          >
            {(putMutation.error ?? deleteMutation.error)?.message ??
              "Failed to update encryption."}
          </p>
        )}
      </div>
    </section>
  );
}

function SectionHeader() {
  return (
    <div className="flex items-center justify-between">
      <h2 className="text-sm font-medium text-muted-foreground">
        Server-Side Encryption
      </h2>
    </div>
  );
}

function CurrentStatusBadge({ enabled }: { enabled: boolean }) {
  if (enabled) {
    return (
      <Badge
        variant="default"
        className="text-xs"
        data-testid="encryption-status-badge"
      >
        Enabled
      </Badge>
    );
  }
  return (
    <Badge
      variant="outline"
      className="text-xs"
      data-testid="encryption-status-badge"
    >
      Disabled
    </Badge>
  );
}

function algorithmLabel(a: SSEAlgorithm): string {
  switch (a) {
    case "AES256":
      return "SSE-S3 (backend keys)";
    case "aws:kms":
      return "SSE-KMS (operator key)";
    default:
      return a;
  }
}
