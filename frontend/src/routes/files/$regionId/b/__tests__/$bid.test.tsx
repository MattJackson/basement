// v1.3.0c.1: bucket-browser folder navigation. The handler now returns
// CommonPrefixes alongside Objects when delimiter="/" (the default),
// and clicking a folder row drills in by re-listing with prefix=folder.
// These tests assert the rendering split (folders first, files after)
// and that the click emits the right navigate() call.

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
      useParams: () => ({ regionId: "lsi-region-id", bid: "lsi" }),
    }),
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserRegionBuckets: vi.fn(),
    useUserRegionObjects: vi.fn(),
    useUserRegionPresignGet: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/$regionId/b/$bid";
import {
  useUserRegionBuckets,
  useUserRegionObjects,
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

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useUserRegionBuckets).mockReturnValue({
    data: [{ id: "lsi", aliases: ["lsi"], objects: 0, bytes: 0, unfinishedUploads: 0 }],
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
    vi.mocked(useUserRegionObjects).mockReturnValue({
      data: {
        objects: [{ key: "readme.txt", size: 42, last_modified: "2026-05-21T00:00:00Z" }],
        commonPrefixes: ["index/", "raw/"],
        isTruncated: false,
      },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as any);

    renderBucket();

    // Both folders render.
    expect(screen.getByText("index")).toBeInTheDocument();
    expect(screen.getByText("raw")).toBeInTheDocument();
    // File renders too.
    expect(screen.getByText("readme.txt")).toBeInTheDocument();
    // Folder rows render BEFORE the file row in the table.
    const rows = screen.getAllByRole("row");
    // First row is the header; folder rows next, then file row.
    const bodyRowText = rows.slice(1).map((r) => r.textContent || "");
    const firstFolderIdx = bodyRowText.findIndex((t) => t.includes("index"));
    const fileIdx = bodyRowText.findIndex((t) => t.includes("readme.txt"));
    expect(firstFolderIdx).toBeGreaterThan(-1);
    expect(fileIdx).toBeGreaterThan(firstFolderIdx);
  });

  it("clicking a folder navigates to the same route with the folder's full prefix", async () => {
    vi.mocked(useUserRegionObjects).mockReturnValue({
      data: {
        objects: [],
        commonPrefixes: ["raw/"],
        isTruncated: false,
      },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as any);

    renderBucket();
    const folderRow = screen.getByText("raw").closest("tr")!;
    await userEvent.click(within(folderRow).getByText("raw"));

    expect(navigateMock).toHaveBeenCalledWith(
      expect.objectContaining({
        to: "/files/$regionId/b/$bid",
        params: { regionId: "lsi-region-id", bid: "lsi" },
        search: expect.objectContaining({ prefix: "raw/" }),
      }),
    );
  });

  it("shows the 'This folder is empty' state when both objects and commonPrefixes are empty inside a prefix", () => {
    // Force prefix=raw/ via window.location.
    const orig = window.location;
    delete (window as any).location;
    (window as any).location = { ...orig, search: "?prefix=raw/" };

    vi.mocked(useUserRegionObjects).mockReturnValue({
      data: { objects: [], commonPrefixes: [], isTruncated: false },
      isLoading: false,
      error: null,
      refetch: vi.fn(),
    } as any);

    renderBucket();
    expect(screen.getByText(/This folder is empty/i)).toBeInTheDocument();

    (window as any).location = orig;
  });
});
