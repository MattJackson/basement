// v1.9.0e.2 — logo href tracks auth mode under the tight mode/view
// coupling.
//
// USER mode → /files (the user landing).
// ADMIN/ELEVATED mode → /admin (the admin landing).
//
// Same rule applies in AppShell, but this test is scoped to UserShell
// because that's the place a mode mismatch is observable: an ADMIN
// dipping into the user view (allowed) should be able to click the
// logo and land back in admin without a second click.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  AuthModeProvider,
  type AuthMode,
} from "@/shared/auth/mode";
import { ElevationProvider } from "@/shared/auth/elevation";

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useVersion: vi.fn(() => ({
      data: {
        version: "v1.9.0e.2",
        commit: "def456ghi",
        builtAt: new Date().toISOString(),
      },
      isLoading: false,
      error: null,
    })),
    useActiveSkin: () => ({ data: null, isLoading: false }),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
    Outlet: () => null,
    useNavigate: vi.fn(() => vi.fn()),
    useSearch: vi.fn(() => ({})),
    useRouter: vi.fn(() => ({ isServer: false })),
  };
});

vi.mock("@/shared/auth/useUser", () => ({
  useUser: () => ({
    data: { username: "matthew", role: "admin", uiAdmin: true, oidcUser: false },
    isLoading: false,
    error: null,
  }),
}));

beforeEach(() => {
  Object.defineProperty(globalThis.window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query: string) => ({
      matches: Boolean(query === "(prefers-color-scheme: dark)"),
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

function Wrapper({
  children,
  initialMode,
}: {
  children: React.ReactNode;
  initialMode: { mode: AuthMode; expiresAt: number };
}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={queryClient}>
      <AuthModeProvider initial={initialMode}>
        <ElevationProvider>{children}</ElevationProvider>
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("UserShell logo href (v1.9.0e.2)", () => {
  it("USER mode → logo href=/files", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(
      <UserShell>
        <div data-testid="child">x</div>
      </UserShell>,
      {
        wrapper: ({ children }) => (
          <Wrapper initialMode={{ mode: "user", expiresAt: 0 }}>
            {children}
          </Wrapper>
        ),
      },
    );

    const logo = screen.getByLabelText("Basement — home");
    expect(logo).toHaveAttribute("href", "/files");
  });

  it("ADMIN mode → logo href=/admin", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(
      <UserShell>
        <div data-testid="child">x</div>
      </UserShell>,
      {
        wrapper: ({ children }) => (
          <Wrapper
            initialMode={{ mode: "admin", expiresAt: Date.now() + 60000 }}
          >
            {children}
          </Wrapper>
        ),
      },
    );

    const logo = screen.getByLabelText("Basement — home");
    expect(logo).toHaveAttribute("href", "/admin");
  });

  it("ELEVATED mode → logo href=/admin", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(
      <UserShell>
        <div data-testid="child">x</div>
      </UserShell>,
      {
        wrapper: ({ children }) => (
          <Wrapper
            initialMode={{ mode: "elevated", expiresAt: Date.now() + 60000 }}
          >
            {children}
          </Wrapper>
        ),
      },
    );

    const logo = screen.getByLabelText("Basement — home");
    expect(logo).toHaveAttribute("href", "/admin");
  });
});
