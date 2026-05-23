// v1.3.0c.1: bucket-browser folder navigation. The handler now returns
// CommonPrefixes alongside Objects when delimiter="/" (the default),
// and clicking a folder row drills in by re-listing with prefix=folder.
// These tests assert the rendering split (folders first, files after)
// and that the click emits the right navigate() call.
//
// v1.4.0a: the bucket browser is now virtualized via @tanstack/react-virtual
// and uses useUserRegionObjectsInfinite (an infinite-query that walks
// continuation tokens). Tests target the rendered DOM rather than
// table semantics — the virtualizer uses div-based rows instead of
// <tr> nodes.

const navigateMock = vi.fn();

// v1.4.0a: useLocation feeds the prefix/token reads in the route
// component; mocked here so the test doesn't need a full router
// context. Tests that need a specific prefix override
// locationSearchStr via the helper at the top of the file.
let locationSearchStr = "";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children, to }: any) => (
      <a data-testid="link-wrapper" href={typeof to === "string" ? to : "#"}>
        {children}
      </a>
    ),
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
      useParams: () => ({ regionId: "lsi-region-id", bid: "lsi" }),
    }),
    useNavigate: () => navigateMock,
    useLocation: () => ({ searchStr: locationSearchStr }),
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserRegionBuckets: vi.fn(),
    useUserRegionObjectsInfinite: vi.fn(),
    useUserRegionPresignGet: vi.fn(),
    useFederationByTarget: vi.fn(),
    // v1.10.0b — versioning hooks. Default mocks land in beforeEach.
    useBucketVersioning: vi.fn(),
    usePutBucketVersioning: vi.fn(),
    useObjectVersions: vi.fn(),
    useDeleteObjectVersion: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/$regionId/b/$bid";
import {
  useBucketVersioning,
  useDeleteObjectVersion,
  useFederationByTarget,
  useObjectVersions,
  usePutBucketVersioning,
  useUserRegionBuckets,
  useUserRegionObjectsInfinite,
  useUserRegionPresignGet,
} from "@/shared/api/queries";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderBucket() {
  const Component = (Route as unknown as { component: React.ComponentType })
    .component;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>,
  );
}

// makeInfiniteResult wraps a single-page result in the
// useInfiniteQuery-shaped envelope the route consumes (data.pages[]).
function makeInfiniteResult(page: {
  objects?: Array<{ key: string; size: number; last_modified?: string }>;
  commonPrefixes?: string[];
  isTruncated?: boolean;
  nextContinuation?: string;
}) {
  return {
    data: {
      pages: [
        {
          objects: page.objects ?? [],
          commonPrefixes: page.commonPrefixes ?? [],
          isTruncated: page.isTruncated ?? false,
          nextContinuation: page.nextContinuation,
        },
      ],
      pageParams: [""],
    },
    isLoading: false,
    isFetchingNextPage: false,
    error: null,
    refetch: vi.fn(),
    fetchNextPage: vi.fn(),
    hasNextPage: page.isTruncated ?? false,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  // v1.4.0a: the hook now returns an envelope wrapping the bucket
  // list + a per-driver capability flag.
  vi.mocked(useUserRegionBuckets).mockReturnValue({
    data: {
      buckets: [{ id: "lsi", aliases: ["lsi"], objects: 0, bytes: 0, unfinishedUploads: 0 }],
      perBucketStatsAvailable: false,
    },
    isLoading: false,
    error: null,
  } as any);
  vi.mocked(useUserRegionPresignGet).mockReturnValue({
    mutateAsync: vi.fn(),
    isError: false,
  } as any);
  // v1.6.0e: default the federation-lookup hook to "no federation"
  // (null data). Tests that need a federation override this per case.
  vi.mocked(useFederationByTarget).mockReturnValue({
    data: null,
    isLoading: false,
    error: null,
  } as any);

  // v1.10.0b: default versioning hooks land in the "supported but
  // never enabled" state — the toggle + per-row Versions button don't
  // surface, the section renders the disabled-but-supported branch.
  // Tests that exercise versioning behavior override these per case.
  vi.mocked(useBucketVersioning).mockReturnValue({
    data: { status: "disabled", supported: true },
    isLoading: false,
    error: null,
  } as any);
  vi.mocked(usePutBucketVersioning).mockReturnValue({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
  } as any);
  vi.mocked(useObjectVersions).mockReturnValue({
    data: { versions: [] },
    isLoading: false,
    error: null,
    refetch: vi.fn(),
  } as any);
  vi.mocked(useDeleteObjectVersion).mockReturnValue({
    mutate: vi.fn(),
    isPending: false,
    isError: false,
    variables: undefined,
  } as any);
});

describe("/files/$regionId/b/$bid — folder navigation (v1.3.0c.1)", () => {
  it("renders folder rows from commonPrefixes alongside file rows from objects", () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "readme.txt", size: 42, last_modified: "2026-05-21T00:00:00Z" }],
        commonPrefixes: ["index/", "raw/"],
      }) as any,
    );

    renderBucket();

    // Both folders render.
    expect(screen.getByText("index")).toBeInTheDocument();
    expect(screen.getByText("raw")).toBeInTheDocument();
    // File renders too.
    expect(screen.getByText("readme.txt")).toBeInTheDocument();
  });

  it("clicking a folder navigates to the same route with the folder's full prefix", async () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [],
        commonPrefixes: ["raw/"],
      }) as any,
    );

    renderBucket();
    await userEvent.click(screen.getByText("raw"));

    expect(navigateMock).toHaveBeenCalledWith(
      expect.objectContaining({
        to: "/files/$regionId/b/$bid",
        params: { regionId: "lsi-region-id", bid: "lsi" },
        search: expect.objectContaining({ prefix: "raw/" }),
      }),
    );
  });

  it("shows the 'This folder is empty' state when both objects and commonPrefixes are empty inside a prefix", () => {
    // Force prefix=raw/ via the mocked useLocation.
    locationSearchStr = "?prefix=raw/";

    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [],
        commonPrefixes: [],
      }) as any,
    );

    renderBucket();
    expect(screen.getByText(/This folder is empty/i)).toBeInTheDocument();

    locationSearchStr = "";
  });
});

// v1.4.0a: virtualized scrolling assertion. We can't visually scroll
// in jsdom (no real height / layout), but we can assert the scroll
// container is in the DOM and that a list of N rows produces a
// virtualizer with N items the test can count.
describe("/files/$regionId/b/$bid — virtualization (v1.4.0a)", () => {
  it("mounts the virtual scroll container", () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [
          { key: "a.txt", size: 10 },
          { key: "b.txt", size: 20 },
        ],
      }) as any,
    );

    renderBucket();

    // The scroll container test hook is present.
    expect(screen.getByTestId("virtual-object-list-scroll")).toBeInTheDocument();
  });
});

// v1.4.0b: batch object operations — select + delete. Covers the four
// scope acceptance gates:
//
//   1. Select-all visible toggles every file row (folders excluded).
//   2. Delete fires N parallel DELETE calls.
//   3. Partial failure surfaces a per-row error indicator on the
//      survivors AND leaves them selected for retry.
//   4. Cancel clears the selection.
describe("/files/$regionId/b/$bid — batch operations (v1.4.0b)", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  // Restore fetch between tests so other suites' assumptions hold.
  // Using afterEach via finally inside each test would also work but
  // this is cleaner across the suite.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const restoreFetch = () => {
    globalThis.fetch = originalFetch;
  };

  it("select-all-visible checkbox ticks every file row but not folder rows", async () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [
          { key: "a.txt", size: 10 },
          { key: "b.txt", size: 20 },
          { key: "c.txt", size: 30 },
        ],
        commonPrefixes: ["raw/"],
      }) as any,
    );

    renderBucket();

    // No action bar yet.
    expect(screen.queryByTestId("batch-action-bar")).not.toBeInTheDocument();

    // Click select-all-visible.
    await userEvent.click(screen.getByTestId("select-all-visible"));

    // Action bar mounts with count = 3 (folder NOT selected).
    expect(screen.getByTestId("batch-action-bar")).toBeInTheDocument();
    expect(screen.getByTestId("batch-action-bar-count")).toHaveTextContent("3 selected");
  });

  it("delete confirm fans out N DELETE requests in parallel", async () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [
          { key: "a.txt", size: 10 },
          { key: "b.txt", size: 20 },
          { key: "c.txt", size: 30 },
        ],
      }) as any,
    );

    // Stub fetch to count + verify the URL shape.
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(null, { status: 204 }),
    );
    globalThis.fetch = fetchMock as any;

    renderBucket();

    // Select all three files, open confirm, confirm.
    await userEvent.click(screen.getByTestId("select-all-visible"));
    await userEvent.click(screen.getByTestId("batch-action-bar-delete"));
    await userEvent.click(screen.getByTestId("batch-delete-confirm-action"));

    // Three DELETEs to the per-key endpoint.
    expect(fetchMock).toHaveBeenCalledTimes(3);
    for (const call of fetchMock.mock.calls) {
      const [url, init] = call as [string, RequestInit];
      expect(init.method).toBe("DELETE");
      expect(url).toMatch(/\/api\/v1\/user\/regions\/lsi-region-id\/buckets\/lsi\/objects\//);
    }
    // Each of a.txt / b.txt / c.txt fired exactly once.
    const urls = fetchMock.mock.calls.map((c: any) => c[0] as string);
    expect(urls.some((u) => u.endsWith("/objects/a.txt"))).toBe(true);
    expect(urls.some((u) => u.endsWith("/objects/b.txt"))).toBe(true);
    expect(urls.some((u) => u.endsWith("/objects/c.txt"))).toBe(true);

    restoreFetch();
  });

  it("partial failure surfaces per-row error indicators AND keeps failed rows selected", async () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [
          { key: "ok.txt", size: 10 },
          { key: "boom.txt", size: 20 },
        ],
      }) as any,
    );

    // ok.txt → 204, boom.txt → 500.
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url.endsWith("/objects/boom.txt")) {
        return new Response(JSON.stringify({ message: "backend exploded" }), {
          status: 500,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(null, { status: 204 });
    });
    globalThis.fetch = fetchMock as any;

    renderBucket();
    await userEvent.click(screen.getByTestId("select-all-visible"));
    await userEvent.click(screen.getByTestId("batch-action-bar-delete"));
    await userEvent.click(screen.getByTestId("batch-delete-confirm-action"));

    // The per-row error label appears for boom.txt only.
    expect(await screen.findByTestId("file-row-error-label-boom.txt")).toBeInTheDocument();
    expect(screen.queryByTestId("file-row-error-label-ok.txt")).not.toBeInTheDocument();

    // The action bar stays mounted with count = 1 (the survivor).
    expect(screen.getByTestId("batch-action-bar-count")).toHaveTextContent("1 selected");

    restoreFetch();
  });

  it("cancel clears the selection without firing any DELETEs", async () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [
          { key: "a.txt", size: 10 },
          { key: "b.txt", size: 20 },
        ],
      }) as any,
    );

    const fetchMock = vi.fn();
    globalThis.fetch = fetchMock as any;

    renderBucket();
    await userEvent.click(screen.getByTestId("select-all-visible"));
    expect(screen.getByTestId("batch-action-bar")).toBeInTheDocument();

    await userEvent.click(screen.getByTestId("batch-action-bar-cancel"));

    expect(screen.queryByTestId("batch-action-bar")).not.toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();

    restoreFetch();
  });
});

// v1.6.0e: federation badge + reverse-lookup. The bucket browser calls
// useFederationByTarget(regionId, bucketAlias) on every render; when a
// federation matches we render a badge below the title that navigates
// to the federation detail route. When the hook returns null, the
// badge is absent — no extra DOM, no spurious queries.
describe("/files/$regionId/b/$bid — federation badge (v1.6.0e)", () => {
  it("renders the federation badge when the hook returns a federation", () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "a.txt", size: 10 }],
      }) as any,
    );
    vi.mocked(useFederationByTarget).mockReturnValue({
      data: {
        id: "fed-123",
        ownerUserId: "matthew",
        name: "lsi",
        primary: { regionId: "lsi-region-id", bucket: "lsi" },
        replicas: [
          { regionId: "r2", bucket: "lsi", health: "in-sync" },
          { regionId: "r3", bucket: "lsi", health: "lagging" },
          { regionId: "r4", bucket: "lsi", health: "in-sync" },
        ],
        policy: {
          syncMode: "continuous",
          lagAlertSec: 300,
          writeQuorum: 1,
        },
        createdAt: "2026-05-20T00:00:00Z",
        updatedAt: "2026-05-22T00:00:00Z",
      },
      isLoading: false,
      error: null,
    } as any);

    renderBucket();

    const badge = screen.getByTestId("federation-badge");
    expect(badge).toBeInTheDocument();
    // The badge surfaces the replica + in-sync count from the federation
    // record's `replicas[i].health` field.
    expect(badge).toHaveTextContent(/Federated/);
    expect(badge).toHaveTextContent(/3 replicas/);
    expect(badge).toHaveTextContent(/2 in-sync/);
    // Hover tooltip name + manage prompt.
    expect(badge.getAttribute("title")).toMatch(/federation 'lsi'/i);
    expect(badge.getAttribute("title")).toMatch(/Manage replicas/i);
  });

  it("renders nothing when the hook returns null (bucket isn't federated)", () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "a.txt", size: 10 }],
      }) as any,
    );
    vi.mocked(useFederationByTarget).mockReturnValue({
      data: null,
      isLoading: false,
      error: null,
    } as any);

    renderBucket();

    expect(screen.queryByTestId("federation-badge")).not.toBeInTheDocument();
  });

  it("clicking the badge navigates to /files/federated-buckets/$id with the federation ID", async () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "a.txt", size: 10 }],
      }) as any,
    );
    vi.mocked(useFederationByTarget).mockReturnValue({
      data: {
        id: "fed-abc",
        ownerUserId: "matthew",
        name: "lsi",
        primary: { regionId: "lsi-region-id", bucket: "lsi" },
        replicas: [{ regionId: "r2", bucket: "lsi", health: "in-sync" }],
        policy: {
          syncMode: "continuous",
          lagAlertSec: 300,
          writeQuorum: 1,
        },
        createdAt: "2026-05-20T00:00:00Z",
        updatedAt: "2026-05-22T00:00:00Z",
      },
      isLoading: false,
      error: null,
    } as any);

    renderBucket();
    await userEvent.click(screen.getByTestId("federation-badge"));

    expect(navigateMock).toHaveBeenCalledWith(
      expect.objectContaining({
        to: "/files/federated-buckets/$id",
        params: { id: "fed-abc" },
      }),
    );
  });
});

// v1.10.0b: versioning UI. Three surfaces wire into the bucket
// browser:
//   1. The VersioningSection card at the bottom of the page —
//      enable/suspend toggle + Save (mutation hook).
//   2. The "Show all versions" toolbar — only shows when versioning
//      is active (enabled or suspended) on a driver that supports it.
//   3. The per-row "Versions" button — gated by the same condition;
//      opens the ObjectVersionsPanel for that key.
describe("/files/$regionId/b/$bid — versioning UI (v1.10.0b)", () => {
  it("renders the versioning section card on every bucket page", () => {
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({ objects: [{ key: "a.txt", size: 10 }] }) as any,
    );

    renderBucket();
    expect(screen.getByTestId("versioning-section")).toBeInTheDocument();
  });

  it("hides the Show-all-versions toggle when versioning is unsupported by the driver", () => {
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "disabled", supported: false },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({ objects: [{ key: "a.txt", size: 10 }] }) as any,
    );

    renderBucket();
    expect(screen.queryByTestId("show-all-versions-toolbar")).not.toBeInTheDocument();
    expect(screen.getByTestId("versioning-unsupported")).toBeInTheDocument();
    // The per-row Versions button must NOT render when the driver
    // doesn't support versioning — would open an empty panel.
    expect(screen.queryByTestId("file-versions-a.txt")).not.toBeInTheDocument();
  });

  it("hides the Show-all-versions toggle when versioning is supported but never enabled", () => {
    // Default beforeEach mock — supported but status="disabled".
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({ objects: [{ key: "a.txt", size: 10 }] }) as any,
    );

    renderBucket();
    expect(screen.queryByTestId("show-all-versions-toolbar")).not.toBeInTheDocument();
    expect(screen.queryByTestId("file-versions-a.txt")).not.toBeInTheDocument();
  });

  it("shows the Show-all-versions toggle and per-row Versions buttons when versioning is enabled", () => {
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "enabled", supported: true },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [
          { key: "a.txt", size: 10 },
          { key: "b.txt", size: 20 },
        ],
      }) as any,
    );

    renderBucket();
    expect(screen.getByTestId("show-all-versions-toggle")).toBeInTheDocument();
    expect(screen.getByTestId("file-versions-a.txt")).toBeInTheDocument();
    expect(screen.getByTestId("file-versions-b.txt")).toBeInTheDocument();
  });

  it("shows the Show-all-versions toggle when versioning is suspended (historical versions still queryable)", () => {
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "suspended", supported: true },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({ objects: [{ key: "a.txt", size: 10 }] }) as any,
    );

    renderBucket();
    expect(screen.getByTestId("show-all-versions-toggle")).toBeInTheDocument();
    expect(screen.getByTestId("file-versions-a.txt")).toBeInTheDocument();
  });

  it("clicking a row's Versions button opens the versions panel for that key", async () => {
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "enabled", supported: true },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "a.txt", size: 10 }],
      }) as any,
    );
    vi.mocked(useObjectVersions).mockReturnValue({
      data: {
        versions: [
          {
            versionId: "v3id",
            key: "a.txt",
            size: 30,
            lastModified: "2026-05-22T14:32:00Z",
            isLatest: true,
            isDeleteMarker: false,
          },
          {
            versionId: "v2id",
            key: "a.txt",
            size: 20,
            lastModified: "2026-05-18T09:14:00Z",
            isLatest: false,
            isDeleteMarker: false,
          },
        ],
      },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as any);

    renderBucket();
    await userEvent.click(screen.getByTestId("file-versions-a.txt"));

    expect(screen.getByTestId("object-versions-panel")).toBeInTheDocument();
    // Both version rows render with the per-version testid.
    expect(screen.getByTestId("object-version-row-v3id")).toBeInTheDocument();
    expect(screen.getByTestId("object-version-row-v2id")).toBeInTheDocument();
    // The current version row carries the "current" affordance via
    // the data-is-latest attribute (and its rendered Badge).
    expect(
      screen.getByTestId("object-version-row-v3id").getAttribute("data-is-latest"),
    ).toBe("true");
  });

  it("renders delete markers with the distinct delete-marker badge", async () => {
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "enabled", supported: true },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "gone.txt", size: 10 }],
      }) as any,
    );
    vi.mocked(useObjectVersions).mockReturnValue({
      data: {
        versions: [
          {
            versionId: "dm1",
            key: "gone.txt",
            size: 0,
            lastModified: "2026-05-22T14:32:00Z",
            isLatest: true,
            isDeleteMarker: true,
          },
          {
            versionId: "v1id",
            key: "gone.txt",
            size: 20,
            lastModified: "2026-05-18T09:14:00Z",
            isLatest: false,
            isDeleteMarker: false,
          },
        ],
      },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as any);

    renderBucket();
    await userEvent.click(screen.getByTestId("file-versions-gone.txt"));

    // Delete-marker badge appears on the tombstone row only.
    const dmRow = screen.getByTestId("object-version-row-dm1");
    expect(dmRow.getAttribute("data-is-delete-marker")).toBe("true");
    expect(
      screen.getAllByTestId("object-version-delete-marker-badge").length,
    ).toBe(1);
    // Real version row carries no delete-marker badge.
    const realRow = screen.getByTestId("object-version-row-v1id");
    expect(realRow.getAttribute("data-is-delete-marker")).toBe("false");
    // Real versions render a Download anchor; delete markers don't.
    expect(screen.getByTestId("object-version-download-v1id")).toBeInTheDocument();
    expect(screen.queryByTestId("object-version-download-dm1")).not.toBeInTheDocument();
  });

  it("delete-version button opens a confirmation modal and fires the mutation on confirm", async () => {
    const deleteMutate = vi.fn();
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "enabled", supported: true },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useDeleteObjectVersion).mockReturnValue({
      mutate: deleteMutate,
      isPending: false,
      isError: false,
      variables: undefined,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "a.txt", size: 10 }],
      }) as any,
    );
    vi.mocked(useObjectVersions).mockReturnValue({
      data: {
        versions: [
          {
            versionId: "v2id",
            key: "a.txt",
            size: 20,
            isLatest: false,
            isDeleteMarker: false,
          },
        ],
      },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as any);

    renderBucket();
    await userEvent.click(screen.getByTestId("file-versions-a.txt"));
    await userEvent.click(screen.getByTestId("object-version-delete-v2id"));
    // Confirmation dialog mounts.
    await userEvent.click(screen.getByTestId("object-version-delete-confirm"));

    expect(deleteMutate).toHaveBeenCalledTimes(1);
    expect(deleteMutate).toHaveBeenCalledWith(
      { key: "a.txt", versionId: "v2id" },
      expect.any(Object),
    );
  });

  it("VersioningSection Save button stays disabled until the operator changes the select", async () => {
    const putMutate = vi.fn();
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "enabled", supported: true },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(usePutBucketVersioning).mockReturnValue({
      mutate: putMutate,
      isPending: false,
      isError: false,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({ objects: [{ key: "a.txt", size: 10 }] }) as any,
    );

    renderBucket();
    const saveBtn = screen.getByTestId("versioning-save");
    expect(saveBtn).toBeDisabled();

    // Switch from Enabled to Suspended and assert Save is now active.
    const select = screen.getByTestId("versioning-status-select") as HTMLSelectElement;
    await userEvent.selectOptions(select, "suspended");
    expect(saveBtn).not.toBeDisabled();

    await userEvent.click(saveBtn);
    expect(putMutate).toHaveBeenCalledTimes(1);
    expect(putMutate).toHaveBeenCalledWith(
      { status: "suspended" },
      expect.any(Object),
    );
  });

  it("toggling Show-all-versions flips highlight state on per-row Versions buttons", async () => {
    vi.mocked(useBucketVersioning).mockReturnValue({
      data: { status: "enabled", supported: true },
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({ objects: [{ key: "a.txt", size: 10 }] }) as any,
    );

    renderBucket();
    const versionsBtn = screen.getByTestId("file-versions-a.txt");
    // Ghost variant by default; toggling Show-all-versions swaps it
    // to outline. Use the className contains assertion since shadcn
    // Buttons compose their variant into the class list.
    const defaultClass = versionsBtn.className;

    const toggle = screen.getByTestId("show-all-versions-toggle");
    await userEvent.click(toggle);

    expect(versionsBtn.className).not.toBe(defaultClass);
  });
});
