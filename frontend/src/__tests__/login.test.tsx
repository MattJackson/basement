import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { LoginForm } from "@/shared/auth/LoginForm";

// Mock the TanStack router hooks the LoginForm calls. The component is
// rendered without a real router context in this test, so we satisfy the
// imports with controllable stubs.
let mockSearch: { next?: string; error?: string } = {};

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  useSearch: () => mockSearch,
}));

// Stub the theme toggle — it touches document.cookie + matchMedia paths
// that aren't relevant to login-page SSO behavior.
vi.mock("@/shared/theme/ThemeToggle", () => ({
  ThemeToggle: () => null,
}));

// Stub the version query — it calls fetch("/api/v1/version") which we'd
// otherwise have to mock per-test.
vi.mock("@/shared/api/queries", () => ({
  useVersion: () => ({ data: undefined }),
}));

function newClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function renderLogin(client: QueryClient) {
  return render(
    <QueryClientProvider client={client}>
      <LoginForm />
    </QueryClientProvider>,
  );
}

// hrefSetter captures window.location.href assignments without actually
// navigating. jsdom doesn't implement navigation, so we install a stub
// that records the assigned URL.
function installLocationHrefSpy(): { calls: string[] } {
  const calls: string[] = [];
  const original = Object.getOwnPropertyDescriptor(window, "location");
  // jsdom's location has a setter on href; replace the whole object.
  // This is the pattern Vitest uses in its own docs.
  delete (window as unknown as { location?: Location }).location;
  (window as unknown as { location: { href: string } }).location = {
    href: "",
    get [Symbol.toPrimitive]() {
      return () => "";
    },
  } as unknown as Location;
  Object.defineProperty(window.location, "href", {
    set(value: string) {
      calls.push(value);
    },
    get() {
      return "";
    },
    configurable: true,
  });
  // Restore handle — caller can put it back after the test.
  (installLocationHrefSpy as unknown as { _restore?: () => void })._restore =
    () => {
      if (original) {
        Object.defineProperty(window, "location", original);
      }
    };
  return { calls };
}

describe("LoginForm — OIDC SSO integration", () => {
  beforeEach(() => {
    mockSearch = {};
  });

  afterEach(() => {
    vi.restoreAllMocks();
    const restore = (installLocationHrefSpy as unknown as { _restore?: () => void })
      ._restore;
    if (restore) restore();
  });

  it("hides the SSO button when /auth/methods reports oidc not configured", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/methods")) {
        return new Response(
          JSON.stringify({ password: true, oidc: { configured: false } }),
          { status: 200 },
        );
      }
      return new Response("{}", { status: 200 });
    });

    renderLogin(newClient());

    // Wait for the useQuery probe to resolve.
    await waitFor(() => {
      expect(screen.queryByTestId("sso-button")).not.toBeInTheDocument();
    });

    // Username/password form is still there.
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
  });

  it("shows the SSO button when /auth/methods reports oidc configured", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/methods")) {
        return new Response(
          JSON.stringify({ password: true, oidc: { configured: true } }),
          { status: 200 },
        );
      }
      return new Response("{}", { status: 200 });
    });

    renderLogin(newClient());

    const button = await screen.findByTestId("sso-button");
    expect(button).toBeInTheDocument();
    expect(button).toHaveTextContent(/sign in with sso/i);
  });

  it("clicking the SSO button navigates the browser to /api/v1/auth/oidc/start", async () => {
    vi.spyOn(window, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/methods")) {
        return new Response(
          JSON.stringify({ password: true, oidc: { configured: true } }),
          { status: 200 },
        );
      }
      return new Response("{}", { status: 200 });
    });

    const spy = installLocationHrefSpy();
    renderLogin(newClient());

    const button = await screen.findByTestId("sso-button");
    fireEvent.click(button);

    expect(spy.calls).toEqual(["/api/v1/auth/oidc/start"]);
  });

  it("renders the OIDC error banner when ?error=<code> is present", async () => {
    mockSearch = { error: "OIDC_STATE_MISMATCH" };

    vi.spyOn(window, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/methods")) {
        return new Response(
          JSON.stringify({ password: true, oidc: { configured: false } }),
          { status: 200 },
        );
      }
      return new Response("{}", { status: 200 });
    });

    renderLogin(newClient());

    const banner = await screen.findByRole("alert");
    expect(banner).toHaveTextContent(/could not be verified/i);
  });

  it("renders an OIDC error banner with a generic message for unknown codes", async () => {
    mockSearch = { error: "SOMETHING_NEW" };

    vi.spyOn(window, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.endsWith("/auth/methods")) {
        return new Response(
          JSON.stringify({ password: true, oidc: { configured: false } }),
          { status: 200 },
        );
      }
      return new Response("{}", { status: 200 });
    });

    renderLogin(newClient());

    const banner = await screen.findByRole("alert");
    expect(banner).toHaveTextContent(/SOMETHING_NEW/);
  });
});
