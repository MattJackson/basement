// Tests for the PersonaPill warning ramps (ADR-0003 v1.3.0a.4 amendment).
//
// The amendment widened the warning windows from "amber at <30s" to
// "amber at <2 min, red+flashing at <30s". This test pins each window
// to its visual state so a future refactor doesn't drift the
// thresholds (the operator's lead-time guarantee is in code, not in
// vibes).

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { PersonaPill, formatRemaining } from "./PersonaPill";
import { AuthModeProvider } from "@/shared/auth/mode";

// Sonner's toast is a side-effect; mock to keep DOM clean.
vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
    warning: vi.fn(),
    info: vi.fn(),
  },
}));

// Elevation prompt is what the "Stay admin" toast action calls; mock
// to a no-op so we don't need an ElevationProvider in the harness.
vi.mock("@/shared/auth/elevation", () => ({
  useElevationPrompt: () => vi.fn(() => Promise.resolve()),
}));

function newClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function Harness({
  initial,
}: {
  initial: { mode: "user" | "admin" | "elevated"; expiresAt: number };
}) {
  return (
    <QueryClientProvider client={newClient()}>
      <AuthModeProvider initial={initial}>
        <PersonaPill />
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("PersonaPill warning windows", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("renders neutral USER pill with no countdown / no warn", () => {
    render(<Harness initial={{ mode: "user", expiresAt: 0 }} />);
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-mode")).toBe("user");
    expect(screen.queryByTestId("persona-countdown")).not.toBeInTheDocument();
  });

  it("ADMIN with >2min remaining is neutral (no warn ramp yet)", () => {
    const expiresAt = Date.now() + 5 * 60 * 1000; // 5 min
    render(<Harness initial={{ mode: "admin", expiresAt }} />);
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-mode")).toBe("admin");
    expect(pill.getAttribute("data-warn")).toBe("none");
  });

  it("ADMIN with <2min remaining goes amber (heads-up window)", () => {
    const expiresAt = Date.now() + 90 * 1000; // 90s
    render(<Harness initial={{ mode: "admin", expiresAt }} />);
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-warn")).toBe("amber");
  });

  it("ADMIN with <30s remaining goes red (final warning)", () => {
    const expiresAt = Date.now() + 20 * 1000; // 20s
    render(<Harness initial={{ mode: "admin", expiresAt }} />);
    const pill = screen.getByTestId("persona-pill");
    expect(pill.getAttribute("data-warn")).toBe("red");
  });
});

describe("formatRemaining", () => {
  it("renders mm:ss under an hour", () => {
    expect(formatRemaining(0)).toBe("0:00");
    expect(formatRemaining(45_000)).toBe("0:45");
    expect(formatRemaining(60_000)).toBe("1:00");
    expect(formatRemaining(125_000)).toBe("2:05");
  });

  it("renders h:mm:ss when ≥1 hour (operator can pick long TTLs now)", () => {
    expect(formatRemaining(3600_000)).toBe("1:00:00");
    expect(formatRemaining(2 * 3600_000 + 5 * 60 * 1000 + 7 * 1000)).toBe(
      "2:05:07",
    );
  });

  it("clamps negative inputs to 0:00", () => {
    expect(formatRemaining(-100)).toBe("0:00");
  });
});
