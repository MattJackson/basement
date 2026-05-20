// Mock the Link component to avoid needing full router setup
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children }: any) => <div data-testid="link-wrapper">{children}</div>,
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import KeysHome from "@/routes/files/keys";
import { useUserKeys } from "@/shared/api/queries";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserKeys: vi.fn(),
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

describe("KeysHome", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("renders loading skeletons when keys are loading", async () => {
    vi.mocked(useUserKeys).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    expect(screen.getByText("My Keys")).toBeInTheDocument();
    expect(screen.getByText("S3 credentials for the buckets you can access")).toBeInTheDocument();
    
    const skeletonCards = screen.getAllByTestId("user-key-pair-card-skeleton");
    expect(skeletonCards.length).toBeGreaterThanOrEqual(1);
  });

  it("renders empty state when keys array is empty", async () => {
    vi.mocked(useUserKeys).mockReturnValue({
      data: { keys: [], errors: [] },
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    expect(screen.getByText("My Keys")).toBeInTheDocument();
    expect(screen.getByText("No keys yet")).toBeInTheDocument();
    expect(screen.getByText("Contact your administrator to get one.")).toBeInTheDocument();
  });

  it("renders error banner when useUserKeys returns an error", async () => {
    const mockError = new Error("Failed to fetch keys");
    
    vi.mocked(useUserKeys).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: mockError,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    expect(screen.getByText("My Keys")).toBeInTheDocument();
    expect(screen.getByText(/Couldn't connect to backend/)).toBeInTheDocument();
  });

  it("renders one card per key", async () => {
    const mockKeys = [
      {
        id: "key1",
        connectionId: "cid-abc",
        name: "My Key 1",
        accessKeyId: "AKIAKEY1",
        created: "2026-05-19T00:00:00Z",
      },
      {
        id: "key2",
        connectionId: "cid-def",
        name: "My Key 2",
        accessKeyId: "AKIAKEY2",
        created: "2026-05-19T00:00:00Z",
      },
    ];

    vi.mocked(useUserKeys).mockReturnValue({
      data: { keys: mockKeys, errors: [] },
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    expect(screen.getByText("My Keys")).toBeInTheDocument();
    
    const cardWrappers = screen.getAllByTestId("user-key-pair-card");
    expect(cardWrappers.length).toBe(2);
  });

  it("displays key name when no alias available", async () => {
    vi.mocked(useUserKeys).mockReturnValue({
      data: { 
        keys: [
          {
            id: "key1",
            connectionId: "cid-abc",
            name: "Test Key",
            accessKeyId: "AKIATEST123456",
            created: null,
          } as any,
        ], 
        errors: [] 
      },
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    expect(screen.getByText(/Key for "Test Key"/)).toBeInTheDocument();
  });

  // Permissions are not returned by /user/keys API, so we skip permission badge tests for v0.5.6

  it("copies access key ID to clipboard on button click", async () => {
    const mockClipboardWriteText = vi.fn();
    Object.assign(navigator, {
      clipboard: { writeText: mockClipboardWriteText },
    });

    vi.mocked(useUserKeys).mockReturnValue({
      data: { 
        keys: [
          {
            id: "key1",
            connectionId: "cid-abc",
            bucketId: "bucket-a",
            name: "Test Key",
            accessKeyId: "AKIATESTKEY123",
            permissions: { owner: false, read: true, write: false },
            created: null,
          } as any,
        ], 
        errors: [] 
      },
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    const copyButton = screen.getByTestId("copy-key-button");
    await userEvent.click(copyButton);

    expect(mockClipboardWriteText).toHaveBeenCalledWith("AKIATESTKEY123");
  });

  it("displays created date when available", async () => {
    vi.mocked(useUserKeys).mockReturnValue({
      data: { 
        keys: [
          {
            id: "key1",
            connectionId: "cid-abc",
            bucketId: "bucket-a",
            name: "Test Key",
            accessKeyId: "AKIATEST123",
            permissions: { owner: false, read: true, write: false },
            created: "2026-05-19T12:00:00Z",
          } as any,
        ], 
        errors: [] 
      },
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    expect(screen.getByText(/Created/)).toBeInTheDocument();
  });

  it("handles cluster errors gracefully", async () => {
    const mockErrors = [
      { connectionId: "cid-bad", message: "Connection failed" },
    ];

    vi.mocked(useUserKeys).mockReturnValue({
      data: { keys: [], errors: mockErrors },
      isLoading: false,
      error: null,
      isPending: false,
      refetch: vi.fn(),
    } as any);

    renderWithProviders(<KeysHome />);

    // Should show cluster error message when no keys but there are cluster errors
    expect(screen.getByText(/Couldn't load keys from/)).toBeInTheDocument();
  });
});
