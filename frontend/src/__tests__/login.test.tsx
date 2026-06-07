import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { LoginForm } from "@/shared/auth/LoginForm";

// Mock the TanStack router hooks the LoginForm calls. The component is
// rendered without a real router context in this test, so we satisfy the
// imports with controllable stubs.
let mockSearch: { next?: string; error?: string } = {};
const navigateSpy = vi.fn();

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
  useSearch: () => mockSearch,
}));

// Stub the theme toggle — it touches document.cookie + matchMedia paths
// that aren't relevant to login-page SSO behavior.
vi.mock("@/shared/theme/ThemeToggle", () => ({
  ThemeToggle: () => null,
}));

// openapi-fetch binds globalThis.fetch at createClient() time (module
// load), so a later vi.spyOn(window,"fetch") can't intercept
// client.POST. Mock the client directly with a controllable POST result
// so the login submit path is deterministic. vi.hoisted lets the
// hoisted vi.mock factory reference these without TDZ errors.
const loginMock = vi.hoisted(() => {
  const state = {
    result: { response: { status: 200 } } as {
      error?: { error?: { message?: string } };
      response: { status: number };
    },
  };
  const post = vi.fn(async () => state.result);
  return { state, post };
});
vi.mock("@/shared/api/client", () => ({
  client: { POST: loginMock.post },
  API_BASE: "/api/v1",
}));

// Stub the version query — it calls fetch("/api/v1/version") which we'd
// otherwise have to mock per-test. Merge with partial mock below.
vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useVersion: () => ({ data: undefined }),
    useOrgCapabilities: () => ({ data: { signupMode: "invite" } as Record<string, unknown> }),
    isSignupEnabled: (mode?: string) => mode === "open" || mode === "invite",
    useActiveSkin: () => ({ data: null, isLoading: false }),
  };
});

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
      } else if (url.endsWith("/auth/me")) {
        return new Response(
          JSON.stringify({ username: "admin", role: "user", uiAdmin: false, oidcUser: false }),
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
      } else if (url.endsWith("/auth/me")) {
        return new Response(
          JSON.stringify({ username: "admin", role: "user", uiAdmin: false, oidcUser: false }),
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
      } else if (url.endsWith("/auth/me")) {
        return new Response(
          JSON.stringify({ username: "admin", role: "user", uiAdmin: false, oidcUser: false }),
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

 });

describe("LoginForm — open-redirect guard on ?next=", () => {
  beforeEach(() => {
    mockSearch = {};
    navigateSpy.mockReset();
    loginMock.post.mockClear();
    loginMock.state.result = { response: { status: 200 } };
    // useOIDCAvailable uses raw fetch (not the typed client), so it still
    // needs a fetch stub. oidc not configured → no SSO button.
    vi.spyOn(window, "fetch").mockImplementation(async () =>
      new Response(JSON.stringify({ password: true, oidc: { configured: false } }), {
        status: 200,
      }),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  async function submitLogin() {
    const u = screen.getByLabelText(/username/i) as HTMLInputElement;
    const p = screen.getByLabelText(/password/i) as HTMLInputElement;
    fireEvent.input(u, { target: { value: "admin" } });
    fireEvent.input(p, { target: { value: "hunter2" } });
    fireEvent.submit(screen.getByRole("button", { name: /^sign in$/i }).closest("form")!);
  }

  it("navigates to a safe in-app next path on success", async () => {
    mockSearch = { next: "/admin/clusters" };
    renderLogin(newClient());
    await submitLogin();
    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith({ to: "/admin/clusters" });
    });
  });

  it("falls back to /files for a protocol-relative open-redirect (//evil.com)", async () => {
    mockSearch = { next: "//evil.com" };
    renderLogin(newClient());
    await submitLogin();
    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith({ to: "/files" });
    });
    expect(navigateSpy).not.toHaveBeenCalledWith({ to: "//evil.com" });
  });

  it("falls back to /files for the backslash-variant open-redirect (/\\evil.com)", async () => {
    mockSearch = { next: "/\\evil.com" };
    renderLogin(newClient());
    await submitLogin();
    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith({ to: "/files" });
    });
  });
});

// NOTE: Signup link visibility tests require more sophisticated mocking.
// They should verify that the sign-up link is shown when signupMode is 'open' or 'invite',
// and hidden when it's 'closed'. This can be added once proper React Query mocking
// is established for these tests.

