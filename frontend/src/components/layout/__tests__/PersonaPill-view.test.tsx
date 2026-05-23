// v1.9.0e.1 — PersonaPill must distinguish admin-mode-on-admin-page
// from admin-mode-on-user-page so the operator can tell at a glance
// whether their current URL matches the authority they're carrying.
//
// Behaviour codified here:
//   mode=admin + URL=/admin/clusters → solid amber pill, label "ADMIN"
//   mode=admin + URL=/files          → outlined amber pill,
//                                      label "ADMIN · user view"
//   mode=user  + URL=/files          → neutral USER pill
//   mode=user  + URL=/admin          → neutral USER pill (the banner
//                                      handles the mismatch; pill stays
//                                      plain so the operator doesn't
//                                      see a "fake admin" indicator)
//
// We pin the visual via the data-view attribute on the pill element so
// CSS-class refactors don't drift the contract.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { PersonaPill } from "../PersonaPill";
import { AuthModeProvider, type AuthMode } from "@/shared/auth/mode";

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}));

// "Stay admin" toast action is a no-op for these tests.
vi.mock("@/shared/auth/elevation", () => ({
  useElevationPrompt: () => vi.fn(() => Promise.resolve()),
}));

// Mutable router pathname controlled per test.
let mockedPath = "/admin/clusters";
vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: mockedPath }),
}));

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Harness({
  initial,
}: {
  initial: { mode: AuthMode; expiresAt: number };
}) {
  return (
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider initial={initial}>
        <PersonaPill />
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("PersonaPill — mode vs. view (v1.9.0e.1)", () => {
  beforeEach(() => {
    mockedPath = "/admin/clusters";
  });

  it("mode=admin on /admin/clusters → admin pill, view=admin (solid)", () => {
    mockedPath = "/admin/clusters";
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 5 * 60 * 1000 }} />,
    );
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-mode")).toBe("admin");
    expect(pill.getAttribute("data-view")).toBe("admin");
    expect(pill.textContent).toBe("ADMIN");
    // Solid variant carries no title tooltip.
    expect(pill.getAttribute("title")).toBeNull();
  });

  it("mode=admin on /files → admin pill, view=user (outlined + suffix)", () => {
    mockedPath = "/files";
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 5 * 60 * 1000 }} />,
    );
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-mode")).toBe("admin");
    expect(pill.getAttribute("data-view")).toBe("user");
    // Label calls out the mismatch.
    expect(pill.textContent).toBe("ADMIN · user view");
    // Tooltip explains the state for the operator.
    expect(pill.getAttribute("title")).toMatch(/admin authority/i);
    // Outlined variant uses a border, not a bg fill — pin the class so
    // a future refactor can't silently swap it back to solid.
    expect(pill.className).toMatch(/border/);
    // Countdown still renders — TTL is URL-agnostic.
    expect(screen.getByTestId("persona-countdown")).toBeInTheDocument();
  });

  it("mode=admin on /files/buckets/foo → admin pill, view=user", () => {
    mockedPath = "/files/buckets/foo";
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 5 * 60 * 1000 }} />,
    );
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-view")).toBe("user");
  });

  it("mode=elevated on /files → admin pill, view=user (alias)", () => {
    mockedPath = "/files";
    render(
      <Harness initial={{ mode: "elevated", expiresAt: Date.now() + 5 * 60 * 1000 }} />,
    );
    const pill = screen.getByTestId("persona-pill");
    // Elevated renders under the admin label per the v1.3.0a.4 collapse.
    expect(pill.getAttribute("data-mode")).toBe("admin");
    expect(pill.getAttribute("data-view")).toBe("user");
  });

  it("mode=user on /files → neutral USER pill, view=user", () => {
    mockedPath = "/files";
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-mode")).toBe("user");
    expect(pill.getAttribute("data-view")).toBe("user");
    expect(pill.textContent).toBe("USER");
  });

  it("mode=user on /admin → neutral USER pill (banner handles the mismatch)", () => {
    mockedPath = "/admin/clusters";
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-mode")).toBe("user");
    // No "admin view" annotation in USER mode — we don't claim authority
    // the operator doesn't have. AdminUserModeBanner is what surfaces
    // the "you're on /admin but not in admin mode" prompt.
    expect(pill.textContent).toBe("USER");
  });
});
