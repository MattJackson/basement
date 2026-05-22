// Tests for the ElevationExpiredBanner (ADR-0003 v1.3.0a.4 amendment).
//
// Behaviour codified here:
//   - Banner hidden on first render (mode=user, never been admin).
//   - Banner hidden on /admin/* if mode is admin (no expiry yet).
//   - Banner shown on /admin/* after admin → user transition.
//   - Banner hidden again when mode flips back to admin (re-elevation).
//   - Banner hidden when navigating away from /admin/* (the operator
//     left the admin context).
//   - Banner is a no-op on non-admin routes regardless of mode.
//   - Re-elevate button calls the elevation prompt with target=admin.
//
// We drive the auth mode via the real AuthModeProvider so the rising +
// falling edge transitions tested here mirror what production sees.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ElevationExpiredBanner } from "./ElevationExpiredBanner";
import {
  AuthModeProvider,
  useSetAuthMode,
  type AuthMode,
} from "@/shared/auth/mode";

// useLocation is the router hook we read for pathname; mock it so we
// can swap the path without a full router setup.
let mockedPath = "/admin/clusters";
vi.mock("@tanstack/react-router", () => ({
  useLocation: () => ({ pathname: mockedPath }),
}));

// promptForElevation: spy-able stub the banner calls when the operator
// clicks "Re-elevate". We mock useElevationPrompt rather than wiring an
// ElevationProvider so the test stays focused on the banner's logic.
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
  children,
}: {
  initial?: { mode: AuthMode; expiresAt: number };
  children?: React.ReactNode;
}) {
  return (
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider initial={initial}>
        <ElevationExpiredBanner />
        {children}
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

/** ModeFlipper exposes a button that flips the auth mode for the test. */
function ModeFlipper({
  to,
  testId,
}: {
  to: { mode: AuthMode; expiresAt: number };
  testId: string;
}) {
  const set = useSetAuthMode();
  return (
    <button data-testid={testId} onClick={() => set(to)}>
      flip
    </button>
  );
}

describe("ElevationExpiredBanner", () => {
  beforeEach(() => {
    mockedPath = "/admin/clusters";
    promptSpy.mockClear();
  });

  it("does NOT render on fresh USER state (never been admin)", () => {
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    expect(
      screen.queryByTestId("elevation-expired-banner"),
    ).not.toBeInTheDocument();
  });

  it("does NOT render while in ADMIN", () => {
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 60000 }} />,
    );
    expect(
      screen.queryByTestId("elevation-expired-banner"),
    ).not.toBeInTheDocument();
  });

  it("renders on /admin/* after ADMIN → USER transition", () => {
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 60000 }}>
        <ModeFlipper to={{ mode: "user", expiresAt: 0 }} testId="to-user" />
      </Harness>,
    );
    expect(
      screen.queryByTestId("elevation-expired-banner"),
    ).not.toBeInTheDocument();

    act(() => {
      screen.getByTestId("to-user").click();
    });

    expect(
      screen.getByTestId("elevation-expired-banner"),
    ).toBeInTheDocument();
  });

  it("hides when mode flips back to admin (re-elevation)", () => {
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 60000 }}>
        <ModeFlipper to={{ mode: "user", expiresAt: 0 }} testId="to-user" />
        <ModeFlipper
          to={{ mode: "admin", expiresAt: Date.now() + 120000 }}
          testId="to-admin"
        />
      </Harness>,
    );
    act(() => {
      screen.getByTestId("to-user").click();
    });
    expect(
      screen.getByTestId("elevation-expired-banner"),
    ).toBeInTheDocument();

    act(() => {
      screen.getByTestId("to-admin").click();
    });
    expect(
      screen.queryByTestId("elevation-expired-banner"),
    ).not.toBeInTheDocument();
  });

  it("does not render on non-admin routes", () => {
    mockedPath = "/files";
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 60000 }}>
        <ModeFlipper to={{ mode: "user", expiresAt: 0 }} testId="to-user" />
      </Harness>,
    );
    act(() => {
      screen.getByTestId("to-user").click();
    });
    expect(
      screen.queryByTestId("elevation-expired-banner"),
    ).not.toBeInTheDocument();
  });

  it("Re-elevate button calls promptForElevation with target=admin", () => {
    render(
      <Harness initial={{ mode: "admin", expiresAt: Date.now() + 60000 }}>
        <ModeFlipper to={{ mode: "user", expiresAt: 0 }} testId="to-user" />
      </Harness>,
    );
    act(() => {
      screen.getByTestId("to-user").click();
    });

    act(() => {
      screen.getByTestId("elevation-expired-reelevate").click();
    });

    expect(promptSpy).toHaveBeenCalledTimes(1);
    const call = promptSpy.mock.calls[0];
    expect(call?.[0]).toBe("admin");
  });
});
