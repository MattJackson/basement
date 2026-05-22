// Mock the router so links don't need a full router setup.
vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children, to }: any) => (
      <a data-testid="link-wrapper" href={typeof to === "string" ? to : "#"}>
        {children}
      </a>
    ),
    createFileRoute: () => () => ({}),
    useRouter: () => ({ invalidate: vi.fn() }),
  };
});

// userPage wraps the inner component with the user layout chrome;
// for unit tests we want just the inner page. The /files/keys layout
// uses the wrapper, but keys/index.tsx (the list page exercised here)
// does not — so the mock is harmless if not invoked.
vi.mock("@/shared/layout/userPage", () => ({
  userPage: (C: any) => C,
}));

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import KeysIndex from "@/routes/files/keys/index";
import { useUserRegions, useDeleteUserRegion } from "@/shared/api/queries";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserRegions: vi.fn(),
    useDeleteUserRegion: vi.fn(),
  };
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false },
  },
});

function renderWithProviders(component: React.ReactNode) {
  return render(
    <QueryClientProvider client={queryClient}>
      {component}
    </QueryClientProvider>,
  );
}

const mutateAsyncMock = vi.fn().mockResolvedValue(undefined);

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useDeleteUserRegion).mockReturnValue({
    mutateAsync: mutateAsyncMock,
    isPending: false,
    error: null,
  } as any);
});

describe("KeysIndex (My Keys)", () => {
  it("renders loading skeletons while regions load", () => {
    vi.mocked(useUserRegions).mockReturnValue({
      data: undefined,
      isLoading: true,
      error: null,
    } as any);

    renderWithProviders(<KeysIndex />);

    expect(screen.getByText("My Keys")).toBeInTheDocument();
    expect(
      screen.getAllByTestId("region-key-card-skeleton").length,
    ).toBeGreaterThanOrEqual(1);
  });

  it("renders the empty state with Add a key CTA when the user has no keys", () => {
    vi.mocked(useUserRegions).mockReturnValue({
      data: [],
      isLoading: false,
      error: null,
    } as any);

    renderWithProviders(<KeysIndex />);

    expect(screen.getByText("My Keys")).toBeInTheDocument();
    expect(screen.getByText("No keys yet")).toBeInTheDocument();
    // The empty-state CTA is "Add a key".
    expect(screen.getAllByText("Add a key").length).toBeGreaterThanOrEqual(1);
  });

  it("renders an error banner when the query errors", () => {
    vi.mocked(useUserRegions).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error("network down"),
    } as any);

    renderWithProviders(<KeysIndex />);

    expect(screen.getByText("My Keys")).toBeInTheDocument();
    expect(
      screen.getByText(/Couldn't load your keys/),
    ).toBeInTheDocument();
  });

  it("renders one card per key with access key + endpoint, including same endpoint twice", () => {
    // v1.2.0d: two keys at the same endpoint with different aliases
    // is now first-class.
    vi.mocked(useUserRegions).mockReturnValue({
      data: [
        {
          id: "r1",
          userId: "matthew",
          alias: "home",
          endpoint: "https://s3.basement.pq.io",
          region: "garage",
          accessKeyId: "GK_ABC123",
          createdAt: "2026-05-19T00:00:00Z",
          updatedAt: "2026-05-19T00:00:00Z",
        },
        {
          id: "r2",
          userId: "matthew",
          alias: "test2",
          endpoint: "https://s3.basement.pq.io",
          region: "garage",
          accessKeyId: "GK_TEST2",
          createdAt: "2026-05-20T00:00:00Z",
          updatedAt: "2026-05-20T00:00:00Z",
        },
      ],
      isLoading: false,
      error: null,
    } as any);

    renderWithProviders(<KeysIndex />);

    const cards = screen.getAllByTestId("region-key-card");
    expect(cards.length).toBe(2);
    expect(screen.getByText("home")).toBeInTheDocument();
    expect(screen.getByText("test2")).toBeInTheDocument();
    expect(screen.getByText("GK_ABC123")).toBeInTheDocument();
    expect(screen.getByText("GK_TEST2")).toBeInTheDocument();
    // Same endpoint listed on both cards — getAllByText to handle the duplicate.
    expect(
      screen.getAllByText("https://s3.basement.pq.io").length,
    ).toBe(2);
  });

  it("copies the access key id to clipboard on Copy click", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });

    vi.mocked(useUserRegions).mockReturnValue({
      data: [
        {
          id: "r1",
          userId: "matthew",
          alias: "home",
          endpoint: "https://s3.basement.pq.io",
          region: "garage",
          accessKeyId: "GK_COPY_ME",
          createdAt: "2026-05-19T00:00:00Z",
          updatedAt: "2026-05-19T00:00:00Z",
        },
      ],
      isLoading: false,
      error: null,
    } as any);

    renderWithProviders(<KeysIndex />);

    await userEvent.click(screen.getByTestId("copy-region-key-button"));
    expect(writeText).toHaveBeenCalledWith("GK_COPY_ME");
  });

  it("opens the delete confirmation when Delete is clicked", async () => {
    vi.mocked(useUserRegions).mockReturnValue({
      data: [
        {
          id: "r1",
          userId: "matthew",
          alias: "home",
          endpoint: "https://s3.basement.pq.io",
          region: "garage",
          accessKeyId: "GK_ABC",
          createdAt: "2026-05-19T00:00:00Z",
          updatedAt: "2026-05-19T00:00:00Z",
        },
      ],
      isLoading: false,
      error: null,
    } as any);

    renderWithProviders(<KeysIndex />);

    await userEvent.click(screen.getByTestId("delete-region-key-button"));

    expect(screen.getByText("Delete key?")).toBeInTheDocument();
    expect(
      screen.getByText(/revokes basement&apos;s stored key|revokes basement's stored key/i),
    ).toBeInTheDocument();
  });
});
