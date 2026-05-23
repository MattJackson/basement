// v1.3.0a.3 — UserMenu "Switch to admin view" elevation flow.
//
// Before this cycle the menu item was a plain <Link> that navigated
// straight into /admin/* — the first admin action then returned 403
// ELEVATION_REQUIRED and forced a re-click cycle. The fix:
//
//   - USER mode click → elevation modal pops up first, navigates to
//     /admin/clusters only after a successful submit.
//   - ADMIN / ELEVATED mode click → navigates immediately (no
//     redundant prompt).
//   - Cancel from the modal → stays put, no navigation.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { UserMenu } from "@/shared/ui/UserMenu";
import { AuthModeProvider, type ModeState } from "@/shared/auth/mode";
import { ElevationProvider } from "@/shared/auth/elevation";

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    Link: ({ children, ...rest }: { children: React.ReactNode } & Record<string, unknown>) => (
      <a {...rest}>{children}</a>
    ),
    useNavigate: () => navigateMock,
  };
});

vi.mock("@/shared/auth/useUser", () => ({
  useUser: vi.fn(() => ({
    data: { username: "matthew", role: "user", oidcUser: false },
    isLoading: false,
    error: null,
  })),
}));

vi.mock("@/shared/api/queries", () => ({
  useVersion: vi.fn(() => ({
    data: { version: "v1.3.0a.3", commit: "abc1234", builtAt: "" },
    isLoading: false,
    error: null,
  })),
}));

function Wrapper({
  children,
  initialMode = { mode: "user", expiresAt: 0 },
}: {
  children: React.ReactNode;
  initialMode?: ModeState;
}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <QueryClientProvider client={qc}>
      <AuthModeProvider initial={initialMode}>
        <ElevationProvider>{children}</ElevationProvider>
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("UserMenu — Switch to admin elevation", () => {
  beforeEach(() => {
    navigateMock.mockReset();
    // The elevation modal POSTs to /api/v1/auth/elevate. Default-stub
    // it to a 200 so a "happy path" elevation just works; individual
    // tests can override.
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          mode: "admin",
          mode_expires_at: Math.floor(Date.now() / 1000) + 900,
          mode_ttl_seconds: 900,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("USER mode: clicking 'Switch to admin view' opens the elevation modal and navigates on success", async () => {
    render(<UserMenu />, { wrapper: Wrapper });

    // Open the dropdown.
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    // Click the switch-to-admin entry.
    const switchItem = await screen.findByTestId("switch-to-admin");
    fireEvent.click(switchItem);

    // Modal should open (USER mode → needs elevate).
    const password = await screen.findByTestId("elevation-password");
    expect(password).toBeInTheDocument();
    // Navigation should NOT have fired yet.
    expect(navigateMock).not.toHaveBeenCalled();

    // Submit the password — stubbed fetch returns 200 → onSuccess
    // resolves → navigation fires.
    fireEvent.change(password, { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters" });
    });
  });

  it("ADMIN mode: navigates immediately without opening the modal", async () => {
    render(<UserMenu />, {
      wrapper: ({ children }) => (
        <Wrapper initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }}>
          {children}
        </Wrapper>
      ),
    });

    fireEvent.click(screen.getByLabelText("Open admin menu"));
    fireEvent.click(await screen.findByTestId("switch-to-admin"));

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters" });
    });
    // No modal should be in the DOM.
    expect(screen.queryByTestId("elevation-password")).not.toBeInTheDocument();
  });

  it("Cancel from elevation modal: does NOT navigate", async () => {
    render(<UserMenu />, { wrapper: Wrapper });

    fireEvent.click(screen.getByLabelText("Open admin menu"));
    fireEvent.click(await screen.findByTestId("switch-to-admin"));

    await screen.findByTestId("elevation-password");
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    // Wait a tick — give the cancel promise a chance to reject + the
    // catch block to swallow it.
    await waitFor(() => {
      expect(screen.queryByTestId("elevation-password")).not.toBeInTheDocument();
    });
    expect(navigateMock).not.toHaveBeenCalled();
  });
});

// v1.10.0.2 — UserMenu trigger tap-target. Pre-fix the trigger
// rendered at ~40px tall on mobile, below the WCAG/iOS HIG 44×44
// threshold flagged in the v1.10.0.1 smoke audit. Class-based
// assertion (jsdom doesn't run Tailwind's stylesheet).
describe("UserMenu trigger tap-target (v1.10.0.2)", () => {
  beforeEach(() => {
    navigateMock.mockReset();
  });

  it("trigger carries the 44px min-height + min-width tap-target utilities on mobile", () => {
    render(<UserMenu />, { wrapper: Wrapper });
    const trigger = screen.getByLabelText("Open admin menu");
    expect(trigger.className).toMatch(/min-h-\[44px\]/);
    expect(trigger.className).toMatch(/min-w-\[44px\]/);
  });
});

describe("UserMenu — Switch to admin (v1.9.0e.2 routing)", () => {
  // v1.9.0e.2 pins the post-elevation landing target. Under the new
  // tight mode/view coupling, elevating always navigates to /admin
  // (specifically /admin/clusters as the admin landing route). Already-
  // elevated clicks short-circuit straight to the same target.
  beforeEach(() => {
    navigateMock.mockReset();
    vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(
        JSON.stringify({
          mode: "admin",
          mode_expires_at: Math.floor(Date.now() / 1000) + 900,
          mode_ttl_seconds: 900,
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("navigates to /admin/clusters after successful elevation (USER mode)", async () => {
    render(<UserMenu />, { wrapper: Wrapper });

    fireEvent.click(screen.getByLabelText("Open admin menu"));
    fireEvent.click(await screen.findByTestId("switch-to-admin"));

    const password = await screen.findByTestId("elevation-password");
    fireEvent.change(password, { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: /^submit$/i }));

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters" });
    });
  });
});

describe("UserMenu — Switch to user (v1.9.0e.2)", () => {
  // Tight coupling: "Switch to user view" drops privileges AND
  // navigates. USER-mode clicks just navigate (no privileges to
  // drop). On a successful drop the local mode flips to USER and the
  // /auth/me cache invalidates so the next hydrate sees the rotated
  // cookie.
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    navigateMock.mockReset();
    fetchSpy = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(null, { status: 200 }),
    );
  });

  afterEach(() => {
    fetchSpy.mockRestore();
  });

  it("USER mode: navigates to /files without calling logout-elevation", async () => {
    render(<UserMenu />, { wrapper: Wrapper });

    fireEvent.click(screen.getByLabelText("Open admin menu"));
    fireEvent.click(await screen.findByTestId("switch-to-user"));

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
    });
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("ADMIN mode: POSTs logout-elevation, then navigates to /files", async () => {
    render(<UserMenu />, {
      wrapper: ({ children }) => (
        <Wrapper
          initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }}
        >
          {children}
        </Wrapper>
      ),
    });

    fireEvent.click(screen.getByLabelText("Open admin menu"));
    fireEvent.click(await screen.findByTestId("switch-to-user"));

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalledWith(
        "/api/v1/auth/logout-elevation",
        expect.objectContaining({ method: "POST", credentials: "include" }),
      );
    });
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/files" });
    });
  });

  it("ADMIN mode + backend rejects drop: does NOT navigate", async () => {
    fetchSpy.mockResolvedValueOnce(new Response(null, { status: 500 }));
    render(<UserMenu />, {
      wrapper: ({ children }) => (
        <Wrapper
          initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }}
        >
          {children}
        </Wrapper>
      ),
    });

    fireEvent.click(screen.getByLabelText("Open admin menu"));
    fireEvent.click(await screen.findByTestId("switch-to-user"));

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalled();
    });
    expect(navigateMock).not.toHaveBeenCalled();
  });
});
