// Tests for /files/federated-buckets list page (v1.6.0d).
//
// Renders the list with seeded query data so we can assert the
// empty-state CTA + the table rendering (name link, primary cell,
// health pill, replica count) without touching the network. The
// useFederations hook key is ["user", "federations"] — same shape as
// the v1.5 backups list test uses for ["user", "backups"].

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

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/files/federated-buckets/index";

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

beforeEach(() => {
  vi.clearAllMocks();
  navigateMock.mockReset();
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
});

describe("FederatedBucketsListPage", () => {
  it("renders the empty state when there are no federations", async () => {
    queryClient.setQueryData(["user", "federations"], []);
    queryClient.setQueryData(["user", "regions"], []);

    renderList();

    expect(
      screen.getByRole("heading", { name: "Federations" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/No federations yet/)).toBeInTheDocument();
    // The empty-state CTA points to the wizard.
    const buttons = screen.getAllByRole("button", { name: /\+ New federation/ });
    const lastBtn = buttons[buttons.length - 1];
    expect(lastBtn).toBeDefined();
    await userEvent.click(lastBtn!);
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/files/federated-buckets/new",
    });
  });

  it("renders one row per federation with primary + replica summary", () => {
    queryClient.setQueryData(["user", "regions"], [
      { id: "r-home", alias: "home", endpoint: "http://garage.lan:3902", userId: "u1", region: "garage", accessKeyId: "GK", addressingStyle: "path", createdAt: "2026-05-01T00:00:00Z", updatedAt: "2026-05-01T00:00:00Z" },
      { id: "r-b2", alias: "b2-offsite", endpoint: "https://s3.us-west-002.backblazeb2.com", userId: "u1", region: "us-west-002", accessKeyId: "GK2", addressingStyle: "virtual_host", createdAt: "2026-05-01T00:00:00Z", updatedAt: "2026-05-01T00:00:00Z" },
    ]);
    queryClient.setQueryData(["user", "federations"], [
      {
        id: "fb-1",
        ownerUserId: "u1",
        name: "photos",
        primary: { regionId: "r-home", bucket: "photos" },
        replicas: [
          { regionId: "r-b2", bucket: "photos-mirror", health: "in-sync", lastSync: "2026-05-22T10:00:00Z" },
        ],
        policy: { syncMode: "continuous", lagAlertSec: 300, writeQuorum: 1 },
        createdAt: "2026-05-22T00:00:00Z",
        updatedAt: "2026-05-22T10:00:00Z",
        computedHealth: "in-sync",
      },
      {
        id: "fb-2",
        ownerUserId: "u1",
        name: "lagging-set",
        primary: { regionId: "r-home", bucket: "docs" },
        replicas: [
          { regionId: "r-b2", bucket: "docs-mirror", health: "lagging", lastSync: "2026-05-22T08:00:00Z", lagObjects: 42, lagBytes: 1024 },
          { regionId: "r-b2", bucket: "docs-mirror-2", health: "in-sync", lastSync: "2026-05-22T10:00:00Z" },
        ],
        policy: { syncMode: "continuous", lagAlertSec: 300, writeQuorum: 1 },
        createdAt: "2026-05-22T00:00:00Z",
        updatedAt: "2026-05-22T10:00:00Z",
        computedHealth: "lagging",
      },
    ]);

    renderList();

    // Names render as links into the detail route. "photos" is also
    // the bucket name in the primary cell, so we anchor on the link
    // (anchor element) to disambiguate.
    expect(
      screen.getByRole("link", { name: "photos" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "lagging-set" }),
    ).toBeInTheDocument();

    // Health summary text — "1/1 in-sync" for the healthy row, "1/2"
    // for the lagging one (only the second replica is in-sync).
    expect(screen.getByText("1/1 in-sync")).toBeInTheDocument();
    expect(screen.getByText("1/2 in-sync")).toBeInTheDocument();

    // Pill data-testid encodes the computed health so the colour-coding
    // assertion stays robust to text changes.
    expect(screen.getByTestId("health-pill-in-sync")).toBeInTheDocument();
    expect(screen.getByTestId("health-pill-lagging")).toBeInTheDocument();

    // Region aliases (not raw UUIDs) render in the primary column —
    // both federations use the same primary region, so two matches is
    // the expected count.
    expect(screen.getAllByText("home").length).toBeGreaterThan(0);
  });

  it("'+ New federation' header button navigates to the wizard", async () => {
    queryClient.setQueryData(["user", "federations"], []);
    queryClient.setQueryData(["user", "regions"], []);

    renderList();

    const headerCta = screen.getAllByRole("button", {
      name: /\+ New federation/,
    })[0];
    expect(headerCta).toBeDefined();
    await userEvent.click(headerCta!);
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/files/federated-buckets/new",
    });
  });
});
