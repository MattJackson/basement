// Tests for /admin/service-accounts/new (v1.7.0c).
//
// Drives the mint form via userEvent and asserts:
//   - The capability picker exposes a checkbox per registry verb +
//     a search input that filters them.
//   - The submit button is gated until name + at least one
//     capability are set.
//   - On a successful create, the shown-once dialog renders, the
//     Copy buttons hit navigator.clipboard, and the Done button only
//     activates after the operator acknowledges.

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

vi.mock("@/shared/auth/elevation", () => ({
  useElevationGuard:
    () =>
    <T,>(fn: () => Promise<T>) =>
      fn(),
}));

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    usePolicies: vi.fn(),
    useCreateServiceAccount: vi.fn(),
  };
});

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Route, defaultScopeFor } from "@/routes/admin/service-accounts/new";
import {
  usePolicies,
  useCreateServiceAccount,
  type ServiceAccountWithSecret,
} from "@/shared/api/queries";

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

const mintResult: ServiceAccountWithSecret = {
  serviceAccount: {
    id: "sa-new",
    ownerUserId: "u-matt",
    name: "test-cli",
    accessKeyId: "BMNT1111222233334444",
    capabilities: [{ id: "host:manage_users", scope: "host:*" }],
    scopes: [],
    createdAt: "2026-05-22T00:00:00Z",
  },
  secret: "s3cret_plaintext_value_shown_once",
};

const mutateAsyncMock = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();
  navigateMock.mockReset();
  mutateAsyncMock.mockReset();

  vi.mocked(usePolicies).mockReturnValue({
    data: {
      capabilities: [
        { id: "host:manage_users", description: "Create / edit users" },
        { id: "host:manage_drivers", description: "Enable / disable drivers" },
        { id: "cluster:create", description: "Register a new cluster" },
        { id: "bucket:create", description: "Create buckets" },
        { id: "*:*", description: "Superuser wildcard" },
      ],
      roles: [],
      assignments: [],
    },
    isLoading: false,
    error: null,
  } as any);

  vi.mocked(useCreateServiceAccount).mockReturnValue({
    mutateAsync: mutateAsyncMock,
    isPending: false,
    error: null,
  } as any);
});

describe("NewServiceAccountPage (/admin/service-accounts/new)", () => {
  it("renders the capability picker with one checkbox per non-wildcard verb", () => {
    renderPage();

    expect(
      screen.getByRole("heading", { name: /Mint a service account/ }),
    ).toBeInTheDocument();
    // 4 real caps from the registry (wildcard filtered out).
    expect(
      screen.getByTestId("cap-checkbox-host:manage_users"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("cap-checkbox-host:manage_drivers"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("cap-checkbox-cluster:create"),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("cap-checkbox-bucket:create"),
    ).toBeInTheDocument();
    // The catch-all wildcard does NOT render in the picker.
    expect(screen.queryByTestId("cap-checkbox-*:*")).not.toBeInTheDocument();
  });

  it("filters capabilities via the search box (case-insensitive)", async () => {
    renderPage();

    await userEvent.type(screen.getByTestId("sa-cap-search"), "cluster");
    expect(
      screen.getByTestId("cap-checkbox-cluster:create"),
    ).toBeInTheDocument();
    expect(
      screen.queryByTestId("cap-checkbox-host:manage_users"),
    ).not.toBeInTheDocument();
  });

  it("gates submit until a name and at least one capability are picked", async () => {
    renderPage();

    const submit = screen.getByTestId("sa-submit-button");
    expect(submit).toBeDisabled();

    await userEvent.type(screen.getByTestId("sa-name-input"), "test-cli");
    expect(submit).toBeDisabled();

    await userEvent.click(
      screen.getByTestId("cap-checkbox-host:manage_users"),
    );
    expect(submit).toBeEnabled();
  });

  it("renders the secret-shown-once dialog on a successful mint and supports copy + done", async () => {
    mutateAsyncMock.mockResolvedValue(mintResult);

    // Mock navigator.clipboard so the test asserts the Copy buttons
    // actually call writeText with the right payloads.
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });

    renderPage();

    await userEvent.type(screen.getByTestId("sa-name-input"), "test-cli");
    await userEvent.click(
      screen.getByTestId("cap-checkbox-host:manage_users"),
    );
    await userEvent.click(screen.getByTestId("sa-submit-button"));

    // Dialog renders with the access key + bearer header + aws snippet.
    expect(screen.getByTestId("sa-akid")).toHaveTextContent(
      "BMNT1111222233334444",
    );
    expect(screen.getByTestId("sa-bearer")).toHaveTextContent(
      "Authorization: Bearer BMNT1111222233334444:s3cret_plaintext_value_shown_once",
    );
    expect(screen.getByTestId("sa-aws-snippet")).toHaveTextContent(
      "[test-cli]",
    );

    // Secret starts hidden, reveal toggles to plaintext.
    expect(screen.getByTestId("sa-secret")).not.toHaveTextContent(
      "s3cret_plaintext_value_shown_once",
    );
    await userEvent.click(screen.getByTestId("sa-secret-reveal"));
    expect(screen.getByTestId("sa-secret")).toHaveTextContent(
      "s3cret_plaintext_value_shown_once",
    );

    // Copy buttons fire the clipboard API with the right values.
    await userEvent.click(screen.getByTestId("sa-akid-copy"));
    expect(writeText).toHaveBeenLastCalledWith("BMNT1111222233334444");

    await userEvent.click(screen.getByTestId("sa-secret-copy"));
    expect(writeText).toHaveBeenLastCalledWith(
      "s3cret_plaintext_value_shown_once",
    );

    await userEvent.click(screen.getByTestId("sa-bearer-copy"));
    expect(writeText).toHaveBeenLastCalledWith(
      "Authorization: Bearer BMNT1111222233334444:s3cret_plaintext_value_shown_once",
    );

    // Done is gated until the acknowledgement checkbox is ticked.
    const done = screen.getByTestId("sa-done-button");
    expect(done).toBeDisabled();

    await userEvent.click(screen.getByTestId("sa-acknowledged-checkbox"));
    expect(done).toBeEnabled();

    await userEvent.click(done);
    expect(navigateMock).toHaveBeenCalledWith({
      to: "/admin/service-accounts",
    });
  });

  it("sends the canonical create body shape on submit", async () => {
    mutateAsyncMock.mockResolvedValue(mintResult);

    renderPage();

    await userEvent.type(screen.getByTestId("sa-name-input"), "test-cli");
    await userEvent.click(
      screen.getByTestId("cap-checkbox-host:manage_users"),
    );
    await userEvent.click(screen.getByTestId("sa-submit-button"));

    expect(mutateAsyncMock).toHaveBeenCalledTimes(1);
    const body = mutateAsyncMock.mock.calls[0]![0];
    expect(body).toMatchObject({
      name: "test-cli",
      capabilities: [{ id: "host:manage_users", scope: "host:*" }],
      scopes: [],
      expiresAt: null,
    });
  });

  it("offers expiry presets and switches to a custom date input when picked", async () => {
    renderPage();

    expect(screen.getByTestId("expiry-preset-never")).toBeInTheDocument();
    expect(screen.getByTestId("expiry-preset-1m")).toBeInTheDocument();
    expect(screen.getByTestId("expiry-preset-6m")).toBeInTheDocument();
    expect(screen.getByTestId("expiry-preset-1y")).toBeInTheDocument();
    expect(screen.getByTestId("expiry-preset-custom")).toBeInTheDocument();

    // Custom button reveals the date input.
    await userEvent.click(screen.getByTestId("expiry-preset-custom"));
    expect(screen.getByTestId("sa-custom-expiry-input")).toBeInTheDocument();
  });
});

describe("defaultScopeFor", () => {
  it("returns host:* for host caps", () => {
    expect(defaultScopeFor("host:manage_users")).toBe("host:*");
  });
  it("returns cluster:* for cluster caps", () => {
    expect(defaultScopeFor("cluster:create")).toBe("cluster:*");
  });
  it("returns bucket:{cid}:* for bucket caps", () => {
    expect(defaultScopeFor("bucket:create")).toBe("bucket:{cid}:*");
  });
  it("returns key:{cid}:* for key caps", () => {
    expect(defaultScopeFor("key:create")).toBe("key:{cid}:*");
  });
});
