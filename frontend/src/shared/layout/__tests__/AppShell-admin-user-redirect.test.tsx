// v1.9.0e.2 — tight mode/view coupling tests for AppShell.
//
// USER mode visiting /admin/* gets a silent redirect to /files.
// The (deleted) AdminEntryElevationGuard used to fire the elevation
// modal instead; under the new model the operator must explicitly opt
// in to admin via UserMenu, and URL-bar navigation to /admin/* in user
// mode bounces back to /files with no prompt.
//
// ADMIN mode visiting /admin/* renders normally (no redirect, no
// banner). ADMIN mode visiting /files/* is also allowed — the admin
// can dip into the user view; only USER → /admin is blocked.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import {
  AuthModeProvider,
  type AuthMode,
} from "@/shared/auth/mode";

// useLocation: swap pathname per test; useNavigate: spy so we can
// assert the silent redirect target. Also stub Link so we don't need
// a real route tree to render the header nav.
let mockedPath = "/admin/clusters";
const navigateSpy = vi.fn((_opts: { to: string; replace?: boolean }) =>
  Promise.resolve(),
);
vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: mockedPath }),
  useNavigate: () => navigateSpy,
  Link: ({ children, ...rest }: { children: React.ReactNode } & Record<string, unknown>) => (
    <a {...rest}>{children}</a>
  ),
  Outlet: () => null,
}));

// useUser drives the "isUIAdmin" branch of the nav; ship a plain admin
// payload so the nav renders the full set (irrelevant to the redirect
// assertion but keeps the render path consistent).
vi.mock("@/shared/auth/useUser", () => ({
  useUser: () => ({
    data: {
      username: "matthew",
      role: "admin" as const,
      uiAdmin: true,
      oidcUser: false,
    },
    isLoading: false,
    isError: false,
  }),
}));

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useVersion: () => ({
      data: { version: "v1.9.0e.2", commit: "abc1234", builtAt: "" },
      isLoading: false,
      error: null,
    }),
    // v1.11.0a — AppShell now consults the onboarding state to decide
    // whether to auto-route to /admin/first-run. Default to "no
    // onboarding needed" so the existing redirect tests don't fight a
    // second navigation.
    useOnboardingState: () => ({ data: { needsOnboarding: false, completed: true } }),
    useOrgCapabilities: () => ({ data: {} }),
  };
});

// Elevation prompt is touched indirectly via UserMenu; mock to a no-op
// so we don't need an ElevationProvider in the harness.
vi.mock("@/shared/auth/elevation", () => ({
  useElevationPrompt: () => vi.fn(() => Promise.resolve()),
}));

beforeEach(() => {
  Object.defineProperty(globalThis.window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
});

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

async function renderShell(opts: {
  path: string;
  initial: { mode: AuthMode; expiresAt: number };
}) {
  mockedPath = opts.path;
  const { AppShell } = await import("@/shared/layout/AppShell");
  return render(
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider initial={opts.initial}>
        <AppShell>
          <div data-testid="page-body" />
        </AppShell>
      </AuthModeProvider>
    </QueryClientProvider>,
  );
}

describe("AppShell — USER mode on /admin/* redirects to /files", () => {
  beforeEach(() => {
    navigateSpy.mockClear();
  });

  it("USER on /admin/clusters → navigate /files (replace)", async () => {
    await renderShell({
      path: "/admin/clusters",
      initial: { mode: "user", expiresAt: 0 },
    });
    expect(navigateSpy).toHaveBeenCalledWith({ to: "/files", replace: true });
  });

  // v1.11.0.15: /admin/keys route removed (keys are per-cluster only);
  // dropped the USER-on-/admin/keys redirect assertion. The redirect
  // middleware itself is covered by the /admin/clusters case above —
  // it matches any /admin/* path the user lands on.

  it("ADMIN on /admin/clusters → no redirect", async () => {
    await renderShell({
      path: "/admin/clusters",
      initial: { mode: "admin", expiresAt: Date.now() + 60000 },
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(navigateSpy).not.toHaveBeenCalled();
  });

  it("ELEVATED on /admin/clusters → no redirect", async () => {
    await renderShell({
      path: "/admin/clusters",
      initial: { mode: "elevated", expiresAt: Date.now() + 60000 },
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(navigateSpy).not.toHaveBeenCalled();
  });

  // The /admin/login route is the pre-auth page and renders bare
  // (outside AppShell), but defensively the redirect effect must not
  // bounce someone deep-linking to it.
  it("USER on /admin/login → no redirect", async () => {
    await renderShell({
      path: "/admin/login",
      initial: { mode: "user", expiresAt: 0 },
    });
    await act(async () => {
      await Promise.resolve();
    });
    expect(navigateSpy).not.toHaveBeenCalled();
  });
});

// v1.10.0e — hydration-race hardening (same shape as the v1.7.0a.3/a.4
// AdminEntryElevationGuard hotfix). Without these guards, every full-
// page navigation to /admin/* would bounce to /files even when the
// cookie reports admin — because the AuthModeHydrator's setMode runs in
// a SUBSEQUENT render so within the first render where user data
// arrives, the provider's mode is still the conservative USER default.
describe("AppShell — hydration-race guards on /admin/* redirect (v1.10.0e)", () => {
  beforeEach(() => {
    navigateSpy.mockClear();
    vi.resetModules();
  });

  it("does not redirect while useUser() is loading", async () => {
    mockedPath = "/admin/clusters";
    vi.doMock("@/shared/auth/useUser", () => ({
      useUser: () => ({
        data: undefined,
        isLoading: true,
        isError: false,
      }),
    }));
    const { AppShell } = await import("@/shared/layout/AppShell");
    render(
      <QueryClientProvider client={newClient()}>
        <AuthModeProvider initial={{ mode: "user", expiresAt: 0 }}>
          <AppShell>
            <div data-testid="page-body" />
          </AppShell>
        </AuthModeProvider>
      </QueryClientProvider>,
    );
    expect(navigateSpy).not.toHaveBeenCalled();
  });

  it("does not redirect when /auth/me already reports admin even though provider mode is still user", async () => {
    mockedPath = "/admin/clusters";
    vi.doMock("@/shared/auth/useUser", () => ({
      useUser: () => ({
        data: {
          username: "matthew",
          role: "admin" as const,
          uiAdmin: true,
          oidcUser: false,
          mode: "admin" as const,
          modeExpiresAt: Math.floor(Date.now() / 1000) + 600,
        },
        isLoading: false,
        isError: false,
      }),
    }));
    const { AppShell } = await import("@/shared/layout/AppShell");
    render(
      <QueryClientProvider client={newClient()}>
        <AuthModeProvider initial={{ mode: "user", expiresAt: 0 }}>
          <AppShell>
            <div data-testid="page-body" />
          </AppShell>
        </AuthModeProvider>
      </QueryClientProvider>,
    );
    expect(navigateSpy).not.toHaveBeenCalled();
  });
});
