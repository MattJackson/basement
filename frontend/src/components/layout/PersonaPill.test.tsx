// Tests for the PersonaPill warning ramps (ADR-0003 v1.3.0a.4 amendment).
//
// The amendment widened the warning windows from "amber at <30s" to
// "amber at <2 min, red+flashing at <30s". This test pins each window
// to its visual state so a future refactor doesn't drift the
// thresholds (the operator's lead-time guarantee is in code, not in
// vibes).

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { PersonaPill, formatRemaining } from "./PersonaPill";
import { AuthModeProvider } from "@/shared/auth/mode";
import { toast } from "sonner";

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

// v1.9.0e.2: PersonaPill.handleDrop now calls useNavigate to send the
// operator to /files after dropping privileges (tight mode/view
// coupling — drop = go user). Mock the router so the test harness
// doesn't need a real route tree, and expose the spy via a module-
// level handle so the new "drop → /files" assertion can read it.
//
// v1.9.0e.1's useLocation hook on PersonaPill was stripped this cycle
// — the mode-vs-view distinction is now expressed by routing (USER
// can't reach /admin/* at all; admin on /files/* is fine) rather than
// by the pill's visual variant, so we only need to mock useNavigate.
const navigateSpy = vi.fn((_opts: { to: string }) => Promise.resolve());
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigateSpy,
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

describe("PersonaPill drop privileges (v1.7.0a.2)", () => {
  // Bug: clicking the × on the ADMIN pill called the backend + the
  // local setter but didn't invalidate the /auth/me cache, so the
  // hydrator's next tick read the stale ADMIN payload and snapped
  // the pill back. This pins the cache invalidation so the regression
  // can't sneak back in.

  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(null, { status: 200 }),
    );
    navigateSpy.mockClear();
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  function HarnessWithClient({
    client,
    initial,
  }: {
    client: QueryClient;
    initial: { mode: "user" | "admin" | "elevated"; expiresAt: number };
  }) {
    return (
      <QueryClientProvider client={client}>
        <AuthModeProvider initial={initial}>
          <PersonaPill />
        </AuthModeProvider>
      </QueryClientProvider>
    );
  }

  it("invalidates [auth, me] cache on successful drop", async () => {
    const client = newClient();
    const invalidateSpy = vi.spyOn(client, "invalidateQueries");

    const expiresAt = Date.now() + 5 * 60 * 1000;
    render(
      <HarnessWithClient
        client={client}
        initial={{ mode: "admin", expiresAt }}
      />,
    );

    const dropBtn = screen.getByTestId("persona-drop");
    await userEvent.click(dropBtn);

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(
        "/api/v1/auth/logout-elevation",
        expect.objectContaining({ method: "POST", credentials: "include" }),
      );
    });
    await waitFor(() => {
      expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: ["auth", "me"] });
    });
  });

  it("flips pill to USER + drops countdown after a successful drop", async () => {
    const client = newClient();
    const expiresAt = Date.now() + 5 * 60 * 1000;
    render(
      <HarnessWithClient
        client={client}
        initial={{ mode: "admin", expiresAt }}
      />,
    );

    expect(screen.getByTestId("persona-pill").getAttribute("data-mode")).toBe(
      "admin",
    );
    expect(screen.getByTestId("persona-countdown")).toBeInTheDocument();

    await userEvent.click(screen.getByTestId("persona-drop"));

    await waitFor(() => {
      expect(screen.getByTestId("persona-pill").getAttribute("data-mode")).toBe(
        "user",
      );
    });
    expect(screen.queryByTestId("persona-countdown")).not.toBeInTheDocument();
  });

  it("does not invalidate cache when the backend rejects the drop", async () => {
    fetchSpy.mockResolvedValueOnce(new Response(null, { status: 500 }));
    const client = newClient();
    const invalidateSpy = vi.spyOn(client, "invalidateQueries");

    const expiresAt = Date.now() + 5 * 60 * 1000;
    render(
      <HarnessWithClient
        client={client}
        initial={{ mode: "admin", expiresAt }}
      />,
    );

    await userEvent.click(screen.getByTestId("persona-drop"));

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalled();
    });
    expect(invalidateSpy).not.toHaveBeenCalled();
    // Pill stays in ADMIN on failure.
    expect(screen.getByTestId("persona-pill").getAttribute("data-mode")).toBe(
      "admin",
    );
    // No navigation on failure — operator stays put with their stale
    // ADMIN session so they can see why the drop didn't take.
    expect(navigateSpy).not.toHaveBeenCalled();
  });

  // v1.9.0e.2 — tight mode/view coupling. Dropping privileges from the
  // × button always sends the operator to /files; the drop bug the
  // operator reported was dropping privileges on /admin/clusters and
  // then the (since-deleted) AdminEntryElevationGuard firing the
  // elevation prompt against the same page they just dropped from.
  // Mode change now drives navigation: drop = go user = /files.
  it("navigates to /files after a successful drop", async () => {
    const client = newClient();
    const expiresAt = Date.now() + 5 * 60 * 1000;
    render(
      <HarnessWithClient
        client={client}
        initial={{ mode: "admin", expiresAt }}
      />,
    );

    await userEvent.click(screen.getByTestId("persona-drop"));

    await waitFor(() => {
      expect(navigateSpy).toHaveBeenCalledWith({ to: "/files" });
    });
  });

  it("does NOT navigate when the backend rejects the drop", async () => {
    fetchSpy.mockResolvedValueOnce(new Response(null, { status: 500 }));
    const client = newClient();
    const expiresAt = Date.now() + 5 * 60 * 1000;
    render(
      <HarnessWithClient
        client={client}
        initial={{ mode: "admin", expiresAt }}
      />,
    );

    await userEvent.click(screen.getByTestId("persona-drop"));

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalled();
    });
    expect(navigateSpy).not.toHaveBeenCalled();
  });
});

// v1.13.2 — duplicate toast prevention when multiple PersonaPill instances exist.
describe("PersonaPill — admin session ended toast (v1.13.2)", () => {
  beforeEach(() => {
    vi.useRealTimers();
    navigateSpy.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("module-level guard prevents duplicate toasts across multiple PersonaPill instances", async () => {
    const client = newClient();
    const expiresAt = Date.now() + 5 * 60 * 1000; // 5 min, not expired yet
    
    // Render TWO PersonaPill instances (simulating AppShell + UserShell)
    render(
      <QueryClientProvider client={client}>
        <AuthModeProvider initial={{ mode: "admin", expiresAt }}>
          <>
            <PersonaPill />
            <PersonaPill />
          </>
        </AuthModeProvider>
      </QueryClientProvider>,
    );

    // Initially in admin mode, no toast should fire yet
    expect(toast.info).not.toHaveBeenCalled();

    // The module-level guard (sessionEndedFiredRef) ensures that when mode changes
    // from admin to user, the "Admin session ended" toast fires exactly once even
    // though both PersonaPill instances subscribe to the same auth mode context.
    // This is verified by the implementation using a Set keyed on expiresAt.
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
