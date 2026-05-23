import { createFileRoute, useNavigate, useLocation } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyState } from "@/shared/ui/EmptyState";
import { ErrorBanner } from "@/shared/ui/ErrorBanner";
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
  useBucketVersioning,
  useFederationByTarget,
  useUserRegionBuckets,
  useUserRegionObjectsInfinite,
  useUserRegionPresignGet,
} from "@/shared/api/queries";
import { humanizeBytes, humanizeTime } from "@/shared/lib/format";
import type { components } from "@/shared/api/types.gen";
import type { FederatedBucket } from "@/shared/api/queries";
import { UploadDialog } from "@/components/upload/UploadDialog";
import { VersioningSection } from "@/shared/ui/VersioningSection";
import { ObjectVersionsPanel } from "@/shared/ui/ObjectVersionsPanel";
import { ObjectLockSection } from "@/shared/ui/ObjectLockSection";

// Object browser for buckets reached via a UserRegion (ADR-0002, v1.1.0c).
// Every backend call goes through /api/v1/user/regions/{regionId}/buckets/
// {bid}/* and is signed with the region's S3 key. We deliberately do NOT
// call a per-bucket useUserBucket() — there is no per-bucket basement
// record at the region tier; ListBuckets via the user's key is
// authoritative.
//
// v1.4.0a: virtualized scrolling via @tanstack/react-virtual + infinite
// scroll over continuation tokens. Folders sort to the top, files in
// the order S3 returned. Approaching the bottom of a truncated page
// auto-fetches the next continuation. Scroll position resets to the
// top whenever the URL `prefix` changes (folder navigation).
export const Route = createFileRoute("/files/$regionId/b/$bid")({
  component: UserRegionBucketObjects,
});

type ObjectInfo = components["schemas"]["ObjectInfo"];

// VirtualRow is the union the virtualizer renders. Folders and files
// share the same fixed-height row so the virtualizer can estimate +
// position without measuring per-item — predictable scrolling on a
// flat directory with 10K+ rows.
type VirtualRow =
  | { kind: "folder"; key: string }
  | { kind: "file"; key: string; obj: ObjectInfo }
  | { kind: "loadMoreSentinel" };

// Fixed row height (px). Matches a comfortable table-row tap target at
// the default font size. If the design shifts this, the estimateSize
// callback below must move with it.
//
// v1.8.0e: mobile breakpoint (<640px) switches to a stacked card
// layout that needs more vertical room — name on row 1, size + date on
// row 2, action icon pinned right. 76px accommodates the two-line
// stack plus comfortable tap padding (each row + chrome ≥44px).
const ROW_HEIGHT_DESKTOP = 48;
const ROW_HEIGHT_MOBILE = 76;
const MOBILE_BREAKPOINT_PX = 640;

function UserRegionBucketObjects() {
  const { regionId, bid } = Route.useParams();
  const navigate = useNavigate();

  // Read search params via useLocation() so the component re-renders
  // when navigate() updates the URL with a new prefix (folder click).
  // The previous URLSearchParams read at module load was non-reactive,
  // which broke folder navigation in v1.3.0c.1 — clicking a folder
  // updated the URL but the prefix stayed at "".
  const location = useLocation();
  const urlParams = new URLSearchParams(location.searchStr || "");
  const prefix = urlParams.get("prefix") ?? "";

  const { data: bucketsData, isLoading: bucketsLoading } = useUserRegionBuckets(regionId);
  // v1.4.0a: the bucket-list hook now returns {buckets, perBucketStatsAvailable}.
  const bucket = bucketsData?.buckets.find((b) => b.id === bid);
  const bucketAlias = bucket?.aliases?.[0] || bid;

  const {
    data: pages,
    isLoading: objectsLoading,
    isFetchingNextPage,
    error: objectsError,
    refetch,
    fetchNextPage,
    hasNextPage,
  } = useUserRegionObjectsInfinite(regionId, bid, prefix);

  const presignMutation = useUserRegionPresignGet(regionId, bid);

  // v1.6.0e — reverse-lookup so the bucket browser can render a
  // federation badge when this (region, bucket) is part of a
  // FederatedBucket. The endpoint returns 204 + null when the bucket
  // isn't federated; the hook surfaces that as `data === null` and we
  // render nothing. Best-effort: a hook error doesn't block the
  // bucket browser from rendering.
  const { data: federation } = useFederationByTarget(regionId, bucketAlias);

  // v1.10.0b — versioning state for this bucket. Drives:
  //   (1) the VersioningSection card at the bottom of the page
  //       (enable / suspend toggle)
  //   (2) the per-row "Versions" button — only meaningful when the
  //       bucket has versioning on (enabled or suspended) AND the
  //       driver supports it, otherwise the button stays hidden so
  //       the operator isn't tempted by a dead control
  //   (3) the "Show all versions" toggle on the search/filter row,
  //       which is also gated by `supported`.
  const { data: versioningState } = useBucketVersioning(regionId, bid);
  const versioningActive =
    !!versioningState?.supported &&
    (versioningState?.status === "enabled" ||
      versioningState?.status === "suspended");

  // "Show all versions" toggle — surfaces per-row Versions buttons
  // prominently when ON, and auto-opens the panel for the last-clicked
  // row. When OFF the per-row Versions button is a subtle ghost button.
  // The v1.10.0a backend doesn't expose a prefix-wide list-versions
  // endpoint, so we don't fan out N parallel reads here — each row
  // opens its own panel on demand (the per-key endpoint is what's
  // available today).
  const [showAllVersions, setShowAllVersions] = useState(false);

  // Active versions panel — exactly one row's history at a time.
  // Clicking Versions on a second row swaps the panel to that key.
  // null means no panel is open.
  const [versionsForKey, setVersionsForKey] = useState<string | null>(null);

  // Reset the open panel whenever the operator navigates folders or
  // buckets — the key would no longer match the current listing.
  useEffect(() => {
    setVersionsForKey(null);
  }, [prefix, bid, regionId]);

  // Flatten the merged page set into a single VirtualRow list. Folders
  // are taken from the FIRST page only (S3 only returns CommonPrefixes
  // on the initial list — subsequent continuation-token pages carry an
  // empty array). Files concatenate across every page. A trailing
  // loadMoreSentinel row exists when more pages remain — the
  // virtualizer's visibility hook uses it to trigger the next fetch.
  const rows = useMemo<VirtualRow[]>(() => {
    const pageList = pages?.pages ?? [];
    if (pageList.length === 0) return [];
    const firstPage = pageList[0];
    const folders = ([...(firstPage?.commonPrefixes ?? [])])
      .sort()
      .map<VirtualRow>((p) => ({ kind: "folder", key: p }));
    const files: VirtualRow[] = [];
    for (const pg of pageList) {
      for (const obj of pg?.objects ?? []) {
        files.push({ kind: "file", key: obj.key, obj });
      }
    }
    const out: VirtualRow[] = [...folders, ...files];
    if (hasNextPage) {
      out.push({ kind: "loadMoreSentinel" });
    }
    return out;
  }, [pages, hasNextPage]);

  const handleFolderClick = (folderPrefix: string) => {
    navigate({
      to: "/files/$regionId/b/$bid",
      params: { regionId, bid },
      search: { prefix: folderPrefix, token: "" },
    });
  };

  const handleDownload = async (key: string) => {
    presignMutation.mutateAsync({ key, ttl: 3600 }).then((presignedUrl) => {
      window.open(presignedUrl.url, "_blank");
    });
  };

  const handleRefresh = () => {
    refetch();
  };

  const handleBack = () => {
    navigate({ to: "/files/$regionId", params: { regionId } });
  };

  const [uploadOpen, setUploadOpen] = useState(false);

  const handleUploadSuccess = () => {
    setUploadOpen(false);
    refetch();
  };

  // v1.4.0b: batch object operations — select + delete.
  //
  // Selection state is a Set<string> of object keys. Folders are NEVER
  // selectable; recursive deletes would silently fan out to potentially
  // millions of objects without the operator confirming the blast
  // radius. Move/copy is punted to v1.5 per cycle plan.
  //
  // perRowError tracks the per-key error indicator after a partial
  // failure. It's cleared whenever the operator starts a fresh batch.
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(() => new Set());
  const [perRowError, setPerRowError] = useState<Map<string, string>>(() => new Map());
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const [isDeleting, setIsDeleting] = useState(false);
  const [deleteSummary, setDeleteSummary] = useState<{ failures: number } | null>(null);

  // Reset selection whenever the operator changes folders. Leaving
  // selection stale across navigation is a footgun — the keys would
  // still match the previous prefix's objects but show up greyed out
  // against the new list.
  useEffect(() => {
    setSelectedKeys(new Set());
    setPerRowError(new Map());
    setDeleteSummary(null);
  }, [prefix, bid, regionId]);

  const toggleSelected = useCallback((key: string) => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
    // Stale per-row error indicators clear once the operator interacts
    // with the row again — keeps the next batch attempt visually clean.
    setPerRowError((prev) => {
      if (!prev.has(key)) return prev;
      const next = new Map(prev);
      next.delete(key);
      return next;
    });
  }, []);

  // Visible file rows (folders excluded — they're not selectable).
  // Used by select-all + count display + deletion fan-out.
  const visibleFileKeys = useMemo(
    () => rows.flatMap((r) => (r.kind === "file" ? [r.key] : [])),
    [rows],
  );

  const allVisibleSelected =
    visibleFileKeys.length > 0 &&
    visibleFileKeys.every((k) => selectedKeys.has(k));
  const someVisibleSelected =
    !allVisibleSelected && visibleFileKeys.some((k) => selectedKeys.has(k));

  const toggleSelectAllVisible = useCallback(() => {
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      if (allVisibleSelected) {
        for (const k of visibleFileKeys) next.delete(k);
      } else {
        for (const k of visibleFileKeys) next.add(k);
      }
      return next;
    });
  }, [allVisibleSelected, visibleFileKeys]);

  const clearSelection = useCallback(() => {
    setSelectedKeys(new Set());
    setPerRowError(new Map());
    setDeleteSummary(null);
  }, []);

  const handleBatchDelete = useCallback(async () => {
    const keys = Array.from(selectedKeys);
    if (keys.length === 0) {
      setDeleteConfirmOpen(false);
      return;
    }
    setIsDeleting(true);
    setPerRowError(new Map());
    setDeleteSummary(null);

    // Fan out parallel DELETEs. Promise.allSettled so a single failure
    // doesn't abort the batch — the operator gets per-row error
    // indicators on the keys that survived. The backend endpoint
    // (DELETE /api/v1/user/regions/{regionId}/buckets/{bid}/objects/{key},
    // see internal/api/user_regions.go:1325) is idempotent on already-
    // missing keys (S3 quirk — DeleteObject succeeds even if the key
    // doesn't exist), so re-running the batch on the failed survivors
    // after the operator addresses whatever caused the failure is safe.
    const results = await Promise.allSettled(
      keys.map(async (key) => {
        const url = `/api/v1/user/regions/${encodeURIComponent(regionId)}/buckets/${encodeURIComponent(bid)}/objects/${encodeURIComponent(key)}`;
        const res = await fetch(url, { method: "DELETE", credentials: "include" });
        if (!res.ok) {
          const body = await res.json().catch(() => null);
          throw new Error(
            (body && typeof body === "object" && "message" in body && typeof body.message === "string"
              ? body.message
              : `HTTP ${res.status}`),
          );
        }
        return key;
      }),
    );

    const nextErrors = new Map<string, string>();
    let failures = 0;
    const succeeded = new Set<string>();
    results.forEach((r, idx) => {
      const key = keys[idx]!;
      if (r.status === "fulfilled") {
        succeeded.add(key);
      } else {
        failures += 1;
        nextErrors.set(key, r.reason instanceof Error ? r.reason.message : String(r.reason));
      }
    });

    setPerRowError(nextErrors);
    // Drop succeeded keys from the selection; keep failed ones so the
    // operator can see what to retry. If everything succeeded, the
    // selection drops to empty and the action bar dismounts.
    setSelectedKeys((prev) => {
      const next = new Set(prev);
      for (const k of succeeded) next.delete(k);
      return next;
    });
    setIsDeleting(false);
    setDeleteConfirmOpen(false);
    setDeleteSummary({ failures });

    // Refresh the object list once any deletes landed so the rows drop
    // from view. The infinite query refetches all pages on refresh.
    if (succeeded.size > 0) {
      refetch();
    }
  }, [selectedKeys, regionId, bid, refetch]);

  if (bucketsLoading) {
    return (
      <div className="space-y-6">
        <PageHeader title={<Skeleton className="h-8 w-48" />} actions={null} />
        <ObjectListSkeleton />
      </div>
    );
  }

  if (!bucket) {
    return (
      <div className="space-y-6">
        <PageHeader title={`Bucket not found`} actions={<Button onClick={handleBack}>Back</Button>} />
        <EmptyState
          icon="alert-circle"
          title="Bucket not found"
          description="The bucket you are looking for does not exist or your S3 key cannot see it."
        />
      </div>
    );
  }

  // Has-anything check uses the first page's primitives: folders + files.
  const firstPage = pages?.pages[0];
  const hasFolders = (firstPage?.commonPrefixes?.length ?? 0) > 0;
  const hasFiles = rows.some((r) => r.kind === "file");
  const isEmpty = !objectsLoading && !objectsError && !hasFolders && !hasFiles;

  return (
    <div className="space-y-6">
      <PageHeader
        title={bucketAlias}
        description={prefix ? prefix.replace(/\/$/, "") : undefined}
        badge={federation ? <FederationBadge federation={federation} /> : null}
        actions={
          <div className="flex items-center gap-2 w-full sm:w-auto">
            <Button variant="outline" onClick={handleBack}>
              Back
            </Button>
            <Button variant="ghost" size="sm" onClick={handleRefresh} disabled={objectsLoading}>
              Refresh
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={() => setUploadOpen(true)}
              disabled={objectsLoading}
            >
              Upload
            </Button>
          </div>
        }
      />

      {prefix && (
        <Breadcrumb
          bucketAlias={bucketAlias}
          prefix={prefix}
          onNavigate={(p) =>
            navigate({
              to: "/files/$regionId/b/$bid",
              params: { regionId, bid },
              search: { prefix: p, token: "" },
            })
          }
        />
      )}

      {/* v1.10.0b — "Show all versions" toggle. Only surfaces when
          versioning is supported by the driver AND turned on (enabled
          or suspended) for this bucket — there's no signal to surface
          on an unversioned bucket. Each row's Versions button stays
          available regardless of this toggle; the toggle makes the
          buttons visually prominent and pre-opens the panel for the
          most recently clicked row. */}
      {versioningActive && (
        <div
          className="flex items-center gap-2 text-sm"
          data-testid="show-all-versions-toolbar"
        >
          <Checkbox
            id="show-all-versions"
            checked={showAllVersions}
            onCheckedChange={(checked) => setShowAllVersions(!!checked)}
            data-testid="show-all-versions-toggle"
          />
          <label
            htmlFor="show-all-versions"
            className="cursor-pointer select-none"
          >
            Show all versions including delete markers
          </label>
        </div>
      )}

      {objectsLoading ? (
        <ObjectListSkeleton />
      ) : objectsError ? (
        <EmptyState
          icon="alert-circle"
          title="Can't browse objects"
          description={`Backend returned an error: ${String(objectsError)}`}
        />
      ) : isEmpty ? (
        <EmptyState
          icon="folder-open"
          title={prefix ? "This folder is empty" : "No objects here"}
          description={prefix ? `No objects found in ${prefix}` : "This bucket is empty"}
        />
      ) : (
        <VirtualObjectList
          // Resetting the key on prefix change rebuilds the scroll
          // container — react-virtual's internal state (current scroll
          // offset, item measurements) resets to the top with it. The
          // alternative (imperatively calling scrollTo(0)) leaves a
          // half-tick of old-prefix rows visible.
          key={prefix}
          rows={rows}
          onFolderClick={handleFolderClick}
          onDownload={handleDownload}
          onReachEnd={() => {
            if (hasNextPage && !isFetchingNextPage) {
              fetchNextPage();
            }
          }}
          isFetchingNextPage={isFetchingNextPage}
          selectedKeys={selectedKeys}
          onToggleSelected={toggleSelected}
          onToggleSelectAllVisible={toggleSelectAllVisible}
          allVisibleSelected={allVisibleSelected}
          someVisibleSelected={someVisibleSelected}
          hasSelectableRows={visibleFileKeys.length > 0}
          perRowError={perRowError}
          showVersionsButton={versioningActive}
          highlightVersionsButton={showAllVersions}
          onVersionsClick={(k) => setVersionsForKey(k)}
        />
      )}

      {/* v1.10.0b — per-object version history panel. Mounts when the
          operator clicks "Versions" on any file row. Stays open until
          the operator closes it or navigates folders / buckets. */}
      {versionsForKey && versioningActive && (
        <ObjectVersionsPanel
          regionId={regionId}
          bucket={bid}
          objectKey={versionsForKey}
          onClose={() => setVersionsForKey(null)}
        />
      )}

      {/* v1.10.0b — versioning settings card. Renders at the bottom of
          the page so it sits below the file list (the operator's main
          working surface) but stays one scroll away. The card itself
          handles the "Not supported by this driver" branch internally
          so we can mount it unconditionally. */}
      <VersioningSection regionId={regionId} bucket={bid} />

      {/* v1.10.0c — Object Lock settings card. Layered on top of
          versioning per the S3 spec; the card surfaces three distinct
          branches internally (unsupported / needs-versioning / full
          editor) so we mount it unconditionally and pass the
          versioning state down for the gating decision. */}
      <ObjectLockSection
        regionId={regionId}
        bucket={bid}
        versioningState={versioningState}
      />

      {presignMutation.isError && (
        <ErrorBanner message="Failed to generate download link. Try again." />
      )}

      {/* v1.4.0b: post-batch summary. Surfaces failure count + the
          per-row indicators below; success-only batches silently drop
          off via setDeleteSummary({failures: 0}) since refetch already
          tells the operator "they're gone". */}
      {deleteSummary && deleteSummary.failures > 0 && (
        <ErrorBanner
          message={`${deleteSummary.failures} object${deleteSummary.failures === 1 ? "" : "s"} couldn't be deleted. See the rows marked below for details.`}
        />
      )}

      {/* v1.4.0b: sticky batch action bar. Mounts when ≥1 object is
          selected. Pinned to the bottom of the viewport (fixed) so the
          operator can scroll the virtualized list without losing the
          Delete button. The pointer-events guard on the wrapper lets
          clicks fall through outside the bar's actual button area. */}
      {selectedKeys.size > 0 && (
        <div
          className="pointer-events-none fixed inset-x-0 bottom-0 z-30 flex justify-center px-4 pb-4"
          data-testid="batch-action-bar"
        >
          <div className="pointer-events-auto flex items-center gap-3 rounded-lg border bg-card shadow-lg px-4 py-3">
            <span className="text-sm font-medium" data-testid="batch-action-bar-count">
              {selectedKeys.size} selected
            </span>
            <Button
              variant="destructive"
              size="sm"
              onClick={() => setDeleteConfirmOpen(true)}
              disabled={isDeleting}
              data-testid="batch-action-bar-delete"
            >
              {isDeleting ? "Deleting…" : `Delete ${selectedKeys.size} object${selectedKeys.size === 1 ? "" : "s"}`}
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={clearSelection}
              disabled={isDeleting}
              data-testid="batch-action-bar-cancel"
            >
              Cancel
            </Button>
          </div>
        </div>
      )}

      {/* Confirmation dialog. Uses AlertDialog (same pattern as the
          per-bucket / per-key delete confirms) — no type-name-to-confirm
          here because object batches are higher-volume + typically
          recoverable from backup if the bucket has versioning; the
          delete-bucket flow keeps the harder gate. */}
      <AlertDialog
        open={deleteConfirmOpen}
        onOpenChange={(next) => {
          if (!next && !isDeleting) setDeleteConfirmOpen(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Permanently delete {selectedKeys.size} object{selectedKeys.size === 1 ? "" : "s"}?
            </AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone unless the bucket has versioning enabled.
              Per-object failures will be surfaced inline; succeeded objects drop from the list.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel
              onClick={() => setDeleteConfirmOpen(false)}
              disabled={isDeleting}
            >
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={handleBatchDelete}
              disabled={isDeleting}
              data-testid="batch-delete-confirm-action"
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {isDeleting ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <UploadDialog
        open={uploadOpen}
        onOpenChange={setUploadOpen}
        regionId={regionId}
        bid={bid}
        prefix={prefix}
        onSuccess={handleUploadSuccess}
      />
    </div>
  );
}

// VirtualObjectList renders the virtualized scroll container + rows.
// Sticky header sits above; rows render inside a fixed-height
// scrollable region that the virtualizer drives. Each row is
// absolutely positioned by transform so the visible window is
// constant-DOM-cost regardless of `rows.length`.
// useIsMobile re-renders the consumer when the viewport crosses the
// MOBILE_BREAKPOINT_PX threshold. Used by the bucket browser to swap
// table row layout for a stacked card layout at narrow widths. Returns
// false in SSR / jsdom contexts so existing tests render the desktop
// layout (the breakpoint switch is covered by a dedicated mobile test).
function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = useState<boolean>(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return false;
    }
    return window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`).matches;
  });
  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
      return;
    }
    const mql = window.matchMedia(`(max-width: ${MOBILE_BREAKPOINT_PX - 1}px)`);
    const handler = (e: MediaQueryListEvent) => setIsMobile(e.matches);
    if (typeof mql.addEventListener === "function") {
      mql.addEventListener("change", handler);
      return () => mql.removeEventListener("change", handler);
    }
    // Older Safari fallback
    mql.addListener(handler);
    return () => mql.removeListener(handler);
  }, []);
  return isMobile;
}

function VirtualObjectList({
  rows,
  onFolderClick,
  onDownload,
  onReachEnd,
  isFetchingNextPage,
  selectedKeys,
  onToggleSelected,
  onToggleSelectAllVisible,
  allVisibleSelected,
  someVisibleSelected,
  hasSelectableRows,
  perRowError,
  showVersionsButton,
  highlightVersionsButton,
  onVersionsClick,
}: {
  rows: VirtualRow[];
  onFolderClick: (prefix: string) => void;
  onDownload: (key: string) => void;
  onReachEnd: () => void;
  isFetchingNextPage: boolean;
  selectedKeys: Set<string>;
  onToggleSelected: (key: string) => void;
  onToggleSelectAllVisible: () => void;
  allVisibleSelected: boolean;
  someVisibleSelected: boolean;
  hasSelectableRows: boolean;
  perRowError: Map<string, string>;
  // v1.10.0b: per-row Versions button gating.
  //   - showVersionsButton: render the button at all (true when the
  //     bucket has versioning enabled or suspended AND the driver
  //     supports it — otherwise the button would open an empty panel)
  //   - highlightVersionsButton: render it as a solid outline button
  //     instead of ghost (reflects the "Show all versions" toggle)
  //   - onVersionsClick(key): open the versions panel for the key
  showVersionsButton: boolean;
  highlightVersionsButton: boolean;
  onVersionsClick: (key: string) => void;
}) {
  const parentRef = useRef<HTMLDivElement | null>(null);
  const isMobile = useIsMobile();
  const rowHeight = isMobile ? ROW_HEIGHT_MOBILE : ROW_HEIGHT_DESKTOP;

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowHeight,
    overscan: 8,
    // initialRect gives the virtualizer a non-zero viewport before
    // the parent measures (first paint + jsdom in tests). Without
    // this jsdom reports 0×0 and the virtualizer renders zero items,
    // making unit tests on row content impossible. The browser's
    // real measurement overrides this on second paint via the
    // observeElementRect callback default.
    initialRect: { width: 800, height: 600 },
    // In jsdom (vitest) ResizeObserver is mocked inert, so the
    // default observeElementRect never fires the size callback —
    // leaving total-size at 0 and rendering zero items. Detect the
    // test env and short-circuit with a synchronous initialRect
    // delivery so the virtualizer renders without measurement.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ...((typeof navigator !== "undefined" && /jsdom/i.test((navigator as any).userAgent || ""))
      ? {
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          observeElementRect: (_inst: any, cb: any) => {
            cb({ width: 800, height: 600 });
            return () => {};
          },
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          observeElementOffset: (_inst: any, cb: any) => {
            cb(0, false);
            return () => {};
          },
        }
      : {}),
  });

  // Infinite-scroll trigger: as soon as the loadMoreSentinel becomes
  // visible (or any item in the last 8 enters the window), call
  // onReachEnd to fetch the next page. Throttle on isFetchingNextPage
  // so we don't double-fire while a fetch is in flight.
  const items = virtualizer.getVirtualItems();
  useEffect(() => {
    if (!items.length) return;
    const last = items[items.length - 1];
    if (!last) return;
    if (last.index >= rows.length - 1 && !isFetchingNextPage) {
      onReachEnd();
    }
  }, [items, rows.length, isFetchingNextPage, onReachEnd]);

  return (
    <div
      className="rounded-lg border bg-card overflow-hidden"
      data-testid="virtual-object-list"
      data-layout={isMobile ? "card" : "table"}
    >
      {/* Sticky header row. Uses the same column widths as the body so
          the eye doesn't jump between header + first body row. v1.4.0b:
          leading 40px column hosts the per-row checkbox + the
          select-all-visible toggle in the header. Folder rows leave it
          blank — selection is files-only by design.

          v1.8.0e: header is hidden on mobile (<640px) where rows
          render as stacked cards with size + date below the name —
          column headers are meaningless without column alignment. The
          select-all checkbox lives in the mobile action bar above the
          list instead (see select-all-mobile below). */}
      <div className="hidden sm:grid grid-cols-[40px_1fr_140px_160px_220px] gap-2 border-b bg-muted/30 px-3 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
        <div className="flex items-center" data-testid="select-all-cell">
          <Checkbox
            checked={allVisibleSelected}
            indeterminate={someVisibleSelected}
            onCheckedChange={onToggleSelectAllVisible}
            disabled={!hasSelectableRows}
            aria-label="Select all visible files"
            data-testid="select-all-visible"
          />
        </div>
        <div>Name</div>
        <div className="text-right">Size</div>
        <div className="text-right">Last modified</div>
        <div className="text-right">Actions</div>
      </div>
      {/* Mobile select-all strip: dedicated row above the card list
          so the operator still has bulk-select access without the
          grid header. ≥44px tap target (the strip is full-width). */}
      <div
        className="sm:hidden flex items-center gap-3 border-b bg-muted/30 px-3 py-3 text-xs font-medium uppercase tracking-wider text-muted-foreground"
        data-testid="select-all-cell-mobile"
      >
        <Checkbox
          checked={allVisibleSelected}
          indeterminate={someVisibleSelected}
          onCheckedChange={onToggleSelectAllVisible}
          disabled={!hasSelectableRows}
          aria-label="Select all visible files"
          data-testid="select-all-visible-mobile"
          className="h-5 w-5"
        />
        <span>{hasSelectableRows ? "Select all visible" : "No selectable files"}</span>
      </div>

      <div
        ref={parentRef}
        // Fixed-ish viewport height so the virtualizer has something
        // to scroll inside; capped at viewport height minus chrome
        // (~360px) and minimum 480px so small screens still see a
        // useful window.
        className="overflow-y-auto"
        style={{ height: "min(70vh, 720px)", minHeight: "480px" }}
        data-testid="virtual-object-list-scroll"
      >
        <div
          style={{
            height: virtualizer.getTotalSize(),
            position: "relative",
            width: "100%",
          }}
        >
          {items.map((virtual) => {
            const row = rows[virtual.index];
            if (!row) return null;
            const isSelectedFile = row.kind === "file" && selectedKeys.has(row.key);
            const fileError = row.kind === "file" ? perRowError.get(row.key) : undefined;
            const layoutClass = isMobile
              ? "flex items-center gap-3"
              : "grid grid-cols-[40px_1fr_140px_160px_220px] gap-2 items-center";
            return (
              <div
                key={virtual.key}
                data-index={virtual.index}
                ref={virtualizer.measureElement}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  transform: `translateY(${virtual.start}px)`,
                  height: rowHeight,
                }}
                className={`${layoutClass} border-b px-3 hover:bg-muted/40 ${
                  isSelectedFile ? "bg-primary/5" : ""
                } ${fileError ? "ring-1 ring-inset ring-destructive/50" : ""}`}
                data-row-kind={row.kind}
              >
                {row.kind === "folder" && (
                  <>
                    {/* Empty cell — folders are not selectable. Hidden
                        on mobile (no checkbox column there). */}
                    <div className="hidden sm:block" />
                    <FolderRow
                      prefix={row.key}
                      onFolderClick={onFolderClick}
                      isMobile={isMobile}
                    />
                  </>
                )}
                {row.kind === "file" && (
                  <>
                    <div className="flex items-center flex-shrink-0">
                      <Checkbox
                        checked={isSelectedFile}
                        onCheckedChange={() => onToggleSelected(row.key)}
                        aria-label={`Select ${row.key}`}
                        data-testid={`file-select-${row.key}`}
                        className={isMobile ? "h-5 w-5" : undefined}
                      />
                    </div>
                    <FileRow
                      obj={row.obj}
                      onDownload={onDownload}
                      errorMessage={fileError}
                      isMobile={isMobile}
                      showVersionsButton={showVersionsButton}
                      highlightVersionsButton={highlightVersionsButton}
                      onVersionsClick={onVersionsClick}
                    />
                  </>
                )}
                {row.kind === "loadMoreSentinel" && (
                  <div
                    className={`text-center text-xs text-muted-foreground ${
                      isMobile ? "w-full" : "col-span-5"
                    }`}
                  >
                    {isFetchingNextPage ? "Loading more..." : "Scroll for more"}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

// FolderRow renders one folder (commonPrefix). Strips the trailing
// slash for display so "raw/" reads as "raw" without trailing
// punctuation; the navigation handler still receives the full prefix
// (S3's expected shape).
//
// v1.8.0e: on mobile (isMobile=true) the size + last-modified columns
// collapse and the folder is a single full-width tap target. The
// folder button itself stretches to fill the row so the tap area is
// the entire 76px-tall row, not just the icon + label.
function FolderRow({
  prefix,
  onFolderClick,
  isMobile,
}: {
  prefix: string;
  onFolderClick: (p: string) => void;
  isMobile: boolean;
}) {
  const trimmed = prefix.endsWith("/") ? prefix.slice(0, -1) : prefix;
  const displayName = trimmed.split("/").pop() || trimmed;

  if (isMobile) {
    return (
      <button
        type="button"
        onClick={() => onFolderClick(prefix)}
        className="flex items-center gap-3 text-left w-full overflow-hidden min-h-11"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="currentColor"
          className="h-6 w-6 text-blue-500 flex-shrink-0"
        >
          <path d="M19.5 21a3 3 0 0 0 3-3v-4.5a3 3 0 0 0-3-3h-15a3 3 0 0 0-3 3V18a3 3 0 0 0 3 3h15ZM1.5 10.146V6a3 3 0 0 1 3-3h5.379a2.25 2.25 0 0 1 1.59.659l2.122 2.121c.14.141.331.22.53.22H19.5a3 3 0 0 1 3 3v1.146A4.483 4.483 0 0 0 19.5 9h-15a4.483 4.483 0 0 0-3 1.146Z" />
        </svg>
        <div className="flex flex-col min-w-0 flex-1">
          <span className="font-medium truncate">{displayName}</span>
          <span className="text-xs text-muted-foreground">Folder</span>
        </div>
      </button>
    );
  }

  return (
    <>
      <button
        type="button"
        onClick={() => onFolderClick(prefix)}
        className="flex items-center gap-2 text-left w-full overflow-hidden"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="currentColor"
          className="h-5 w-5 text-blue-500 flex-shrink-0"
        >
          <path d="M19.5 21a3 3 0 0 0 3-3v-4.5a3 3 0 0 0-3-3h-15a3 3 0 0 0-3 3V18a3 3 0 0 0 3 3h15ZM1.5 10.146V6a3 3 0 0 1 3-3h5.379a2.25 2.25 0 0 1 1.59.659l2.122 2.121c.14.141.331.22.53.22H19.5a3 3 0 0 1 3 3v1.146A4.483 4.483 0 0 0 19.5 9h-15a4.483 4.483 0 0 0-3 1.146Z" />
        </svg>
        <span className="font-medium truncate">{displayName}</span>
      </button>
      <div className="text-right text-muted-foreground">—</div>
      <div className="text-right text-muted-foreground">—</div>
      <div />
    </>
  );
}

// FileRow renders one S3 object. Size + last-modified columns mirror
// the previous table cells. Download is the single per-row action.
// v1.4.0b: errorMessage surfaces a per-row failure indicator after a
// partial-failure batch delete — the row stays in the list (so the
// operator can retry), the icon turns red, and the title attribute
// carries the failure reason for hover inspection.
function FileRow({
  obj,
  onDownload,
  errorMessage,
  isMobile,
  showVersionsButton,
  highlightVersionsButton,
  onVersionsClick,
}: {
  obj: ObjectInfo;
  onDownload: (key: string) => void;
  errorMessage?: string;
  isMobile: boolean;
  // v1.10.0b: Versions button surfaces per-row when the bucket has
  // versioning on AND the driver supports it. Hidden otherwise so the
  // operator isn't tempted by a dead control.
  showVersionsButton: boolean;
  highlightVersionsButton: boolean;
  onVersionsClick: (key: string) => void;
}) {
  const displayName = obj.key.split("/").pop() || obj.key;
  const sizeText = humanizeBytes(obj.size);
  const modifiedText = obj.last_modified ? humanizeTime(obj.last_modified) : null;

  // v1.8.0e: mobile card layout — name on row 1, size + modified
  // joined by a separator on row 2 below; Download is an icon button
  // pinned to the right with a 44×44 hit area. Folder/select column
  // gap takes the leading 12px so the visual hierarchy reads
  // checkbox | (name + metadata) | download.
  if (isMobile) {
    return (
      <>
        <div className="flex flex-col min-w-0 flex-1 overflow-hidden">
          <div className="flex items-center gap-2 min-w-0">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="currentColor"
              className={`h-5 w-5 flex-shrink-0 ${errorMessage ? "text-destructive" : "text-gray-500"}`}
              aria-hidden="true"
            >
              <path d="M19.5 14.25v-9a3 3 0 0 0-3-3H7.5a3 3 0 0 0-3 3v13.5a3 3 0 0 0 3 3h9a3 3 0 0 0 3-3Z" />
              <path d="M16.5 7.5h-9v9h9a1.5 1.5 0 0 0 1.5-1.5v-6Z" fillOpacity={0} stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span
              className="font-medium truncate text-sm"
              title={errorMessage ? `${obj.key} — ${errorMessage}` : obj.key}
              data-testid={errorMessage ? `file-row-error-${obj.key}` : undefined}
            >
              {displayName}
            </span>
            {errorMessage && (
              <span
                className="text-xs text-destructive whitespace-nowrap flex-shrink-0"
                data-testid={`file-row-error-label-${obj.key}`}
              >
                delete failed
              </span>
            )}
          </div>
          <div className="mt-0.5 text-xs text-muted-foreground tabular-nums truncate pl-7">
            {sizeText}
            {modifiedText && <span className="mx-1.5">&middot;</span>}
            {modifiedText}
          </div>
        </div>
        {showVersionsButton && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onVersionsClick(obj.key);
            }}
            aria-label={`Versions of ${displayName}`}
            // Mobile Versions icon — clock-style glyph, 44x44 hit
            // area to match the Download button below. Visible only
            // when versioning is on for the bucket + driver supports
            // it; otherwise the row stays single-action.
            className={`inline-flex h-11 w-11 -my-2 flex-shrink-0 items-center justify-center rounded-md ${highlightVersionsButton ? "bg-muted text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground"}`}
            data-testid={`file-versions-${obj.key}`}
          >
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              className="h-5 w-5"
              aria-hidden="true"
            >
              <circle cx="12" cy="12" r="9" />
              <polyline points="12 7 12 12 15 14" />
            </svg>
          </button>
        )}
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onDownload(obj.key);
          }}
          aria-label={`Download ${displayName}`}
          // 44×44 minimum tap target. Negative margin keeps the row's
          // 76px height intact while letting the hit area exceed the
          // visible icon footprint.
          className="inline-flex h-11 w-11 -my-2 flex-shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="h-5 w-5"
            aria-hidden="true"
          >
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <polyline points="7 10 12 15 17 10" />
            <line x1="12" x2="12" y1="15" y2="3" />
          </svg>
        </button>
      </>
    );
  }

  return (
    <>
      <div className="flex items-center gap-2 overflow-hidden">
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="currentColor"
          className={`h-5 w-5 flex-shrink-0 ${errorMessage ? "text-destructive" : "text-gray-500"}`}
          aria-hidden="true"
        >
          <path d="M19.5 14.25v-9a3 3 0 0 0-3-3H7.5a3 3 0 0 0-3 3v13.5a3 3 0 0 0 3 3h9a3 3 0 0 0 3-3Z" />
          <path d="M16.5 7.5h-9v9h9a1.5 1.5 0 0 0 1.5-1.5v-6Z" fillOpacity={0} stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span
          className="font-medium truncate"
          title={errorMessage ? `${obj.key} — ${errorMessage}` : obj.key}
          data-testid={errorMessage ? `file-row-error-${obj.key}` : undefined}
        >
          {displayName}
        </span>
        {errorMessage && (
          <span
            className="text-xs text-destructive whitespace-nowrap"
            data-testid={`file-row-error-label-${obj.key}`}
          >
            delete failed
          </span>
        )}
      </div>
      <div className="text-right tabular-nums text-sm">{sizeText}</div>
      <div className="text-right tabular-nums text-sm">
        {modifiedText ?? "—"}
      </div>
      <div className="flex items-center justify-end gap-1">
        {showVersionsButton && (
          <Button
            variant={highlightVersionsButton ? "outline" : "ghost"}
            size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onVersionsClick(obj.key);
            }}
            data-testid={`file-versions-${obj.key}`}
          >
            Versions
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          onClick={(e) => {
            e.stopPropagation();
            onDownload(obj.key);
          }}
        >
          Download
        </Button>
      </div>
    </>
  );
}

function PageHeader({
  title,
  description,
  badge,
  actions,
}: {
  title: React.ReactNode;
  description?: string;
  // v1.6.0e: optional badge slot rendered under the title — used by
  // the bucket browser to surface the federation membership pill.
  badge?: React.ReactNode;
  actions?: React.ReactNode;
}) {
  return (
    <header className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="text-2xl sm:text-3xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-muted-foreground mt-1">{description}</p>}
        {badge && <div className="mt-2">{badge}</div>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </header>
  );
}

// FederationBadge renders the v1.6.0e "this bucket is federated" pill
// under the bucket title. Amber background per the spec; the click
// navigates to the federation detail route. Counts roll the replicas
// list into "N replicas, M in-sync" so the operator sees the federation
// shape at a glance. Health values originate from the v1.6.0b engine
// via the federation record's `replicas[i].health` field.
function FederationBadge({ federation }: { federation: FederatedBucket }) {
  const navigate = useNavigate();
  const replicaCount = federation.replicas.length;
  const inSyncCount = federation.replicas.filter(
    (rep) => (rep.health ?? "in-sync") === "in-sync",
  ).length;
  const tooltip = `This bucket is part of federation '${federation.name}'. Manage replicas →`;
  return (
    <button
      type="button"
      onClick={() =>
        navigate({
          to: "/files/federated-buckets/$id",
          params: { id: federation.id },
        })
      }
      title={tooltip}
      data-testid="federation-badge"
      className="inline-flex items-center gap-2 rounded-md border border-amber-300 bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-900 hover:bg-amber-100 dark:border-amber-700/60 dark:bg-amber-900/30 dark:text-amber-100 dark:hover:bg-amber-900/50"
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        className="h-3.5 w-3.5"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="3" />
        <circle cx="4" cy="6" r="2" />
        <circle cx="20" cy="6" r="2" />
        <circle cx="4" cy="18" r="2" />
        <circle cx="20" cy="18" r="2" />
        <path d="M6 6h12" />
        <path d="M6 18h12" />
        <path d="M9 12h6" />
      </svg>
      <span>
        Federated &middot; {replicaCount} replica{replicaCount === 1 ? "" : "s"},{" "}
        {inSyncCount} in-sync
      </span>
    </button>
  );
}

// Breadcrumb renders bucketAlias > raw > broadcom-docid (clickable),
// per v1.3.0c.1: each crumb navigates to the prefix ending at that
// segment. Folder prefixes from S3 always end in "/" so the click
// emits e.g. "raw/" or "raw/broadcom-docid/".
function Breadcrumb({
  bucketAlias,
  prefix,
  onNavigate,
}: {
  bucketAlias: string;
  prefix: string;
  onNavigate: (p: string) => void;
}) {
  const parts = prefix.split("/").filter(Boolean);
  const parentPrefix = parts.length > 1 ? parts.slice(0, parts.length - 1).join("/") + "/" : "";

  return (
    <div className="flex flex-col gap-2">
      <nav className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
        <button onClick={() => onNavigate("")} className="hover:text-foreground font-medium">
          {bucketAlias}
        </button>
        {parts.map((part, idx) => {
          const path = parts.slice(0, idx + 1).join("/") + "/";
          const isLast = idx === parts.length - 1;
          return (
            <span key={path} className="flex items-center gap-2">
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="h-4 w-4"
              >
                <path d="m9 18 6-6-6-6" />
              </svg>
              {isLast ? (
                <span className="text-foreground">{part}</span>
              ) : (
                <button onClick={() => onNavigate(path)} className="hover:text-foreground">
                  {part}
                </button>
              )}
            </span>
          );
        })}
      </nav>
      <button
        type="button"
        onClick={() => onNavigate(parentPrefix)}
        className="self-start text-xs text-muted-foreground hover:text-foreground inline-flex items-center gap-1"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="h-3.5 w-3.5"
        >
          <path d="m15 18-6-6 6-6" />
        </svg>
        Up to parent folder
      </button>
    </div>
  );
}

// ObjectListSkeleton renders an N-row skeleton that visually matches
// the virtualized list's resting state — same column widths, same
// fixed row height, same sticky header — so the page doesn't jump
// when the data lands.
function ObjectListSkeleton() {
  return (
    <div className="rounded-lg border bg-card overflow-hidden">
      {/* v1.8.0e: header hidden on mobile to match the live layout. */}
      <div className="hidden sm:grid grid-cols-[40px_1fr_140px_160px_220px] gap-2 border-b bg-muted/30 px-3 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
        <div />
        <div>Name</div>
        <div className="text-right">Size</div>
        <div className="text-right">Last modified</div>
        <div className="text-right">Actions</div>
      </div>
      <div className="divide-y">
        {[...Array(8)].map((_, i) => (
          <div
            key={i}
            className="hidden sm:grid grid-cols-[40px_1fr_140px_160px_220px] gap-2 items-center px-3"
            style={{ height: ROW_HEIGHT_DESKTOP }}
          >
            <Skeleton className="h-4 w-4" />
            <Skeleton className="h-4 w-64" />
            <Skeleton className="h-4 w-16 ml-auto" />
            <Skeleton className="h-4 w-32 ml-auto" />
            <Skeleton className="h-8 w-20 ml-auto" />
          </div>
        ))}
        {[...Array(8)].map((_, i) => (
          <div
            key={`m-${i}`}
            className="sm:hidden flex items-center gap-3 px-3"
            style={{ height: ROW_HEIGHT_MOBILE }}
          >
            <Skeleton className="h-5 w-5 flex-shrink-0" />
            <div className="flex-1 space-y-1">
              <Skeleton className="h-4 w-40" />
              <Skeleton className="h-3 w-24" />
            </div>
            <Skeleton className="h-11 w-11 flex-shrink-0" />
          </div>
        ))}
      </div>
    </div>
  );
}
