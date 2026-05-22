// v1.8.0e — mobile bucket browser layout.
//
// Asserts the bucket browser swaps its 5-column table layout for a
// stacked card layout when window.matchMedia('(max-width: 639px)')
// reports a match (i.e. the viewport is below the sm: breakpoint).
//
// The switch is driven by useIsMobile() inside the route component;
// we stub matchMedia BEFORE importing the route so the initial render
// already picks the mobile layout. The marker is the
// data-layout="card" attribute on the list container, plus the
// presence of the mobile-only select-all strip.

const navigateMock = vi.fn();
let locationSearchStr = "";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children, to }: any) => (
      <a href={typeof to === "string" ? to : "#"}>{children}</a>
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
  };
});

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/$regionId/b/$bid";
import {
  useFederationByTarget,
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

// installMatchMediaMock replaces window.matchMedia with a stub that
// reports a match for the mobile breakpoint query and nothing else.
// Restored in afterEach via the returned teardown handle.
function installMatchMediaMock(isMobile: boolean) {
  const original = window.matchMedia;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).matchMedia = vi.fn().mockImplementation((query: string) => {
    const mobileQuery = query.includes("max-width: 639px");
    return {
      matches: mobileQuery ? isMobile : false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    };
  });
  return () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).matchMedia = original;
  };
}

function makeInfiniteResult(page: {
  objects?: Array<{ key: string; size: number; last_modified?: string }>;
  commonPrefixes?: string[];
}) {
  return {
    data: {
      pages: [
        {
          objects: page.objects ?? [],
          commonPrefixes: page.commonPrefixes ?? [],
          isTruncated: false,
        },
      ],
      pageParams: [""],
    },
    isLoading: false,
    isFetchingNextPage: false,
    error: null,
    refetch: vi.fn(),
    fetchNextPage: vi.fn(),
    hasNextPage: false,
  };
}

let teardownMatchMedia: (() => void) | null = null;

beforeEach(() => {
  vi.clearAllMocks();
  locationSearchStr = "";
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
  vi.mocked(useFederationByTarget).mockReturnValue({
    data: null,
    isLoading: false,
    error: null,
  } as any);
});

afterEach(() => {
  if (teardownMatchMedia) {
    teardownMatchMedia();
    teardownMatchMedia = null;
  }
});

describe("/files/$regionId/b/$bid — mobile layout (v1.8.0e)", () => {
  it("renders as cards (data-layout='card') below the sm: breakpoint", () => {
    teardownMatchMedia = installMatchMediaMock(true);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "report.pdf", size: 12345, last_modified: "2026-05-21T00:00:00Z" }],
      }) as any,
    );

    renderBucket();

    const list = screen.getByTestId("virtual-object-list");
    expect(list.getAttribute("data-layout")).toBe("card");
    // Mobile-only select-all strip is present.
    expect(screen.getByTestId("select-all-cell-mobile")).toBeInTheDocument();
  });

  it("renders as a table (data-layout='table') at >=sm breakpoint", () => {
    teardownMatchMedia = installMatchMediaMock(false);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "report.pdf", size: 12345 }],
      }) as any,
    );

    renderBucket();

    const list = screen.getByTestId("virtual-object-list");
    expect(list.getAttribute("data-layout")).toBe("table");
  });

  it("mobile rows expose a 44x44 download tap target via aria-label", () => {
    teardownMatchMedia = installMatchMediaMock(true);
    vi.mocked(useUserRegionObjectsInfinite).mockReturnValue(
      makeInfiniteResult({
        objects: [{ key: "report.pdf", size: 12345 }],
      }) as any,
    );

    renderBucket();

    // The mobile FileRow renders an aria-labeled icon button instead
    // of the desktop "Download" text button.
    const button = screen.getByRole("button", { name: /Download report\.pdf/i });
    expect(button).toBeInTheDocument();
    // Tailwind h-11 w-11 = 44x44.
    expect(button.className).toMatch(/\bh-11\b/);
    expect(button.className).toMatch(/\bw-11\b/);
  });
});
