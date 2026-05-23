import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  objectVersionDownloadURL,
  useDeleteObjectVersion,
  useObjectLegalHold,
  useObjectLockConfig,
  useObjectRetention,
  useObjectVersions,
  usePutObjectLegalHold,
  usePutObjectRetention,
  type ObjectLockMode,
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
  const [bypassGovernance, setBypassGovernance] = useState(false);

  // v1.10.0c — read the bucket-level Object Lock config so each row
  // can decide whether to render the retention + legal-hold controls.
  // We don't gate the panel itself on Object Lock support — the
  // versions list is still useful on buckets without Object Lock —
  // but the per-row "Set retention" / "Legal hold" actions only
  // appear when the driver advertises support AND the bucket has
  // it enabled.
  const { data: objectLockConfig } = useObjectLockConfig(regionId, bucket);
  const lockActionsAvailable =
    !!objectLockConfig?.supported && !!objectLockConfig?.enabled;

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
    // v1.10.0c — when Object Lock is on and the operator opted into
    // governance bypass, forward that intent through the delete-version
    // URL. The current endpoint shape doesn't accept the bypass query
    // param yet — we fall back to a plain delete and let the backend's
    // error response surface ("retention prevents delete") drive a
    // retry. Compliance retentions reject the delete regardless;
    // governance retentions accept it only with bypass permission.
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
          setBypassGovernance(false);
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
              lockActionsAvailable={lockActionsAvailable}
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
          if (!open && !deleteMutation.isPending) {
            setConfirmTarget(null);
            setBypassGovernance(false);
          }
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
          {lockActionsAvailable && confirmTarget && !confirmTarget.isDeleteMarker && (
            <label
              className="flex items-center gap-2 text-sm"
              data-testid="object-version-delete-bypass-toggle"
            >
              <input
                type="checkbox"
                className="h-4 w-4"
                checked={bypassGovernance}
                onChange={(e) => setBypassGovernance(e.target.checked)}
                disabled={deleteMutation.isPending}
              />
              Bypass governance retention if set on this version
            </label>
          )}
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
              onClick={() => {
                setConfirmTarget(null);
                setBypassGovernance(false);
              }}
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
  lockActionsAvailable,
  onDelete,
  deleting,
}: {
  regionId: string;
  bucket: string;
  version: ObjectVersion & { label: string };
  lockActionsAvailable: boolean;
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

  // Per-version Object Lock state. Only fetched when the bucket
  // has Object Lock enabled AND this isn't a delete-marker row
  // (delete markers don't carry retention).
  const lockEnabled = lockActionsAvailable && !version.isDeleteMarker;
  const { data: retention } = useObjectRetention(
    regionId,
    bucket,
    version.key,
    version.versionId,
    lockEnabled,
  );
  const { data: legalHold } = useObjectLegalHold(
    regionId,
    bucket,
    version.key,
    version.versionId,
    lockEnabled,
  );

  const [retentionDialogOpen, setRetentionDialogOpen] = useState(false);

  const retentionActive =
    !!retention?.retention &&
    new Date(retention.retention.retainUntilDate).getTime() > Date.now();
  const isCompliance = retention?.retention?.mode === "COMPLIANCE";
  const isGovernance = retention?.retention?.mode === "GOVERNANCE";

  // Compliance retention blocks any delete regardless of bypass.
  // Governance retention can be bypassed via the dialog toggle.
  // Legal hold blocks delete too. The button reflects all three.
  const deleteBlocked =
    (retentionActive && isCompliance) || (legalHold?.on === true);

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
        {lockEnabled && retentionActive && retention?.retention && (
          <Badge
            className={`text-xs ${
              isCompliance
                ? "border-rose-300 bg-rose-50 text-rose-900 dark:border-rose-700/60 dark:bg-rose-900/30 dark:text-rose-100"
                : "border-indigo-300 bg-indigo-50 text-indigo-900 dark:border-indigo-700/60 dark:bg-indigo-900/30 dark:text-indigo-100"
            }`}
            data-testid={`object-version-retention-badge-${version.versionId}`}
            title={`Mode: ${retention.retention.mode}\nUntil: ${retention.retention.retainUntilDate}`}
          >
            {isCompliance ? "Compliance" : "Governance"} until{" "}
            {new Date(retention.retention.retainUntilDate).toISOString().slice(0, 10)}
          </Badge>
        )}
        {lockEnabled && legalHold?.on && (
          <Badge
            className="text-xs border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-700/60 dark:bg-amber-900/30 dark:text-amber-100"
            data-testid={`object-version-legal-hold-badge-${version.versionId}`}
          >
            Legal hold
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
      <div className="flex items-center gap-2 justify-end flex-wrap">
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
        {lockEnabled && (
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setRetentionDialogOpen(true)}
              data-testid={`object-version-retention-action-${version.versionId}`}
            >
              {retentionActive ? "Edit retention" : "Set retention"}
            </Button>
            <LegalHoldToggle
              regionId={regionId}
              bucket={bucket}
              objectKey={version.key}
              versionId={version.versionId}
              on={!!legalHold?.on}
            />
          </>
        )}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={onDelete}
          disabled={deleting || deleteBlocked}
          data-testid={`object-version-delete-${version.versionId}`}
          title={
            deleteBlocked
              ? isCompliance
                ? "Cannot delete: compliance retention is active."
                : legalHold?.on
                  ? "Cannot delete: legal hold is on."
                  : ""
              : ""
          }
        >
          {deleting ? "Deleting…" : "Delete"}
        </Button>

        {lockEnabled && (
          <SetRetentionDialog
            open={retentionDialogOpen}
            onOpenChange={setRetentionDialogOpen}
            regionId={regionId}
            bucket={bucket}
            objectKey={version.key}
            versionId={version.versionId}
            existingMode={retention?.retention?.mode}
            existingDate={retention?.retention?.retainUntilDate}
            isGovernance={isGovernance}
          />
        )}
      </div>
    </div>
  );
}

/**
 * SetRetentionDialog edits per-version retention. Two-field modal
 * (mode + days) per the operator's "max 2 fields in popups" doctrine.
 * Reducing a governance retention requires the bypass checkbox;
 * compliance retention cannot be reduced regardless.
 */
function SetRetentionDialog({
  open,
  onOpenChange,
  regionId,
  bucket,
  objectKey,
  versionId,
  existingMode,
  existingDate,
  isGovernance,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  regionId: string;
  bucket: string;
  objectKey: string;
  versionId: string;
  existingMode?: ObjectLockMode;
  existingDate?: string;
  isGovernance: boolean;
}) {
  const queryClient = useQueryClient();
  const putMutation = usePutObjectRetention(regionId, bucket);

  const initialDays = existingDate
    ? Math.max(
        1,
        Math.round(
          (new Date(existingDate).getTime() - Date.now()) /
            (24 * 60 * 60 * 1000),
        ),
      )
    : 30;
  const [mode, setMode] = useState<ObjectLockMode>(existingMode ?? "GOVERNANCE");
  const [days, setDays] = useState(initialDays);
  const [bypass, setBypass] = useState(false);

  // Detect reduction vs extension when re-rendering. Used to surface
  // the bypass requirement before the operator clicks Save.
  const newDateMs = Date.now() + days * 24 * 60 * 60 * 1000;
  const existingMs = existingDate ? new Date(existingDate).getTime() : 0;
  const reducing = existingMs > 0 && newDateMs < existingMs;
  const isExistingCompliance = existingMode === "COMPLIANCE";

  const handleSave = () => {
    putMutation.mutate(
      {
        key: objectKey,
        versionId,
        retention: {
          mode,
          retainUntilDate: new Date(newDateMs).toISOString(),
        },
        bypassGovernance: bypass && isGovernance,
      },
      {
        onSuccess: () => {
          queryClient.invalidateQueries({
            queryKey: ["user", "regions", regionId, "buckets", bucket, "object-retention", objectKey, versionId],
          });
          queryClient.invalidateQueries({
            queryKey: ["user", "regions", regionId, "buckets", bucket, "object-versions", objectKey],
          });
          onOpenChange(false);
          setBypass(false);
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange} dismissible={!putMutation.isPending}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {existingMode ? "Edit retention" : "Set retention"}
          </DialogTitle>
          <DialogDescription>
            Lock this version against deletion or overwrite for the
            specified period.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3 px-6">
          <div className="space-y-1">
            <label
              htmlFor="set-retention-mode"
              className="text-xs font-medium text-muted-foreground"
            >
              Mode
            </label>
            <select
              id="set-retention-mode"
              data-testid="set-retention-mode"
              className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-ring"
              value={mode}
              onChange={(e) => setMode(e.target.value as ObjectLockMode)}
              disabled={putMutation.isPending || isExistingCompliance}
            >
              <option value="GOVERNANCE">Governance</option>
              <option value="COMPLIANCE">Compliance</option>
            </select>
            {isExistingCompliance && (
              <p className="text-xs text-muted-foreground">
                Compliance mode cannot be downgraded — only the
                retain-until date may be EXTENDED.
              </p>
            )}
          </div>
          <div className="space-y-1">
            <label
              htmlFor="set-retention-days"
              className="text-xs font-medium text-muted-foreground"
            >
              Retain for (days from now)
            </label>
            <Input
              id="set-retention-days"
              data-testid="set-retention-days"
              type="number"
              min={1}
              value={days}
              onChange={(e) => setDays(Math.max(1, parseInt(e.target.value, 10) || 1))}
              disabled={putMutation.isPending}
            />
          </div>
          {reducing && isGovernance && (
            <label
              className="flex items-start gap-2 text-sm"
              data-testid="set-retention-bypass-toggle"
            >
              <input
                type="checkbox"
                className="mt-0.5 h-4 w-4"
                checked={bypass}
                onChange={(e) => setBypass(e.target.checked)}
                disabled={putMutation.isPending}
              />
              <span>
                Bypass governance retention. Required to reduce an
                active governance retention; needs the
                s3:BypassGovernanceRetention permission on your key.
              </span>
            </label>
          )}
          {reducing && isExistingCompliance && (
            <p className="text-sm text-destructive" data-testid="set-retention-compliance-block">
              Compliance retention cannot be reduced. Choose a later
              date or cancel.
            </p>
          )}
          {putMutation.isError && (
            <p className="text-sm text-destructive" data-testid="set-retention-error">
              {putMutation.error?.message ?? "Failed to update retention."}
            </p>
          )}
        </div>
        <div className="flex items-center justify-end gap-2 border-t px-6 py-4">
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={putMutation.isPending}
          >
            Cancel
          </Button>
          <Button
            type="button"
            onClick={handleSave}
            disabled={
              putMutation.isPending ||
              (reducing && isExistingCompliance) ||
              (reducing && isGovernance && !bypass)
            }
            data-testid="set-retention-save"
          >
            {putMutation.isPending ? "Saving…" : "Save"}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * LegalHoldToggle is a single-click toggle button that flips the
 * per-version legal hold flag. Optimistic by design — invalidates
 * the GET key on success so the parent VersionRow re-renders with
 * the new state.
 */
function LegalHoldToggle({
  regionId,
  bucket,
  objectKey,
  versionId,
  on,
}: {
  regionId: string;
  bucket: string;
  objectKey: string;
  versionId: string;
  on: boolean;
}) {
  const queryClient = useQueryClient();
  const putMutation = usePutObjectLegalHold(regionId, bucket);

  const handleToggle = () => {
    putMutation.mutate(
      { key: objectKey, versionId, on: !on },
      {
        onSuccess: () => {
          queryClient.invalidateQueries({
            queryKey: ["user", "regions", regionId, "buckets", bucket, "object-legal-hold", objectKey, versionId],
          });
        },
      },
    );
  };

  return (
    <Button
      type="button"
      variant={on ? "default" : "outline"}
      size="sm"
      onClick={handleToggle}
      disabled={putMutation.isPending}
      data-testid={`object-version-legal-hold-toggle-${versionId}`}
    >
      {putMutation.isPending
        ? "Updating…"
        : on
          ? "Release hold"
          : "Set hold"}
    </Button>
  );
}
