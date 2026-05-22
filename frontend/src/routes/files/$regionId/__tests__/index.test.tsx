// Tests for the v1.3.0a.1 USER_KEY_REJECTED handling on the region's
// bucket-list page. When the API returns 401 USER_KEY_REJECTED, the
// page must show the friendly inline alert + a Delete button + a link
// to add a fresh key — NOT the generic "Can't list buckets" empty
// state that was the only error UI before this cycle.

const navigateMock = vi.fn();

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
      useParams: () => ({ regionId: "lsi-region-id" }),
    }),
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserRegion: vi.fn(),
    useUserRegions: vi.fn(),
    useUserRegionBuckets: vi.fn(),
    useDeleteUserRegion: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/$regionId/index";
import {
  useDeleteUserRegion,
  useUserRegion,
  useUserRegionBuckets,
  useUserRegions,
} from "@/shared/api/queries";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderRegion() {
  const RegionBuckets = (Route as unknown as { component: React.ComponentType })
    .component;
  return render(
    <QueryClientProvider client={queryClient}>
      <RegionBuckets />
    </QueryClientProvider>,
  );
}

// keyRejectedError mirrors the shape `apiError()` produces in
// queries.ts: a real Error with a non-enumerable `code` (and `status`)
// glued on. The page reads `(err as any).code` to branch.
function keyRejectedError(): Error {
  const e = new Error("USER_KEY_REJECTED: Your access key was rejected");
  (e as Error & { code?: string; status?: number }).code = "USER_KEY_REJECTED";
  (e as Error & { code?: string; status?: number }).status = 401;
  return e;
}

const mutateAsyncMock = vi.fn().mockResolvedValue(undefined);

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useUserRegions).mockReturnValue({
    data: [
      {
        id: "lsi-region-id",
        userId: "matthew",
        alias: "lsi",
        endpoint: "https://s3.basement.pq.io",
        region: "garage",
        accessKeyId: "GK6f4403ea8f6168544d035f4d",
        createdAt: "2026-05-19T00:00:00Z",
        updatedAt: "2026-05-19T00:00:00Z",
      },
    ],
    isLoading: false,
    error: null,
  } as any);
  vi.mocked(useUserRegion).mockReturnValue({
    data: {
      id: "lsi-region-id",
      userId: "matthew",
      alias: "lsi",
      endpoint: "https://s3.basement.pq.io",
      region: "garage",
      accessKeyId: "GK6f4403ea8f6168544d035f4d",
      createdAt: "2026-05-19T00:00:00Z",
      updatedAt: "2026-05-19T00:00:00Z",
    },
    isLoading: false,
    error: null,
  } as any);
  vi.mocked(useDeleteUserRegion).mockReturnValue({
    mutateAsync: mutateAsyncMock,
    isPending: false,
    error: null,
  } as any);
});

describe("/files/$regionId — USER_KEY_REJECTED handling", () => {
  it("renders the friendly KeyRejectedAlert instead of the generic error when the API returns USER_KEY_REJECTED", () => {
    vi.mocked(useUserRegionBuckets).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: keyRejectedError(),
    } as any);

    renderRegion();

    // The dedicated alert is rendered.
    expect(screen.getByTestId("user-key-rejected-alert")).toBeInTheDocument();
    expect(
      screen.getByText(/Your access key was rejected/i),
    ).toBeInTheDocument();
    // Action: delete this region.
    expect(
      screen.getByTestId("delete-rejected-region-button"),
    ).toBeInTheDocument();
    // Action: add a fresh key.
    const link = screen.getByTestId("add-new-key-link").closest("a");
    expect(link).toHaveAttribute("href", "/files/keys/new");

    // The generic "Can't list buckets" empty state must NOT render
    // when the code is USER_KEY_REJECTED — that was the bug we're
    // fixing.
    expect(screen.queryByText("Can't list buckets")).toBeNull();
  });

  it("falls back to the generic error UI when the error is NOT USER_KEY_REJECTED", () => {
    const generic = new Error("network down");
    (generic as Error & { code?: string }).code = "NETWORK_ERROR";
    vi.mocked(useUserRegionBuckets).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: generic,
    } as any);

    renderRegion();

    // The KEY rejected alert must NOT render for non-auth errors.
    expect(screen.queryByTestId("user-key-rejected-alert")).toBeNull();
    // The generic "Can't list buckets" pathway still fires.
    expect(screen.getByText("Can't list buckets")).toBeInTheDocument();
  });

  it("calls deleteRegion + navigates back to /files/keys when Delete this region is clicked", async () => {
    vi.mocked(useUserRegionBuckets).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: keyRejectedError(),
    } as any);

    renderRegion();

    await userEvent.click(screen.getByTestId("delete-rejected-region-button"));

    expect(mutateAsyncMock).toHaveBeenCalledWith("lsi-region-id");
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/files/keys",
      replace: true,
    });
  });
});

// v1.4.0a: per-driver capability flag (PerBucketStatsAvailable) drives
// whether the Size + Objects columns render. Garage v1 returns false
// today — hide the columns rather than render rows of em-dashes.
describe("/files/$regionId — Size/Objects column gating (v1.4.0a)", () => {
  it("hides Size + Objects columns when perBucketStatsAvailable=false", () => {
    vi.mocked(useUserRegionBuckets).mockReturnValue({
      data: {
        buckets: [
          { id: "lsi", aliases: ["lsi"], objects: 0, bytes: 0, unfinishedUploads: 0 },
        ],
        perBucketStatsAvailable: false,
      },
      isLoading: false,
      error: null,
    } as any);

    renderRegion();

    // Bucket name renders. ("lsi" also appears in the page header
    // as the region alias; assert at least one occurrence.)
    expect(screen.getAllByText("lsi").length).toBeGreaterThan(0);
    // Size + Objects column headers are absent.
    expect(screen.queryByText("Size")).toBeNull();
    expect(screen.queryByText("Objects")).toBeNull();
  });

  it("shows Size + Objects columns when perBucketStatsAvailable=true", () => {
    vi.mocked(useUserRegionBuckets).mockReturnValue({
      data: {
        buckets: [
          { id: "lsi", aliases: ["lsi"], objects: 42, bytes: 1024, unfinishedUploads: 0 },
        ],
        perBucketStatsAvailable: true,
      },
      isLoading: false,
      error: null,
    } as any);

    renderRegion();

    // Headers present.
    expect(screen.getByText("Size")).toBeInTheDocument();
    expect(screen.getByText("Objects")).toBeInTheDocument();
    // Counters render.
    expect(screen.getByText("42")).toBeInTheDocument();
  });
});
