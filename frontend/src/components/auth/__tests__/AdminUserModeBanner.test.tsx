// Tests for AdminUserModeBanner (ADR-0003 v1.7.0a.1 amendment).
//
// Behaviour codified here:
//   - Renders when mode === user AND on /admin/*.
//   - Renders nothing when mode === admin.
//   - Renders nothing on non-admin routes (/files/*).
//   - "Elevate to admin" button calls promptForElevation with target=admin.
//   - "Drop to /files" button navigates to /files.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { AdminUserModeBanner } from "../AdminUserModeBanner";
import {
  AuthModeProvider,
  type AuthMode,
} from "@/shared/auth/mode";

// useLocation: swap pathname per test; useNavigate: spy so we can
// assert the "Drop to /files" click routes correctly.
let mockedPath = "/admin/clusters";
const navigateSpy = vi.fn((_opts: { to: string }) => Promise.resolve());
vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: mockedPath }),
  useNavigate: () => navigateSpy,
}));

const promptSpy = vi.fn((_target: AuthMode, _reason?: string) =>
  Promise.resolve(),
);
vi.mock("@/shared/auth/elevation", () => ({
  useElevationPrompt: () => promptSpy,
}));

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Harness({
  initial,
}: {
  initial?: { mode: AuthMode; expiresAt: number };
}) {
  return (
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider initial={initial}>
        <AdminUserModeBanner />
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("AdminUserModeBanner", () => {
  beforeEach(() => {
    mockedPath = "/admin/clusters";
    promptSpy.mockClear();
    navigateSpy.mockClear();
  });

  it("renders when mode === user AND on /admin/*", () => {
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    expect(screen.getByTestId("admin-user-mode-banner")).toBeInTheDocument();
  });

  it("renders nothing when mode === admin", () => {
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 60000 }} />,
    );
    expect(
      screen.queryByTestId("admin-user-mode-banner"),
    ).not.toBeInTheDocument();
  });

  it("renders nothing when mode === elevated", () => {
    render(
      <Harness initial={{ mode: "elevated", expiresAt: Date.now() + 60000 }} />,
    );
    expect(
      screen.queryByTestId("admin-user-mode-banner"),
    ).not.toBeInTheDocument();
  });

  it("renders nothing on /files/* even when mode === user", () => {
    mockedPath = "/files";
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    expect(
      screen.queryByTestId("admin-user-mode-banner"),
    ).not.toBeInTheDocument();
  });

  it("renders nothing on /files/buckets/foo even when mode === user", () => {
    mockedPath = "/files/buckets/foo";
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    expect(
      screen.queryByTestId("admin-user-mode-banner"),
    ).not.toBeInTheDocument();
  });

  it("Elevate button calls promptForElevation with target=admin", () => {
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    act(() => {
      screen.getByTestId("admin-user-mode-elevate").click();
    });
    expect(promptSpy).toHaveBeenCalledTimes(1);
    const call = promptSpy.mock.calls[0];
    expect(call?.[0]).toBe("admin");
  });

  it("Drop button navigates to /files", () => {
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    act(() => {
      screen.getByTestId("admin-user-mode-drop").click();
    });
    expect(navigateSpy).toHaveBeenCalledTimes(1);
    const call = navigateSpy.mock.calls[0];
    expect(call?.[0]).toEqual({ to: "/files" });
  });
});
