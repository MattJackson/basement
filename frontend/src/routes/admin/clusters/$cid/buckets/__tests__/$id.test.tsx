// Regression coverage for handleAliasSave (audit fe3, P2 CORRECTNESS).
//
// The bucket-detail header edits the PRIMARY alias (index 0). The old
// implementation rebuilt the alias list as
//   [aliasInput, ...aliases.filter((a) => a !== aliases[0])]
// which removed EVERY alias equal to the old primary by value — so a
// bucket whose primary string happened to collide with a secondary
// alias silently dropped the secondary on save. The fix replaces only
// the index-0 slot and preserves all other aliases by position.
//
// We render the inner component (adminPage chrome + elevation guard are
// mocked to passthroughs) and drive the click-to-edit → Save flow,
// asserting the exact `aliases` array sent to the update mutation.

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children }: any) => <a>{children}</a>,
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
      useParams: () => ({ cid: "cluster-A", id: "bucket-1" }),
    }),
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useBucket: vi.fn(),
    useClusterKeys: vi.fn(),
    useGetCluster: vi.fn(),
  };
});

vi.mock("@/shared/api/mutations", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useUpdateBucket: vi.fn(),
    useDeleteBucket: vi.fn(),
    useUpdateKeyPermissions: vi.fn(),
  };
});

vi.mock("@/shared/auth/elevation", () => ({
  // Passthrough — invoke the mutation directly, no elevation modal.
  useElevationGuard: () => async (op: () => Promise<unknown>) => op(),
}));

vi.mock("@/shared/layout/adminPage", () => ({
  adminPage: (C: any) => C,
}));

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/admin/clusters/$cid/buckets/$id";
import { useBucket, useClusterKeys, useGetCluster } from "@/shared/api/queries";
import {
  useUpdateBucket,
  useDeleteBucket,
  useUpdateKeyPermissions,
} from "@/shared/api/mutations";

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

const updateMutateAsync = vi.fn().mockResolvedValue(undefined);

function mockBucket(aliases: string[]) {
  vi.mocked(useBucket).mockReturnValue({
    data: {
      id: "bucket-1",
      aliases,
      bytes: 0,
      objects: 0,
      unfinishedUploads: 0,
      keys: [],
      quotas: {},
    },
    isLoading: false,
    error: null,
  } as any);
}

beforeEach(() => {
  vi.clearAllMocks();
  updateMutateAsync.mockClear();
  vi.mocked(useGetCluster).mockReturnValue({
    data: { id: "cluster-A", driver: "garage" },
    isLoading: false,
    error: null,
  } as any);
  vi.mocked(useClusterKeys).mockReturnValue({
    data: [],
    isLoading: false,
    error: null,
  } as any);
  vi.mocked(useUpdateBucket).mockReturnValue({
    mutateAsync: updateMutateAsync,
    isPending: false,
    isError: false,
    error: null,
  } as any);
  vi.mocked(useDeleteBucket).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  } as any);
  vi.mocked(useUpdateKeyPermissions).mockReturnValue({
    mutateAsync: vi.fn(),
    isPending: false,
    isError: false,
    error: null,
  } as any);
});

describe("AdminBucketDetail — handleAliasSave (audit fe3 P2)", () => {
  it("editing the primary alias preserves all secondary aliases", async () => {
    mockBucket(["primary", "secondary-a", "secondary-b"]);
    renderRoute();

    // Click the title to enter edit mode (seeds the input with the
    // current primary alias).
    await userEvent.click(screen.getByRole("button", { name: "primary" }));
    const input = screen.getByDisplayValue("primary");
    await userEvent.clear(input);
    await userEvent.type(input, "renamed-primary");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(updateMutateAsync).toHaveBeenCalledTimes(1);
    expect(updateMutateAsync).toHaveBeenCalledWith({
      cid: "cluster-A",
      id: "bucket-1",
      update: { aliases: ["renamed-primary", "secondary-a", "secondary-b"] },
    });
  });

  it("does not drop a secondary alias that collides in value with the old primary", async () => {
    // The footgun: secondary[0] === primary by value. The old
    // value-equality filter removed BOTH; the index-based fix keeps the
    // secondary copy intact.
    mockBucket(["dup", "dup", "keep"]);
    renderRoute();

    await userEvent.click(screen.getByRole("button", { name: "dup" }));
    const input = screen.getByDisplayValue("dup");
    await userEvent.clear(input);
    await userEvent.type(input, "new-name");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(updateMutateAsync).toHaveBeenCalledWith({
      cid: "cluster-A",
      id: "bucket-1",
      update: { aliases: ["new-name", "dup", "keep"] },
    });
  });

  it("handles a single-alias bucket (no secondaries to preserve)", async () => {
    mockBucket(["only"]);
    renderRoute();

    await userEvent.click(screen.getByRole("button", { name: "only" }));
    const input = screen.getByDisplayValue("only");
    await userEvent.clear(input);
    await userEvent.type(input, "only-renamed");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(updateMutateAsync).toHaveBeenCalledWith({
      cid: "cluster-A",
      id: "bucket-1",
      update: { aliases: ["only-renamed"] },
    });
  });
});
