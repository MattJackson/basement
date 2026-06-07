// ADR-0009 Phase D live-edge regression.
//
// The cluster-detail page renders the WIRING surface for anyone with
// cluster.wiring.read (UI Admin on any cluster + cluster-admin on
// theirs) but must only fetch/render the CONTENTS surface
// (buckets/keys/encryption-admins/lock-status) when the viewer also
// holds cluster.contents.read. A UI Admin holds wiring.read but NOT
// contents.read — so the contents queries must be DISABLED for them
// (enabled=false) and never fire a request that would 403.
//
// We assert at the query-hook boundary: the page passes `enabled` to
// useClusterBuckets/useClusterKeys/useClusterLockStatus reflecting the
// viewer's contents capability.

import type { ComponentType, ReactNode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render } from "@testing-library/react";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
    useNavigate: () => vi.fn(),
    createFileRoute: () => (config: { component: ComponentType }) => ({
      component: config.component,
      useParams: () => ({ cid: "cluster-A" }),
    }),
  };
});

const useClusterBuckets = vi.fn(() => ({ data: [], isLoading: false }));
const useClusterKeys = vi.fn(() => ({ data: [], isLoading: false }));
const useClusterLockStatus = vi.fn(() => ({ data: undefined, isLoading: false }));
const useClusterAdmins = vi.fn(() => ({ data: undefined, isLoading: false, error: null }));

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useGetCluster: () => ({
      data: { id: "cluster-A", label: "Cluster A", driver: "garage-v1", color: "#fff" },
      isLoading: false,
      error: null,
    }),
    useNodes: () => ({ data: [] }),
    useCapabilities: () => ({ data: { layout: "readonly" } }),
    useTestClusterQuery: () => ({ data: null, isFetching: false, isPending: false, refetch: vi.fn() }),
    useClusterBuckets,
    useClusterKeys,
    useClusterLockStatus,
    useClusterAdmins,
    useBucket: () => ({ data: undefined }),
    useKey: () => ({ data: undefined }),
    usePolicies: () => ({ data: { roles: [] } }),
    useLockCluster: () => ({ mutate: vi.fn(), isPending: false }),
    useAddClusterAdmin: () => ({ mutate: vi.fn(), isPending: false }),
    useRemoveClusterAdmin: () => ({ mutate: vi.fn(), isPending: false }),
    useAssignRole: () => ({ mutateAsync: vi.fn(), isPending: false }),
    useUnassignRole: () => ({ mutateAsync: vi.fn(), isPending: false }),
  };
});

vi.mock("@/shared/api/mutations", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useDeleteCluster: () => ({ mutateAsync: vi.fn(), isPending: false }),
  };
});

vi.mock("@/shared/auth/elevation", () => ({
  useElevationGuard: () => async (op: () => Promise<unknown>) => op(),
}));

vi.mock("@/shared/layout/adminPage", () => ({
  adminPage: (C: ComponentType) => C,
}));

vi.mock("@/shared/auth/clusterUnlock", () => ({
  useClusterUnlockPrompt: () => vi.fn(),
}));

// useUser drives useCan via the capability list. Swap per test.
let mockCaps: string[] = [];
vi.mock("@/shared/auth/useUser", () => ({
  useUser: () => ({
    data: { username: "matthew", capabilities: mockCaps },
    isLoading: false,
    isError: false,
  }),
}));

async function renderDetail() {
  const { Route } = await import("@/routes/admin/clusters/$cid/index");
  const Comp = (Route as unknown as { component: ComponentType }).component;
  return render(<Comp />);
}

describe("cluster detail — ADR-0009 contents gating", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("UI Admin (wiring.read, NO contents.read) DISABLES the contents queries", async () => {
    mockCaps = [
      "cluster.wiring.read",
      "cluster.wiring.list",
      "cluster.wiring.update",
      "cluster.wiring.delete",
    ];
    await renderDetail();

    // Each contents hook is called with enabled=false so no request
    // fires (and no 403 toast surfaces).
    expect(useClusterBuckets).toHaveBeenCalledWith("cluster-A", false);
    expect(useClusterKeys).toHaveBeenCalledWith("cluster-A", false);
    expect(useClusterLockStatus).toHaveBeenCalledWith("cluster-A", false);
    // The admins section isn't even rendered for a UI Admin, so its
    // hook never runs.
    expect(useClusterAdmins).not.toHaveBeenCalled();
  });

  it("Cluster Admin (contents.read) ENABLES the contents queries", async () => {
    mockCaps = ["cluster.wiring.read", "cluster.contents.read"];
    await renderDetail();

    expect(useClusterBuckets).toHaveBeenCalledWith("cluster-A", true);
    expect(useClusterKeys).toHaveBeenCalledWith("cluster-A", true);
    expect(useClusterLockStatus).toHaveBeenCalledWith("cluster-A", true);
    // The admins section IS rendered, firing its hook.
    expect(useClusterAdmins).toHaveBeenCalled();
  });
});
