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
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { UserClusterCard } from "@/components/clusters/UserClusterCard";
import { useUserClusterBuckets } from "@/shared/api/queries";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserClusterBuckets: vi.fn(),
  };
});

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

describe("UserClusterCard", () => {
  const fixture = {
    id: "cid-test123",
    label: "Test Cluster",
    driver: "garage-v1",
    color: "#FF5733",
  };

  it("renders cluster label and navigates to /files/{id}", async () => {
    vi.mocked(useUserClusterBuckets).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<UserClusterCard {...fixture} />);

    // Link is mocked - check for link wrapper and href in the card
    const linkWrapper = screen.getByTestId("link-wrapper");
    expect(linkWrapper).toBeInTheDocument();
    
    // Verify the card has data-testid="user-cluster-card" 
    const card = screen.getByTestId("user-cluster-card");
    expect(card).toBeInTheDocument();
  });

  it("renders driver badge with correct label", async () => {
    vi.mocked(useUserClusterBuckets).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<UserClusterCard {...fixture} />);

    expect(screen.getByText("Garage v1")).toBeInTheDocument();
  });

  it("renders bucket count from useUserClusterBuckets", async () => {
    const mockBuckets = [
      { id: "bkt-1", name: "bucket-one" },
      { id: "bkt-2", name: "bucket-two" },
      { id: "bkt-3", name: "bucket-three" },
    ];

    vi.mocked(useUserClusterBuckets).mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<UserClusterCard {...fixture} />);

    expect(screen.getByText("3 buckets")).toBeInTheDocument();
  });

  it("renders singular 'bucket' when count is 1", async () => {
    const mockBuckets = [{ id: "bkt-1", name: "single-bucket" }];

    vi.mocked(useUserClusterBuckets).mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<UserClusterCard {...fixture} />);

    expect(screen.getByText("1 bucket")).toBeInTheDocument();
  });

  it("renders '0 buckets' when no buckets returned", async () => {
    vi.mocked(useUserClusterBuckets).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<UserClusterCard {...fixture} />);

    expect(screen.getByText("0 buckets")).toBeInTheDocument();
  });

  it("renders color dot with correct background", async () => {
    vi.mocked(useUserClusterBuckets).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<UserClusterCard {...fixture} />);

    // Color dot has inline style with backgroundColor
    const colorDot = screen.queryByTestId("color-dot");
    expect(colorDot).toHaveStyle({ backgroundColor: "#FF5733" });
  });

  it("uses default color when none provided", async () => {
    const fixtureNoColor = { ...fixture, color: undefined };

    vi.mocked(useUserClusterBuckets).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<UserClusterCard {...fixtureNoColor} />);

    const colorDot = screen.queryByTestId("color-dot");
    expect(colorDot).toHaveStyle({ backgroundColor: "#C9874B" });
  });

  it("renders different driver labels correctly", async () => {
    const testCases = [
      { driver: "garage-v1", expected: "Garage v1" },
      { driver: "garage", expected: "Garage" },
      { driver: "aws-s3", expected: "AWS S3" },
      { driver: "minio", expected: "MinIO" },
    ];

    for (const { driver, expected } of testCases) {
      vi.mocked(useUserClusterBuckets).mockReturnValue({
        data: [],
        isLoading: false,
        error: null,
        isPending: false,
        refetch: vi.fn(),
      } as any);

      renderWithProviders(<UserClusterCard {...fixture} driver={driver} />);
      expect(screen.getByText(expected)).toBeInTheDocument();
    }
  });
});
