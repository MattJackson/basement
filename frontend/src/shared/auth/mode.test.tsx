// Tests for the AuthModeProvider state machine (ADR-0003 v1.2.0b).
//
// Two surfaces under test:
//
//   1. The pure computeNextStateOnDowngrade ladder — easy to reason
//      about without React, covers the "ELEVATED → ADMIN → USER on
//      expiry, USER stays" rule.
//   2. The React provider's auto-downgrade tick — a 1Hz interval that
//      mirrors the rule against the live clock. We drive it with
//      vitest fake timers so the test runs in milliseconds, not
//      minutes.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import {
  AuthModeProvider,
  computeNextStateOnDowngrade,
  useAuthMode,
  useSetAuthMode,
} from "./mode";

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrap({
  initial,
  children,
}: {
  initial?: { mode: "user" | "admin" | "elevated"; expiresAt: number };
  children: React.ReactNode;
}) {
  return (
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider initial={initial}>{children}</AuthModeProvider>
    </QueryClientProvider>
  );
}

function ModeProbe() {
  const { mode, expiresAt } = useAuthMode();
  return (
    <span data-testid="probe">
      {mode}|{expiresAt}
    </span>
  );
}

describe("computeNextStateOnDowngrade", () => {
  const now = 1_700_000_000_000;

  it("returns same state when expiresAt is 0 (USER)", () => {
    const s = { mode: "user" as const, expiresAt: 0 };
    expect(computeNextStateOnDowngrade(s, now)).toBe(s);
  });

  it("returns same state when not yet expired", () => {
    const s = { mode: "admin" as const, expiresAt: now + 10_000 };
    expect(computeNextStateOnDowngrade(s, now)).toBe(s);
  });

  it("downgrades ELEVATED to ADMIN at expiry", () => {
    const s = { mode: "elevated" as const, expiresAt: now - 1 };
    expect(computeNextStateOnDowngrade(s, now)).toEqual({
      mode: "admin",
      expiresAt: 0,
    });
  });

  it("downgrades ADMIN to USER at expiry", () => {
    const s = { mode: "admin" as const, expiresAt: now - 1 };
    expect(computeNextStateOnDowngrade(s, now)).toEqual({
      mode: "user",
      expiresAt: 0,
    });
  });

  it("never downgrades USER (already the floor)", () => {
    const s = { mode: "user" as const, expiresAt: now - 1 };
    expect(computeNextStateOnDowngrade(s, now)).toBe(s);
  });
});

describe("AuthModeProvider", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("seeds context from `initial` prop", () => {
    render(
      <Wrap initial={{ mode: "admin", expiresAt: 1234 }}>
        <ModeProbe />
      </Wrap>,
    );
    expect(screen.getByTestId("probe").textContent).toBe("admin|1234");
  });

  it("defaults to USER / 0 when no initial is provided", () => {
    render(
      <Wrap>
        <ModeProbe />
      </Wrap>,
    );
    expect(screen.getByTestId("probe").textContent).toBe("user|0");
  });

  it("setAuthMode replaces the state", () => {
    function Setter() {
      const set = useSetAuthMode();
      return (
        <button
          data-testid="set"
          onClick={() => set({ mode: "elevated", expiresAt: 5_555 })}
        >
          set
        </button>
      );
    }
    render(
      <Wrap>
        <ModeProbe />
        <Setter />
      </Wrap>,
    );
    expect(screen.getByTestId("probe").textContent).toBe("user|0");
    act(() => {
      screen.getByTestId("set").click();
    });
    expect(screen.getByTestId("probe").textContent).toBe("elevated|5555");
  });

  it("auto-downgrades ELEVATED → ADMIN past expiresAt on the tick", () => {
    // Seed at ELEVATED with expiry 500ms in the future.
    const now = Date.now();
    render(
      <Wrap initial={{ mode: "elevated", expiresAt: now + 500 }}>
        <ModeProbe />
      </Wrap>,
    );
    expect(screen.getByTestId("probe").textContent).toBe(
      `elevated|${now + 500}`,
    );

    // Advance past the expiry + past the next 1Hz tick.
    act(() => {
      vi.advanceTimersByTime(1500);
    });

    expect(screen.getByTestId("probe").textContent).toBe("admin|0");
  });

  it("auto-downgrades ADMIN → USER past expiresAt on the tick", () => {
    const now = Date.now();
    render(
      <Wrap initial={{ mode: "admin", expiresAt: now + 500 }}>
        <ModeProbe />
      </Wrap>,
    );

    act(() => {
      vi.advanceTimersByTime(1500);
    });

    expect(screen.getByTestId("probe").textContent).toBe("user|0");
  });

  it("does NOT downgrade while still inside the window", () => {
    const now = Date.now();
    render(
      <Wrap initial={{ mode: "admin", expiresAt: now + 60_000 }}>
        <ModeProbe />
      </Wrap>,
    );

    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(screen.getByTestId("probe").textContent).toBe(
      `admin|${now + 60_000}`,
    );
  });
});
