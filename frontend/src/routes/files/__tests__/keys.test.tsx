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
import {
  useUserRegions,
  useDeleteUserRegion,
  useRotateUserRegion,
} from "@/shared/api/queries";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUserRegions: vi.fn(),
    useDeleteUserRegion: vi.fn(),
    useRotateUserRegion: vi.fn(),
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
const rotateMutateAsyncMock = vi.fn().mockResolvedValue(undefined);

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useDeleteUserRegion).mockReturnValue({
    mutateAsync: mutateAsyncMock,
    isPending: false,
    error: null,
  } as any);
  vi.mocked(useRotateUserRegion).mockReturnValue({
    mutateAsync: rotateMutateAsyncMock,
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

  // v1.3.0c — per-region addressing-style subtitle on each card.
  it("shows 'via path-style' on path-style keys and 'via virtual-host' on virtual-host keys", () => {
    vi.mocked(useUserRegions).mockReturnValue({
      data: [
        {
          id: "r1",
          userId: "matthew",
          alias: "home",
          endpoint: "https://s3.pq.io",
          region: "garage",
          accessKeyId: "GK_PATH",
          addressingStyle: "path",
          createdAt: "2026-05-21T00:00:00Z",
          updatedAt: "2026-05-21T00:00:00Z",
        },
        {
          id: "r2",
          userId: "matthew",
          alias: "aws-tools",
          endpoint: "https://s3.us-east-1.amazonaws.com",
          region: "us-east-1",
          accessKeyId: "AKIA_VH",
          addressingStyle: "virtual_host",
          createdAt: "2026-05-21T00:00:00Z",
          updatedAt: "2026-05-21T00:00:00Z",
        },
      ],
      isLoading: false,
      error: null,
    } as any);

    renderWithProviders(<KeysIndex />);

    const subtitles = screen.getAllByTestId("region-addressing-style");
    expect(subtitles[0]).toHaveTextContent("via path-style");
    expect(subtitles[1]).toHaveTextContent("via virtual-host");
  });

  // v1.3.0c — Rotate button + 2-field dialog wiring.
  it("opens the rotate dialog and submits the new credentials", async () => {
    rotateMutateAsyncMock.mockResolvedValueOnce({
      id: "r1",
      userId: "matthew",
      alias: "home",
      endpoint: "https://s3.pq.io",
      region: "garage",
      accessKeyId: "GK_NEW",
      addressingStyle: "path",
      createdAt: "2026-05-21T00:00:00Z",
      updatedAt: "2026-05-21T00:00:00Z",
    });

    vi.mocked(useUserRegions).mockReturnValue({
      data: [
        {
          id: "r1",
          userId: "matthew",
          alias: "home",
          endpoint: "https://s3.pq.io",
          region: "garage",
          accessKeyId: "GK_OLD",
          addressingStyle: "path",
          createdAt: "2026-05-21T00:00:00Z",
          updatedAt: "2026-05-21T00:00:00Z",
        },
      ],
      isLoading: false,
      error: null,
    } as any);

    renderWithProviders(<KeysIndex />);

    await userEvent.click(screen.getByTestId("rotate-region-key-button"));

    expect(screen.getByText("Rotate key?")).toBeInTheDocument();

    await userEvent.type(
      screen.getByTestId("rotate-region-access-key"),
      "GK_NEW",
    );
    await userEvent.type(screen.getByTestId("rotate-region-secret"), "fresh");

    await userEvent.click(screen.getByTestId("rotate-region-submit"));

    expect(rotateMutateAsyncMock).toHaveBeenCalledTimes(1);
    expect(rotateMutateAsyncMock).toHaveBeenCalledWith({
      regionId: "r1",
      accessKeyId: "GK_NEW",
      secretKey: "fresh",
    });
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
