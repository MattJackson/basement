// Tests for /admin/service-accounts list page (v1.7.0c).
//
// Renders the list with seeded query data so we can assert the
// empty-state CTA + the per-row actions (Rotate, Revoke) without
// touching the network. The useServiceAccounts hook key is
// ["admin", "service-accounts"] — same shape as the v0.9.0g policies
// list test uses for ["admin", "policies"].

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
    }),
    Link: ({ children, to }: any) => (
      <a data-testid="link" href={typeof to === "string" ? to : "#"}>
        {children}
      </a>
    ),
    useNavigate: () => navigateMock,
  };
});

// Elevation guard runs the wrapped fn directly in tests — no real
// elevation modal needed. The list page's row actions wrap their
// mutateAsync calls in runWithElevation, so the mock has to forward
// the call into the underlying mutate.
vi.mock("@/shared/auth/elevation", () => ({
  useElevationGuard:
    () =>
    <T,>(fn: () => Promise<T>) =>
      fn(),
}));

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route, saStatus } from "@/routes/admin/service-accounts/index";
import type { ServiceAccount } from "@/shared/api/queries";

let queryClient: QueryClient;

function renderList() {
  const Component = (Route as unknown as { component: React.ComponentType })
    .component;
  return render(
    <QueryClientProvider client={queryClient}>
      <Component />
    </QueryClientProvider>,
  );
}

const baseSA: ServiceAccount = {
  id: "sa-1",
  ownerUserId: "u-matt",
  name: "ci-deploy",
  accessKeyId: "BMNT1234567890ABCDEF",
  capabilities: [
    { id: "host:manage_users", scope: "host:*" },
    { id: "cluster:edit", scope: "cluster:cid-abc" },
  ],
  scopes: [],
  createdAt: "2026-05-22T00:00:00Z",
  lastUsedAt: "2026-05-22T10:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  navigateMock.mockReset();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
});

describe("ServiceAccountsPage", () => {
  it("renders the empty state when no service accounts exist", async () => {
    queryClient.setQueryData(["admin", "service-accounts"], []);

    renderList();

    expect(
      screen.getByRole("heading", { name: "Service accounts" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/No service accounts yet/)).toBeInTheDocument();

    // The header "+ New" button navigates to the mint route.
    await userEvent.click(screen.getByTestId("new-sa-button"));
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/admin/service-accounts/new",
    });
  });

  it("renders one row per service account with the per-row actions visible", async () => {
    queryClient.setQueryData(["admin", "service-accounts"], [
      baseSA,
      {
        ...baseSA,
        id: "sa-2",
        name: "backup-cron",
        accessKeyId: "BMNT9876543210FEDCBA",
        revokedAt: "2026-05-21T00:00:00Z",
      },
    ]);

    renderList();

    // Row 1 (active) — rotate enabled, revoke enabled.
    expect(screen.getByTestId("sa-row-sa-1")).toBeInTheDocument();
    expect(screen.getByText("ci-deploy")).toBeInTheDocument();
    expect(screen.getByText("BMNT1234567890ABCDEF")).toBeInTheDocument();

    // Row 2 (revoked) — hidden by default, shown when toggle is checked.
    expect(screen.queryByTestId("sa-row-sa-2")).not.toBeInTheDocument();

    const toggle = screen.getByTestId("show-revoked-toggle");
    await userEvent.click(toggle);

    expect(screen.getByTestId("sa-row-sa-2")).toBeInTheDocument();
    expect(screen.getByText("backup-cron")).toBeInTheDocument();
    expect(screen.getAllByText("Revoked")[0]).toBeInTheDocument();
  });

  it("filters rows by name + access key substring (case-insensitive)", async () => {
    queryClient.setQueryData(["admin", "service-accounts"], [
      baseSA,
      {
        ...baseSA,
        id: "sa-2",
        name: "backup-cron",
        accessKeyId: "BMNTABCDEFABCDEF1234",
      },
    ]);

    renderList();

    const searchBox = screen.getByPlaceholderText(/Search by name or access key/);
    await userEvent.type(searchBox, "backup");

    expect(screen.queryByTestId("sa-row-sa-1")).not.toBeInTheDocument();
    expect(screen.getByTestId("sa-row-sa-2")).toBeInTheDocument();

    await userEvent.clear(searchBox);
    await userEvent.type(searchBox, "BMNT1234");
    expect(screen.getByTestId("sa-row-sa-1")).toBeInTheDocument();
    expect(screen.queryByTestId("sa-row-sa-2")).not.toBeInTheDocument();
  });

  it("hides revoked SAs by default and shows them when toggle is checked", async () => {
    queryClient.setQueryData(["admin", "service-accounts"], [
      baseSA,
      {
        ...baseSA,
        id: "sa-revoked",
        name: "revoked-sa",
        accessKeyId: "BMNTREVOKEDKEY1234567890",
        revokedAt: "2026-01-01T00:00:00Z",
      },
    ]);

    renderList();

    expect(screen.getByTestId("sa-row-sa-1")).toBeInTheDocument();
    expect(screen.queryByTestId("sa-row-sa-revoked")).not.toBeInTheDocument();

    const toggle = screen.getByTestId("show-revoked-toggle");
    await userEvent.click(toggle);

    expect(screen.getByTestId("sa-row-sa-1")).toBeInTheDocument();
    expect(screen.getByTestId("sa-row-sa-revoked")).toBeInTheDocument();
  });
});

describe("saStatus", () => {
  it("returns revoked when RevokedAt is set", () => {
    expect(
      saStatus({ ...baseSA, revokedAt: "2026-05-21T00:00:00Z" }),
    ).toBe("revoked");
  });

  it("returns expired when ExpiresAt is in the past", () => {
    expect(
      saStatus({ ...baseSA, expiresAt: "2020-01-01T00:00:00Z" }),
    ).toBe("expired");
  });

  it("returns active when neither revoked nor expired", () => {
    expect(saStatus(baseSA)).toBe("active");
    expect(
      saStatus({ ...baseSA, expiresAt: "2099-01-01T00:00:00Z" }),
    ).toBe("active");
  });

  it("revoked beats expired (matches v1.7.0b middleware order)", () => {
    expect(
      saStatus({
        ...baseSA,
        revokedAt: "2026-05-21T00:00:00Z",
        expiresAt: "2020-01-01T00:00:00Z",
      }),
    ).toBe("revoked");
  });
});
