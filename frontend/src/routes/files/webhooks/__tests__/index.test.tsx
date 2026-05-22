// Tests for /files/webhooks list page (v1.7.0e).
//
// Renders the list with seeded query data so we can assert the empty
// state CTA + the table rendering (name link, target URL, events,
// status pill, last-delivery summary, failure-count badge, action
// buttons) without touching the network. The useWebhooks hook key is
// ["user", "webhooks"] — same shape as v1.5 backups + v1.6
// federations.

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

import { Route } from "@/routes/files/webhooks/index";

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

describe("WebhooksListPage", () => {
  it("renders the empty state when there are no webhooks", async () => {
    queryClient.setQueryData(["user", "webhooks"], []);

    renderList();

    expect(
      screen.getByRole("heading", { name: "Webhooks" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/No webhooks configured/)).toBeInTheDocument();

    // Empty-state CTA navigates to the wizard.
    const buttons = screen.getAllByRole("button", { name: /\+ New webhook/ });
    const lastBtn = buttons[buttons.length - 1];
    expect(lastBtn).toBeDefined();
    await userEvent.click(lastBtn!);
    expect(navigateMock).toHaveBeenCalledWith({ to: "/files/webhooks/new" });
  });

  it("renders one row per webhook with name, target URL, events, and status", () => {
    queryClient.setQueryData(
      ["user", "webhooks"],
      [
        {
          id: "wh-1",
          ownerUserId: "u1",
          name: "ci-deploy",
          targetUrl: "https://ci.example.com/hooks/basement",
          events: ["object.created"],
          hasSecret: true,
          enabled: true,
          createdAt: "2026-05-22T00:00:00Z",
          updatedAt: "2026-05-22T00:00:00Z",
          lastDelivery: {
            deliveredAt: "2026-05-22T10:00:00Z",
            httpStatus: 200,
            durationMs: 84,
            success: true,
          },
        },
        {
          id: "wh-2",
          ownerUserId: "u1",
          name: "broken-target",
          targetUrl: "https://oops.example.com/hooks",
          events: ["object.deleted", "object.modified"],
          hasSecret: true,
          enabled: false,
          failureCount: 4,
          createdAt: "2026-05-22T00:00:00Z",
          updatedAt: "2026-05-22T00:00:00Z",
          lastDelivery: {
            deliveredAt: "2026-05-22T08:00:00Z",
            httpStatus: 502,
            durationMs: 120,
            success: false,
            error: "bad gateway",
          },
        },
      ],
    );

    renderList();

    // Both webhook names render as links into the detail route.
    expect(screen.getByRole("link", { name: "ci-deploy" })).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "broken-target" }),
    ).toBeInTheDocument();

    // Event badges render the full event-type string verbatim.
    expect(screen.getByText("object.created")).toBeInTheDocument();
    expect(screen.getByText("object.deleted")).toBeInTheDocument();
    expect(screen.getByText("object.modified")).toBeInTheDocument();

    // Status pills carry the enabled / disabled affordance.
    expect(screen.getByTestId("status-pill-wh-1")).toHaveTextContent("enabled");
    expect(screen.getByTestId("status-pill-wh-2")).toHaveTextContent("disabled");

    // Failure-count badge shows up only on the broken row.
    expect(screen.getByTestId("failure-badge-wh-2")).toHaveTextContent(
      "4 fail",
    );
    expect(screen.queryByTestId("failure-badge-wh-1")).not.toBeInTheDocument();

    // Action buttons render with stable testids for downstream tests.
    expect(screen.getByTestId("test-button-wh-1")).toBeInTheDocument();
    expect(screen.getByTestId("toggle-button-wh-1")).toHaveTextContent(
      "Disable",
    );
    expect(screen.getByTestId("toggle-button-wh-2")).toHaveTextContent(
      "Enable",
    );
    expect(screen.getByTestId("delete-button-wh-1")).toBeInTheDocument();
  });

  it("'+ New webhook' header button navigates to the wizard", async () => {
    queryClient.setQueryData(["user", "webhooks"], []);
    renderList();

    const headerCta = screen.getAllByRole("button", {
      name: /\+ New webhook/,
    })[0];
    expect(headerCta).toBeDefined();
    await userEvent.click(headerCta!);
    expect(navigateMock).toHaveBeenCalledWith({ to: "/files/webhooks/new" });
  });

  it("'Test' button is disabled on a disabled webhook (matches backend rule)", () => {
    queryClient.setQueryData(
      ["user", "webhooks"],
      [
        {
          id: "wh-1",
          ownerUserId: "u1",
          name: "disabled-one",
          targetUrl: "https://example.com",
          events: ["object.created"],
          hasSecret: true,
          enabled: false,
          createdAt: "2026-05-22T00:00:00Z",
          updatedAt: "2026-05-22T00:00:00Z",
        },
      ],
    );
    renderList();
    expect(screen.getByTestId("test-button-wh-1")).toBeDisabled();
  });
});
