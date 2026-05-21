// Mock the Link component to avoid needing full router setup
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children }: any) => <div data-testid="link-wrapper">{children}</div>,
  };
});

import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import FilesHome from "@/routes/files/index";
import { useUserRegions } from "@/shared/api/queries";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserRegions: vi.fn(),
  };
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
    },
  },
});

function renderWithProviders(component: React.ReactNode) {
  return render(
    <QueryClientProvider client={queryClient}>
      {component}
    </QueryClientProvider>,
  );
}

describe("FilesHome (region tier, ADR-0002)", () => {
  it("renders loading skeletons when regions are loading", async () => {
    vi.mocked(useUserRegions).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Regions")).toBeInTheDocument();
    expect(screen.getByText("S3 endpoints you have a key for")).toBeInTheDocument();
    expect(screen.queryByText(/Couldn't load your regions/)).not.toBeInTheDocument();
  });

  it("renders empty state with Connect button when regions array is empty", async () => {
    vi.mocked(useUserRegions).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Regions")).toBeInTheDocument();
    expect(screen.getByText("No regions yet")).toBeInTheDocument();
    expect(
      screen.getByText("Connect to a region with an S3 access key from your cluster admin."),
    ).toBeInTheDocument();
    // CTA appears once (in the empty-state action slot) when there are no regions.
    expect(screen.getAllByText("+ Connect a region")).toHaveLength(1);
  });

  it("renders region cards when regions are populated", async () => {
    const mockRegions = [
      {
        id: "reg-abc123",
        userId: "matthew",
        alias: "home",
        endpoint: "https://s3.basement.pq.io",
        region: "garage",
        accessKeyId: "GKabc",
        createdAt: "2026-05-01T00:00:00Z",
        updatedAt: "2026-05-01T00:00:00Z",
      },
      {
        id: "reg-def456",
        userId: "matthew",
        alias: "work",
        endpoint: "https://s3.amazonaws.com",
        region: "us-east-1",
        accessKeyId: "AKIA...",
        createdAt: "2026-05-02T00:00:00Z",
        updatedAt: "2026-05-02T00:00:00Z",
      },
    ];

    vi.mocked(useUserRegions).mockReturnValue({
      data: mockRegions,
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Regions")).toBeInTheDocument();

    // Link is mocked - check for link wrappers (one per region)
    const cardWrappers = screen.queryAllByTestId("link-wrapper");
    expect(cardWrappers.length).toBe(2);

    // Aliases visible
    expect(screen.getByText("home")).toBeInTheDocument();
    expect(screen.getByText("work")).toBeInTheDocument();
    // Endpoint hostnames visible
    expect(screen.getByText("s3.basement.pq.io")).toBeInTheDocument();
    expect(screen.getByText("s3.amazonaws.com")).toBeInTheDocument();
  });

  it("renders ErrorBanner when useUserRegions returns an error", async () => {
    const mockError = new Error("Failed to fetch regions");

    vi.mocked(useUserRegions).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: mockError,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Regions")).toBeInTheDocument();
    expect(
      screen.getByText("Couldn't load your regions. Retrying automatically..."),
    ).toBeInTheDocument();
  });
});
