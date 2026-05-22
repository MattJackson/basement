// v1.4.0b: paginated key permissions screen. Filter + pagination +
// only-granted toggle + sticky save survive across page navigation
// (checkbox state lives in a single editPermissions array, not per
// page). These tests exercise the four scope acceptance gates:
//
//   1. Filter narrows the visible list.
//   2. Pagination works (Next/Prev + page indicator + per-page slice).
//   3. "Only granted" toggle hides ungranted rows.
//   4. Save persists state across pagination — toggling a checkbox on
//      page 1, paginating to page 2, then back to page 1, retains the
//      tick.
//
// The route component reads useElevationGuard() out of the
// ElevationProvider context; we mock the hook so the test doesn't need
// the provider tree.

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children }: any) => <a>{children}</a>,
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
      useParams: () => ({ cid: "cluster-A", id: "key-1" }),
    }),
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useKey: vi.fn(),
    useClusterBuckets: vi.fn(),
    useGetCluster: vi.fn(),
  };
});

vi.mock("@/shared/api/mutations", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUpdateKeyPermissions: vi.fn(),
    useDeleteKey: vi.fn(),
  };
});

vi.mock("@/shared/auth/elevation", () => ({
  // useElevationGuard returns a passthrough so mutation.mutateAsync is
  // invoked directly (no elevation modal in tests).
  useElevationGuard: () => async (op: () => Promise<unknown>) => op(),
}));

vi.mock("@/shared/layout/adminPage", () => ({
  // adminPage normally wraps with the admin chrome (header, sidebar,
  // capability guard). For unit tests we render the inner component
  // directly so we don't have to mock the chrome's data dependencies.
  adminPage: (C: any) => C,
}));

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/admin/clusters/$cid/keys/$id";
import { useKey, useClusterBuckets, useGetCluster } from "@/shared/api/queries";
import { useUpdateKeyPermissions, useDeleteKey } from "@/shared/api/mutations";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderRoute() {
  const Component = (Route as unknown as { component: React.ComponentType }).component;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>,
  );
}

// Build a synthetic cluster with N buckets, indexed bucket-0001 ..
// bucket-NNNN. The first 3 are granted on the key (read=true) so the
// "Only granted" toggle has something visible.
function makeCluster(bucketCount: number) {
  const buckets = Array.from({ length: bucketCount }, (_, i) => ({
    id: `bid-${String(i + 1).padStart(4, "0")}`,
    connectionId: "cluster-A",
    aliases: [`bucket-${String(i + 1).padStart(4, "0")}`],
  }));
  return buckets;
}

beforeEach(() => {
  vi.clearAllMocks();

  vi.mocked(useGetCluster).mockReturnValue({
    data: { id: "cluster-A", driver: "garage" },
    isLoading: false,
    error: null,
  } as any);

  vi.mocked(useUpdateKeyPermissions).mockReturnValue({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
    isError: false,
  } as any);

  vi.mocked(useDeleteKey).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
  } as any);
});

describe("/admin/clusters/$cid/keys/$id — paginated permissions (v1.4.0b)", () => {
  it("filter narrows the visible bucket list", async () => {
    const clusterBuckets = makeCluster(120);
    vi.mocked(useClusterBuckets).mockReturnValue({
      data: clusterBuckets,
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useKey).mockReturnValue({
      data: {
        id: "key-1",
        name: "test-key",
        accessKeyId: "AKIA...",
        buckets: [
          { bucketId: "bid-0001", globalAliases: ["bucket-0001"], read: true, write: false, owner: false },
        ],
      },
      isLoading: false,
      error: null,
    } as any);

    renderRoute();
    await userEvent.click(screen.getByText(/Edit permissions/));

    // First page (50 rows) — bucket-0001 (granted) is in the first
    // page since granted rows sort to the top.
    expect(screen.getByText("bucket-0001")).toBeInTheDocument();
    // bucket-0099 is in the second page.
    expect(screen.queryByText("bucket-0099")).not.toBeInTheDocument();

    // Type a narrow filter — only bucket-0099 matches.
    const filter = screen.getByTestId("key-perms-filter");
    await userEvent.type(filter, "bucket-0099");
    expect(screen.getByText("bucket-0099")).toBeInTheDocument();
    expect(screen.queryByText("bucket-0001")).not.toBeInTheDocument();

    // Page indicator reflects the narrowed list (1 result).
    expect(screen.getByTestId("key-perms-page-indicator")).toHaveTextContent(/Showing 1-1 of 1/);
  });

  it("pagination Next/Prev moves through pages", async () => {
    const clusterBuckets = makeCluster(120);
    vi.mocked(useClusterBuckets).mockReturnValue({
      data: clusterBuckets,
      isLoading: false,
      error: null,
    } as any);
    vi.mocked(useKey).mockReturnValue({
      data: { id: "key-1", name: "test-key", buckets: [] },
      isLoading: false,
      error: null,
    } as any);

    renderRoute();
    await userEvent.click(screen.getByText(/Edit permissions/));

    // Page 1: bucket-0001 .. bucket-0050.
    expect(screen.getByText("bucket-0001")).toBeInTheDocument();
    expect(screen.getByText("bucket-0050")).toBeInTheDocument();
    expect(screen.queryByText("bucket-0051")).not.toBeInTheDocument();
    expect(screen.getByTestId("key-perms-page-indicator")).toHaveTextContent(/Showing 1-50 of 120/);
    expect(screen.getByTestId("key-perms-page-indicator")).toHaveTextContent(/page 1 of 3/);

    // Next → page 2 (bucket-0051 .. bucket-0100).
    await userEvent.click(screen.getByTestId("key-perms-next"));
    expect(screen.getByText("bucket-0051")).toBeInTheDocument();
    expect(screen.getByText("bucket-0100")).toBeInTheDocument();
    expect(screen.queryByText("bucket-0001")).not.toBeInTheDocument();
    expect(screen.getByTestId("key-perms-page-indicator")).toHaveTextContent(/page 2 of 3/);

    // Prev → back to page 1.
    await userEvent.click(screen.getByTestId("key-perms-prev"));
    expect(screen.getByText("bucket-0001")).toBeInTheDocument();
    expect(screen.queryByText("bucket-0051")).not.toBeInTheDocument();
  });

  it("'Only granted' toggle filters to rows with at least one permission bit set", async () => {
    const clusterBuckets = makeCluster(60);
    vi.mocked(useClusterBuckets).mockReturnValue({
      data: clusterBuckets,
      isLoading: false,
      error: null,
    } as any);
    // Two granted (bucket-0001 read, bucket-0002 write); 58 ungranted.
    vi.mocked(useKey).mockReturnValue({
      data: {
        id: "key-1",
        name: "test-key",
        buckets: [
          { bucketId: "bid-0001", globalAliases: ["bucket-0001"], read: true, write: false, owner: false },
          { bucketId: "bid-0002", globalAliases: ["bucket-0002"], read: false, write: true, owner: false },
        ],
      },
      isLoading: false,
      error: null,
    } as any);

    renderRoute();
    await userEvent.click(screen.getByText(/Edit permissions/));

    // Default: ALL 60 buckets visible (page 1 shows 50 of 60).
    expect(screen.getByTestId("key-perms-page-indicator")).toHaveTextContent(/Showing 1-50 of 60/);

    // Flip the "Only granted" toggle.
    await userEvent.click(screen.getByTestId("key-perms-only-granted"));
    expect(screen.getByTestId("key-perms-page-indicator")).toHaveTextContent(/Showing 1-2 of 2/);
    expect(screen.getByText("bucket-0001")).toBeInTheDocument();
    expect(screen.getByText("bucket-0002")).toBeInTheDocument();
    expect(screen.queryByText("bucket-0003")).not.toBeInTheDocument();
  });

  it("checkbox state survives pagination — tick on page 1, navigate, tick is preserved on save", async () => {
    const clusterBuckets = makeCluster(120);
    vi.mocked(useClusterBuckets).mockReturnValue({
      data: clusterBuckets,
      isLoading: false,
      error: null,
    } as any);
    const mutateAsync = vi.fn().mockResolvedValue({});
    vi.mocked(useUpdateKeyPermissions).mockReturnValue({
      mutateAsync,
      isPending: false,
      isError: false,
    } as any);
    vi.mocked(useKey).mockReturnValue({
      data: { id: "key-1", name: "test-key", buckets: [] },
      isLoading: false,
      error: null,
    } as any);

    renderRoute();
    await userEvent.click(screen.getByText(/Edit permissions/));

    // Tick the read checkbox on bid-0005 (page 1).
    const readBox = document.getElementById("read-bid-0005")!;
    expect(readBox).toBeTruthy();
    await userEvent.click(readBox);

    // Paginate forward then back.
    await userEvent.click(screen.getByTestId("key-perms-next"));
    await userEvent.click(screen.getByTestId("key-perms-next"));
    await userEvent.click(screen.getByTestId("key-perms-prev"));
    await userEvent.click(screen.getByTestId("key-perms-prev"));

    // Save.
    const saveButton = within(screen.getByTestId("key-perms-sticky-save")).getByText("Save changes");
    await userEvent.click(saveButton);

    // The mutation should have been called with the full 120-row list,
    // with bid-0005's read flag = true.
    expect(mutateAsync).toHaveBeenCalledTimes(1);
    const call = mutateAsync.mock.calls[0]![0];
    expect(call.cid).toBe("cluster-A");
    expect(call.id).toBe("key-1");
    expect(call.permissions).toHaveLength(120);
    const bid5 = call.permissions.find((p: any) => p.bucketId === "bid-0005");
    expect(bid5).toBeDefined();
    expect(bid5.read).toBe(true);
    expect(bid5.write).toBe(false);
    expect(bid5.owner).toBe(false);
  });
});
