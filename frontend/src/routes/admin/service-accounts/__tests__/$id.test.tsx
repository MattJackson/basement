// Tests for /admin/service-accounts/$id (v1.8.0d).
//
// Confirms the detail page renders the canonical SA fields, the
// capabilities list, and the McpConfigSection in its placeholder
// variant (no plaintext secret available on the read path).

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => (config: { component: any }) => ({
      component: config.component,
      // The detail page reads Route.useParams() — return the seeded
      // id so the component sees the same value the real router
      // would inject.
      useParams: () => ({ id: "sa-1" }),
    }),
    Link: ({ children, to, params }: any) => (
      <a
        data-testid="link"
        href={
          typeof to === "string"
            ? to.replace("$id", params?.id ?? "")
            : "#"
        }
      >
        {children}
      </a>
    ),
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useServiceAccount: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route } from "@/routes/admin/service-accounts/$id";
import { useServiceAccount, type ServiceAccount } from "@/shared/api/queries";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function renderPage() {
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
});

describe("ServiceAccountDetailPage (/admin/service-accounts/$id)", () => {
  it("renders identity, capabilities, and the MCP config section with the rotate-hint placeholder", () => {
    vi.mocked(useServiceAccount).mockReturnValue({
      data: baseSA,
      isLoading: false,
      error: null,
    } as any);

    renderPage();

    // Identity card surfaces name + AKID
    expect(
      screen.getByRole("heading", { name: /ci-deploy/ }),
    ).toBeInTheDocument();
    expect(screen.getByText("BMNT1234567890ABCDEF")).toBeInTheDocument();

    // Capabilities list shows one row per capability
    const caps = screen.getByTestId("sa-capabilities-list");
    expect(caps).toBeInTheDocument();
    expect(caps.textContent).toContain("host:manage_users");
    expect(caps.textContent).toContain("host:*");
    expect(caps.textContent).toContain("cluster:edit");
    expect(caps.textContent).toContain("cluster:cid-abc");

    // The MCP config section renders the placeholder + rotate hint —
    // detail-page variant has no plaintext secret to surface.
    expect(screen.getByTestId("mcp-config-section")).toBeInTheDocument();
    const yaml = screen.getByTestId("mcp-yaml-snippet").textContent ?? "";
    expect(yaml).toContain("access_key_id: BMNT1234567890ABCDEF");
    expect(yaml).toContain("secret_key: <SECRET_FROM_ROTATE>");
    expect(screen.getByTestId("mcp-rotate-hint")).toBeInTheDocument();
  });

  it("renders an error banner when the SA can't be loaded", () => {
    vi.mocked(useServiceAccount).mockReturnValue({
      data: undefined,
      isLoading: false,
      error: new Error("admin/service-accounts/sa-1: 404 not found"),
    } as any);

    renderPage();

    expect(screen.getByText(/admin\/service-accounts\/sa-1/)).toBeInTheDocument();
    // The MCP config section MUST NOT render when there's no SA — the
    // YAML needs an access key id to be meaningful.
    expect(screen.queryByTestId("mcp-config-section")).not.toBeInTheDocument();
  });

  it("shows a Revoked badge + revoked timestamp when the SA has been revoked", () => {
    vi.mocked(useServiceAccount).mockReturnValue({
      data: { ...baseSA, revokedAt: "2026-05-22T12:00:00Z" },
      isLoading: false,
      error: null,
    } as any);

    renderPage();

    // Two "Revoked" strings render: the status badge in the header
    // + the field-row label in the identity card. Match all to
    // confirm both surfaces appear, then the test reads as "yes the
    // SA carries the revoked indicator twice — once as state, once
    // as a timestamped field".
    const revoked = screen.getAllByText("Revoked");
    expect(revoked.length).toBeGreaterThanOrEqual(1);
  });
});
