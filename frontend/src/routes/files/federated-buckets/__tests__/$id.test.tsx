// Tests for /files/federated-buckets/$id detail page (v1.6.0d).
//
// Asserts:
//   - Per-replica health table renders with correct cells
//   - "Resync now" calls the resync mutation with the federation id
//   - "Promote to primary" opens the confirmation Dialog, surfaces
//     old + new primary, and the confirm button fires the failover
//     mutation with the right body
//   - Delete uses the existing confirm() shim — accepted = mutate

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
      useParams: () => ({ id: "fb-1" }),
    }),
    Link: ({ children }: any) => <a>{children}</a>,
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useFederation: vi.fn(),
    useFailoverFederation: vi.fn(),
    useResyncFederation: vi.fn(),
    useDeleteFederation: vi.fn(),
    useUserRegions: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/federated-buckets/$id/index";
import {
  useFederation,
  useFailoverFederation,
  useResyncFederation,
  useDeleteFederation,
  useUserRegions,
} from "@/shared/api/queries";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderDetail() {
  const Component = (Route as unknown as { component: React.ComponentType })
    .component;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>,
  );
}

const failoverMutate = vi.fn();
const resyncMutate = vi.fn();
const deleteMutate = vi.fn();

const baseFederation = {
  id: "fb-1",
  ownerUserId: "u1",
  name: "photos",
  primary: { regionId: "r-home", bucket: "photos" },
  replicas: [
    {
      regionId: "r-b2",
      bucket: "photos-mirror",
      health: "in-sync",
      lastSync: "2026-05-22T10:00:00Z",
      lagObjects: 0,
      lagBytes: 0,
    },
    {
      regionId: "r-wasabi",
      bucket: "photos-arch",
      health: "lagging",
      lastSync: "2026-05-22T08:00:00Z",
      lagObjects: 42,
      lagBytes: 1024,
    },
  ],
  policy: {
    syncMode: "continuous",
    lagAlertSec: 300,
    writeQuorum: 1,
  },
  createdAt: "2026-05-22T00:00:00Z",
  updatedAt: "2026-05-22T10:00:00Z",
  computedHealth: "lagging",
};

beforeEach(() => {
  vi.clearAllMocks();
  failoverMutate.mockReset();
  resyncMutate.mockReset();
  deleteMutate.mockReset();
  navigateMock.mockReset();

  vi.mocked(useFederation).mockReturnValue({
    data: baseFederation,
    isLoading: false,
    error: null,
  } as any);

  vi.mocked(useFailoverFederation).mockReturnValue({
    mutateAsync: failoverMutate,
    isPending: false,
    error: null,
  } as any);

  vi.mocked(useResyncFederation).mockReturnValue({
    mutateAsync: resyncMutate,
    isPending: false,
    error: null,
  } as any);

  vi.mocked(useDeleteFederation).mockReturnValue({
    mutateAsync: deleteMutate,
    isPending: false,
    error: null,
  } as any);

  vi.mocked(useUserRegions).mockReturnValue({
    data: [
      { id: "r-home", alias: "home", endpoint: "http://garage.lan:3902" },
      { id: "r-b2", alias: "b2-offsite", endpoint: "https://b2.example.com" },
      { id: "r-wasabi", alias: "wasabi-arch", endpoint: "https://s3.wasabisys.com" },
    ],
    isLoading: false,
    error: null,
  } as any);
});

describe("FederationDetailPage", () => {
  it("renders the federation name, configuration, and per-replica health rows", () => {
    renderDetail();

    expect(
      screen.getByRole("heading", { name: "photos" }),
    ).toBeInTheDocument();
    expect(screen.getByText("photos-mirror")).toBeInTheDocument();
    expect(screen.getByText("photos-arch")).toBeInTheDocument();
    // Lag stats render: lagging replica should show 42 objects / 1.0 KB
    expect(screen.getByText("42 / 1.0 KB")).toBeInTheDocument();
    // Status badge uses the worst-health rollup.
    expect(screen.getByTestId("federation-status-badge")).toHaveTextContent(
      "lagging",
    );
  });

  it("'Resync now' calls the resync mutation with the federation id", async () => {
    resyncMutate.mockResolvedValueOnce({ id: "fb-1", status: "queued" });

    renderDetail();

    await userEvent.click(screen.getByTestId("resync-button"));
    expect(resyncMutate).toHaveBeenCalledTimes(1);
    expect(resyncMutate).toHaveBeenCalledWith("fb-1");
  });

  it("clicking 'Promote to primary' opens the confirm Dialog and the confirm button fires failover", async () => {
    failoverMutate.mockResolvedValueOnce({ ...baseFederation });

    renderDetail();

    // Click the promote button on the second replica (wasabi-arch).
    await userEvent.click(screen.getByTestId("promote-r-wasabi-photos-arch"));

    // Dialog renders the new primary as "wasabi-arch / photos-arch".
    expect(screen.getByTestId("promote-new-primary")).toHaveTextContent(
      "wasabi-arch / photos-arch",
    );

    await userEvent.click(screen.getByTestId("confirm-promote"));

    expect(failoverMutate).toHaveBeenCalledTimes(1);
    expect(failoverMutate).toHaveBeenCalledWith({
      id: "fb-1",
      body: {
        newPrimaryRegionId: "r-wasabi",
        newPrimaryBucket: "photos-arch",
      },
    });
  });

  it("'Delete' confirms via window.confirm and calls the delete mutation on accept", async () => {
    deleteMutate.mockResolvedValueOnce(undefined);
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);

    renderDetail();

    await userEvent.click(screen.getByTestId("delete-button"));
    expect(confirmSpy).toHaveBeenCalledTimes(1);
    expect(deleteMutate).toHaveBeenCalledWith("fb-1");

    confirmSpy.mockRestore();
  });

  it("'Delete' is a no-op when window.confirm is dismissed", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(false);

    renderDetail();

    await userEvent.click(screen.getByTestId("delete-button"));
    expect(deleteMutate).not.toHaveBeenCalled();

    confirmSpy.mockRestore();
  });

  it("'Auto-failover armed' badge is hidden when policy.autoFailover is false", () => {
    renderDetail();
    expect(
      screen.queryByTestId("auto-failover-armed-badge"),
    ).not.toBeInTheDocument();
  });

  it("'Auto-failover armed' badge renders with the policy window when enabled", () => {
    vi.mocked(useFederation).mockReturnValue({
      data: {
        ...baseFederation,
        policy: {
          ...baseFederation.policy,
          autoFailover: true,
          autoFailoverSec: 120,
        },
      },
      isLoading: false,
      error: null,
    } as any);

    renderDetail();

    const badge = screen.getByTestId("auto-failover-armed-badge");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveTextContent("Auto-failover armed");
    expect(badge).toHaveTextContent("2m");
  });
});
