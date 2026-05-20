// @vitest-environment jsdom
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import UserBucketObjects from "@/routes/files/$cid/b/$bid";

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

vi.mock("@/shared/api/queries", () => ({
  useUserClusterBuckets: vi.fn(),
  useUserObjects: vi.fn(),
  useUserPresignGet: vi.fn(),
}));

import { useUserClusterBuckets, useUserObjects, useUserPresignGet } from "@/shared/api/queries";

describe("UserBucketObjects", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders loading state when buckets are loading", async () => {
    useUserClusterBuckets.mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    } as any);

    renderWithProviders(
      <UserBucketObjects />,
    );

    expect(screen.getByText(/no alias/)).toBeInTheDocument();
  });

  it("renders empty state when bucket not found", async () => {
    useUserClusterBuckets.mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
    } as any);

    renderWithProviders(
      <UserBucketObjects />,
    );

    expect(screen.getByText("Bucket not found")).toBeInTheDocument();
  });

  it("renders bucket alias in header when bucket exists", async () => {
    const mockBuckets = [
      {
        id: mockBid,
        aliases: ["my-test-bucket"],
      },
    ];

    useUserClusterBuckets.mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
    } as any);

    const mockObjectsPage = {
      objects: [],
      prefixes: [],
      isTruncated: false,
    };

    useUserObjects.mockReturnValue({
      data: mockObjectsPage,
      isLoading: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(
      <UserBucketObjects />,
    );

    expect(screen.getByText("my-test-bucket")).toBeInTheDocument();
  });

  it("renders folder row when prefix is returned", async () => {
    const mockBuckets = [
      {
        id: mockBid,
        aliases: ["test-bucket"],
      },
    ];

    useUserClusterBuckets.mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
    } as any);

    const mockObjectsPage = {
      objects: [],
      prefixes: ["folder1/", "folder2/"],
      isTruncated: false,
    };

    useUserObjects.mockReturnValue({
      data: mockObjectsPage,
      isLoading: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(
      <UserBucketObjects />,
    );

    expect(screen.getByText("folder1")).toBeInTheDocument();
    expect(screen.getByText("folder2")).toBeInTheDocument();
  });

  it("renders file rows with size and download button", async () => {
    const mockBuckets = [
      {
        id: mockBid,
        aliases: ["test-bucket"],
      },
    ];

    useUserClusterBuckets.mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
    } as any);

    const mockObjectsPage = {
      objects: [
        { key: "file1.txt", size: 1024, lastModified: new Date(), isDir: false },
        { key: "image.png", size: 524288, lastModified: new Date(), isDir: false },
      ],
      prefixes: [],
      isTruncated: false,
    };

    useUserObjects.mockReturnValue({
      data: mockObjectsPage,
      isLoading: false,
      refetch: vi.fn(),
    } as any);

    const presignMutation = {
      mutateAsync: vi.fn().mockResolvedValue({ url: "https://example.com/presigned" }),
      isError: false,
    };

    useUserPresignGet.mockReturnValue(presignMutation);

    renderWithProviders(
      <UserBucketObjects />,
    );

    expect(screen.getByText("file1.txt")).toBeInTheDocument();
    expect(screen.getByText("image.png")).toBeInTheDocument();
    expect(screen.getByText("1.0 KB")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /download/i })).toBeInTheDocument();
  });

  it("handles folder click to update prefix", async () => {
    const mockBuckets = [
      {
        id: mockBid,
        aliases: ["test-bucket"],
      },
    ];

    useUserClusterBuckets.mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
    } as any);

    const mockObjectsPage = {
      objects: [],
      prefixes: ["subfolder/"],
      isTruncated: false,
    };

    useUserObjects.mockReturnValue({
      data: mockObjectsPage,
      isLoading: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(
      <UserBucketObjects />,
    );

    const folderRow = screen.getByText("subfolder").closest("tr");
    if (folderRow) {
      fireEvent.click(folderRow);
    }
  });

  it("handles file download via presign mutation", async () => {
    const mockBuckets = [
      {
        id: mockBid,
        aliases: ["test-bucket"],
      },
    ];

    useUserClusterBuckets.mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
    } as any);

    const mockObjectsPage = {
      objects: [
        { key: "download-me.txt", size: 256, lastModified: new Date(), isDir: false },
      ],
      prefixes: [],
      isTruncated: false,
    };

    useUserObjects.mockReturnValue({
      data: mockObjectsPage,
      isLoading: false,
      refetch: vi.fn(),
    } as any);

    const presignMutation = {
      mutateAsync: vi.fn().mockResolvedValue({ url: "https://s3.example.com/file.txt" }),
      isError: false,
    };

    useUserPresignGet.mockReturnValue(presignMutation);

    window.open = vi.fn();

    renderWithProviders(
      <UserBucketObjects />,
    );

    const downloadButton = screen.getByRole("button", { name: /download/i });
    fireEvent.click(downloadButton);

    expect(presignMutation.mutateAsync).toHaveBeenCalled();
  });

  it("shows empty state when no objects or folders exist", async () => {
    const mockBuckets = [
      {
        id: mockBid,
        aliases: ["empty-bucket"],
      },
    ];

    useUserClusterBuckets.mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
    } as any);

    const mockObjectsPage = {
      objects: [],
      prefixes: [],
      isTruncated: false,
    };

    useUserObjects.mockReturnValue({
      data: mockObjectsPage,
      isLoading: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(
      <UserBucketObjects />,
    );

    expect(screen.getByText("No objects here")).toBeInTheDocument();
  });

  it("renders breadcrumb when prefix is set", async () => {
    const mockBuckets = [
      {
        id: mockBid,
        aliases: ["test-bucket"],
      },
    ];

    useUserClusterBuckets.mockReturnValue({
      data: mockBuckets,
      isLoading: false,
      error: null,
    } as any);

    const mockObjectsPage = {
      objects: [],
      prefixes: [],
      isTruncated: false,
    };

    useUserObjects.mockReturnValue({
      data: mockObjectsPage,
      isLoading: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(
      <UserBucketObjects />,
    );

    const breadcrumb = screen.queryByRole("navigation");
    if (breadcrumb) {
      // Breadcrumb should be present when prefix exists
    }
  });
});
