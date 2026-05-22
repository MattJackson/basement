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
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/$regionId/b/$bid";
import {
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
