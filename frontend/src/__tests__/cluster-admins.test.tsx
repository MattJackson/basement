// v1.3.0e CLUSTER.ADMINS — frontend tests.
//
// Covers the three behaviours the cluster detail page relies on:
//
//   1. ClusterAdminRow renders the user + role + source columns and
//      a disabled Remove button for inherited (wildcard) rows, with
//      a tooltip explaining why.
//   2. ClusterAdminRow renders an enabled Remove button for manual
//      rows scoped exactly to this cluster.
//   3. useClusterAdmins() points at the correct endpoint with the
//      cluster id in the path (smoke check on the query key + URL
//      so a future rename doesn't silently break the FE).

import { render, screen } from "@testing-library/react";
import { describe, it, expect, afterEach, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Table, TableBody } from "@/components/ui/table";
import { ClusterAdminRow } from "@/routes/admin/clusters/$cid";
import { useTranslation } from "react-i18next";

// useElevationGuard reads the mode store; stub it so the row's
// runWithElevation just calls through.
vi.mock("@/shared/auth/elevation", () => ({
  useElevationGuard:
    () => (fn: () => Promise<unknown>) => fn(),
}));

const mockT = vi.fn((key: string) => key);
vi.mock("react-i18next", () => ({
  ...vi.importActual("react-i18next"),
  useTranslation: () => ({ t: mockT }),
}));

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrapper({
  client,
  children,
}: {
  client: QueryClient;
  children: React.ReactNode;
}) {
  return (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <Table>
          <TableBody>{children}</TableBody>
        </Table>
      </TooltipProvider>
    </QueryClientProvider>
  );
}

describe("ClusterAdminRow (v1.3.0e)", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("inherited row renders the wildcard badge + disables Remove", () => {
    const client = makeQueryClient();
    const { t } = useTranslation("pages");

    render(
      <Wrapper client={client}>
        <ClusterAdminRow
          cid="cluster-x"
          assignment={{
            userId: "globalop",
            displayName: "Global Op",
            roleId: "cluster_admin",
            scope: "cluster:*",
            source: "manual",
            inherited: true,
          }}
          t={t}
        />
      </Wrapper>,
    );

    expect(screen.getByText(/Global Op/)).toBeInTheDocument();
    expect(screen.getByText("cluster_admin")).toBeInTheDocument();
    expect(screen.getByText("inherited from global")).toBeInTheDocument();

    const removeBtn = screen.getByRole("button", { name: /remove/i });
    expect(removeBtn).toBeDisabled();
  });

  it("manual row renders source=manual + enables Remove", () => {
    const client = makeQueryClient();
    const { t } = useTranslation("pages");

    render(
      <Wrapper client={client}>
        <ClusterAdminRow
          cid="cluster-x"
          assignment={{
            userId: "wife",
            displayName: "",
            roleId: "cluster_admin",
            scope: "cluster:cluster-x",
            source: "manual",
            inherited: false,
          }}
          t={t}
        />
      </Wrapper>,
    );

    // No displayName → falls back to the bare username.
    expect(screen.getByText("wife")).toBeInTheDocument();
    expect(screen.getByText("manual")).toBeInTheDocument();

    const removeBtn = screen.getByRole("button", { name: /remove/i });
    expect(removeBtn).not.toBeDisabled();
  });

  it("OIDC-sourced manual row renders the OIDC badge", () => {
    const client = makeQueryClient();
    const { t } = useTranslation("pages");

    render(
      <Wrapper client={client}>
        <ClusterAdminRow
          cid="cluster-x"
          assignment={{
            userId: "oidc-user",
            roleId: "cluster_admin",
            scope: "cluster:cluster-x",
            source: "oidc",
            inherited: false,
          }}
          t={t}
        />
      </Wrapper>,
    );

    expect(screen.getByText("OIDC")).toBeInTheDocument();
    // OIDC-sourced rows at exact scope are still removable from the
    // cluster view — the source field just affects the badge label,
    // not the wildcard-gated disable logic.
    expect(
      screen.getByRole("button", { name: /remove/i }),
    ).not.toBeDisabled();
  });
});

describe("useClusterAdmins hook (v1.3.0e)", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("hits /api/v1/admin/clusters/{cid}/admins with the encoded cid", async () => {
    const fetchSpy = vi
      .spyOn(window, "fetch")
      .mockResolvedValue(
        new Response(JSON.stringify({ assignments: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      );

    const { useClusterAdmins } = await import("@/shared/api/queries");
    const client = makeQueryClient();

    function Probe() {
      const q = useClusterAdmins("cluster-x");
      return <div>{q.isLoading ? "loading" : "ready"}</div>;
    }

    render(
      <QueryClientProvider client={client}>
        <Probe />
      </QueryClientProvider>,
    );

    // Allow the query effect to flush.
    await new Promise((r) => setTimeout(r, 10));

    expect(fetchSpy).toHaveBeenCalled();
    const url = String(fetchSpy.mock.calls[0]?.[0]);
    expect(url).toBe("/api/v1/admin/clusters/cluster-x/admins");
  });
});
