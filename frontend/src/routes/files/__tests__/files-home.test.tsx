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
import { useUserClusters } from "@/shared/api/queries";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserClusters: vi.fn(),
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

describe("FilesHome", () => {
  it("renders loading skeletons when clusters are loading", async () => {
    vi.mocked(useUserClusters).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Clusters")).toBeInTheDocument();
    expect(screen.getByText("Storage you have access to")).toBeInTheDocument();
    
    // At minimum, we should see the header area (no error banner)
    expect(screen.queryByText(/Couldn't connect/)).not.toBeInTheDocument();
  });

  it("renders empty state when clusters array is empty", async () => {
    vi.mocked(useUserClusters).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Clusters")).toBeInTheDocument();
    expect(screen.getByText("No clusters yet")).toBeInTheDocument();
    expect(screen.getByText("Contact your administrator to get access.")).toBeInTheDocument();
  });

  it("renders cluster cards when clusters are populated", async () => {
    const mockClusters = [
      {
        id: "cid-abc123",
        label: "My Garage Cluster",
        driver: "garage-v1",
        color: "#4A90D9",
      },
      {
        id: "cid-def456",
        label: "S3 Backup",
        driver: "aws-s3",
        color: "#E74C3C",
      },
    ];

    vi.mocked(useUserClusters).mockReturnValue({
      data: mockClusters,
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Clusters")).toBeInTheDocument();
    
    // Link is mocked - check for link wrappers (one per cluster)
    const cardWrappers = screen.queryAllByTestId("link-wrapper");
    expect(cardWrappers.length).toBe(2);

    expect(screen.getByText("Garage v1")).toBeInTheDocument();
    expect(screen.getByText("AWS S3")).toBeInTheDocument();
  });

  it("renders ErrorBanner when useUserClusters returns an error", async () => {
    const mockError = new Error("Failed to fetch clusters");
    
    vi.mocked(useUserClusters).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: mockError,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<FilesHome />);

    expect(screen.getByText("My Clusters")).toBeInTheDocument();
    expect(screen.getByText("Couldn't connect to backend. Retrying automatically...")).toBeInTheDocument();
  });
});
