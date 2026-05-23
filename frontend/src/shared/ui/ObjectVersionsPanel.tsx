import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import {
  objectVersionDownloadURL,
  useDeleteObjectVersion,
  useObjectVersions,
  type ObjectVersion,
} from "@/shared/api/queries";
import { humanizeBytes, humanizeTime } from "@/shared/lib/format";

/**
 * ObjectVersionsPanel renders the per-object version history view
 * (v1.10.0b). Opens to the right of the bucket browser when the
 * operator clicks "Versions" on a file row.
 *
 * Each row shows: version label (v1 newest first), last-modified,
 * size, and per-row actions (Download, Delete). Delete markers
 * render with a struck-through label and an amber "delete marker"
 * badge — the action stays available because operators sometimes
 * want to remove a stray delete marker to make a noncurrent version
 * "current" again on a versioned bucket.
 *
 * Restore is intentionally NOT in this cycle (v1.10.0b ships
 * list + download + delete; restore is the v1.10.0b.1 follow-up).
 * The button isn't drawn here so an operator doesn't see a stub.
 */
export function ObjectVersionsPanel({
  regionId,
  bucket,
  objectKey,
  onClose,
}: {
  regionId: string;
  bucket: string;
  objectKey: string;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const { data, isLoading, error, refetch } = useObjectVersions(
    regionId,
    bucket,
    objectKey,
    true,
  );
  const deleteMutation = useDeleteObjectVersion(regionId, bucket);
  const [confirmTarget, setConfirmTarget] = useState<ObjectVersion | null>(null);

  const versions = data?.versions ?? [];
  // Newest first within the key — the backend returns them in
  // S3 order (newest version first), so we render as-is. Number
  // descending so v3 is current, v1 is oldest visible.
  const numbered = versions.map((v, idx) => ({
    ...v,
    // The label "v{N}" counts from the OLDEST so v1 is the historical
    // first version operators expect. Reverse the index for the label.
    label: `v${versions.length - idx}`,
  }));

  const handleDelete = (target: ObjectVersion) => {
    deleteMutation.mutate(
      { key: target.key, versionId: target.versionId },
      {
        onSuccess: () => {
          // Refresh both the versions list (this panel) AND the
          // object list (the parent browser) — deleting a current
          // version may drop the row from the bucket listing entirely.
          queryClient.invalidateQueries({
            queryKey: ["user", "regions", regionId, "buckets", bucket, "object-versions", target.key],
          });
          queryClient.invalidateQueries({
            queryKey: ["user", "regions", regionId, "buckets", bucket, "objects-inf"],
          });
          setConfirmTarget(null);
        },
        onError: () => {
          // Keep the dialog open so the operator sees the error and
          // can choose to retry. The error banner inside the dialog
          // surfaces deleteMutation.error.message.
        },
      },
    );
  };

  return (
    <aside
      className="rounded-lg border bg-card overflow-hidden"
      data-testid="object-versions-panel"
    >
      <header className="flex items-center justify-between gap-3 border-b bg-muted/30 px-4 py-3">
        <div className="min-w-0">
          <h3 className="text-sm font-medium truncate" title={objectKey}>
            Versions of {objectKey.split("/").pop() || objectKey}
          </h3>
          {!isLoading && !error && (
            <p className="text-xs text-muted-foreground">
              {versions.length === 0
                ? "No versions"
                : `${versions.length} version${versions.length === 1 ? "" : "s"}`}
            </p>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => refetch()}
            disabled={isLoading}
          >
            Refresh
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={onClose}
            data-testid="object-versions-close"
          >
            Close
          </Button>
        </div>
      </header>

      <div className="divide-y">
        {isLoading ? (
          <div className="p-6 text-sm text-muted-foreground">Loading versions…</div>
        ) : error ? (
          <div className="p-6 text-sm text-destructive">
            Couldn't load versions: {error.message}
          </div>
        ) : versions.length === 0 ? (
          <div className="p-6 text-sm text-muted-foreground">
            This object has no version history yet. Newly enabled buckets only
            start versioning subsequent writes.
          </div>
        ) : (
          numbered.map((v) => (
            <VersionRow
              key={v.versionId}
              regionId={regionId}
              bucket={bucket}
              version={v}
              onDelete={() => setConfirmTarget(v)}
              deleting={
                deleteMutation.isPending &&
                deleteMutation.variables?.versionId === v.versionId
              }
            />
          ))
        )}
      </div>

      <AlertDialog
        open={!!confirmTarget}
        onOpenChange={(open) => {
          if (!open && !deleteMutation.isPending) setConfirmTarget(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Permanently delete this version?
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirmTarget?.isDeleteMarker
                ? "This delete marker will be removed. The previous noncurrent version will become current."
                : "This version row will be removed forever. The object content for this version cannot be recovered."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {deleteMutation.isError && (
            <p
              className="text-sm text-destructive"
              data-testid="object-version-delete-error"
            >
              {deleteMutation.error?.message ?? "Delete failed."}
            </p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel
              onClick={() => setConfirmTarget(null)}
              disabled={deleteMutation.isPending}
            >
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => confirmTarget && handleDelete(confirmTarget)}
              disabled={deleteMutation.isPending}
              data-testid="object-version-delete-confirm"
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleteMutation.isPending ? "Deleting…" : "Delete version"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </aside>
  );
}

function VersionRow({
  regionId,
  bucket,
  version,
  onDelete,
  deleting,
}: {
  regionId: string;
  bucket: string;
  version: ObjectVersion & { label: string };
  onDelete: () => void;
  deleting: boolean;
}) {
  const lastModified = version.lastModified
    ? humanizeTime(version.lastModified)
    : "—";
  const downloadURL = objectVersionDownloadURL(
    regionId,
    bucket,
    version.key,
    version.versionId,
  );

  return (
    <div
      className="grid grid-cols-[auto_1fr_auto] gap-3 items-center px-4 py-3"
      data-testid={`object-version-row-${version.versionId}`}
      data-is-delete-marker={version.isDeleteMarker ? "true" : "false"}
      data-is-latest={version.isLatest ? "true" : "false"}
    >
      <div className="flex items-center gap-2 flex-wrap">
        <Badge
          variant={version.isLatest ? "default" : "outline"}
          className="text-xs tabular-nums"
        >
          {version.label}
        </Badge>
        {version.isLatest && !version.isDeleteMarker && (
          <Badge variant="secondary" className="text-xs">
            current
          </Badge>
        )}
        {version.isDeleteMarker && (
          <Badge
            className="text-xs border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-700/60 dark:bg-amber-900/30 dark:text-amber-100"
            data-testid="object-version-delete-marker-badge"
          >
            delete marker
          </Badge>
        )}
      </div>
      <div className="min-w-0">
        <p
          className={`text-sm font-mono truncate ${
            version.isDeleteMarker ? "line-through text-muted-foreground" : ""
          }`}
          title={version.versionId}
        >
          {version.versionId}
        </p>
        <p className="text-xs text-muted-foreground tabular-nums">
          {lastModified}
          {!version.isDeleteMarker && (
            <>
              <span className="mx-1.5">&middot;</span>
              {humanizeBytes(version.size)}
            </>
          )}
        </p>
      </div>
      <div className="flex items-center gap-2 justify-end">
        {!version.isDeleteMarker && (
          <a
            href={downloadURL}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center justify-center rounded-md border border-border bg-background hover:bg-muted h-8 px-3 text-xs font-medium transition-colors"
            data-testid={`object-version-download-${version.versionId}`}
          >
            Download
          </a>
        )}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onDelete}
          disabled={deleting}
          data-testid={`object-version-delete-${version.versionId}`}
        >
          {deleting ? "Deleting…" : "Delete"}
        </Button>
      </div>
    </div>
  );
}
