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

type MockUserData = {
  username: string;
  role: "admin" | "user";
  uiAdmin: boolean;
  oidcUser?: boolean;
  activeRole: any | null;
  availableRoles: Array<{ kind: string; label: string; cluster?: string }>;
};

let userMockData: MockUserData = { 
  username: "matthew", 
  role: "user", 
  uiAdmin: false, 
  oidcUser: false, 
  activeRole: null, 
  availableRoles: [] 
};

vi.mock("@/shared/auth/useUser", () => ({
  useUser: vi.fn(() => ({
    data: userMockData,
    isLoading: false,
    isError: false,
  })),
}));

export const setUserMock = (userData: Partial<Omit<MockUserData, "availableRoles">> & { availableRoles?: Array<{ kind: string; label: string; cluster?: string }> }) => {
  userMockData = { 
    ...userMockData,
    username: userData.username ?? userMockData.username,
    role: userData.role ?? userMockData.role,
    uiAdmin: userData.uiAdmin ?? userMockData.uiAdmin,
    oidcUser: userData.oidcUser ?? false,
    activeRole: userData.activeRole !== undefined ? userData.activeRole : null,
    availableRoles: userData.availableRoles ?? [],
  };
};

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useVersion: vi.fn(() => ({
      data: { version: "v1.3.0a.3", commit: "abc1234", builtAt: "" },
      isLoading: false,
      error: null,
    })),
    useOrgCapabilities: () => ({ data: {} }),
    useActiveSkin: () => ({ data: null, isLoading: false }),
    useSetActiveSkin: vi.fn(() => ({ mutate: vi.fn() })),
  };
});

vi.mock("@/shared/hooks/useSkin", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useSkinRegistry: () => ({ data: [] }),
    useSkin: () => ({ skin: null, error: null, densityTokens: [], borderRadius: "", setSelectedSkin: vi.fn() }),
  };
});

// v1.13.0a (ADR-0008): UserMenu now mounts useTheme() to drive the
// Theme submenu. useTheme touches window.matchMedia at first render,
// so every test in this file needs a stub or the component throws
// before any assertion can run. Top-level beforeEach (not per-suite)
// so a regression that adds a new describe doesn't silently break.
beforeEach(() => {
  Object.defineProperty(globalThis.window, "matchMedia", {
    writable: true,
    configurable: true,
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

  // v1.13.32: removed two tests that referenced data-testid="switch-to-admin",
  // a button the v1.13.18 active-role selector cycle removed. The role-submenu-trigger
  // tests at the bottom of this file cover the replacement flow. Keep the
  // "ADMIN mode: navigates immediately" test below — it doesn't reference
  // the removed testids and still validates the auto-nav behavior.

  it("ADMIN mode: navigates immediately without opening the modal", async () => {
    render(<UserMenu />, {
      wrapper: ({ children }) => (
        <Wrapper initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }}>
          {children}
        </Wrapper>
      ),
    });

    fireEvent.click(screen.getByLabelText("Open admin menu"));

    // In admin mode, clicking the user menu should navigate immediately to /admin/clusters
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters" });
    });
    // No modal should be in the DOM.
    expect(screen.queryByTestId("elevation-password")).not.toBeInTheDocument();
  });

  // "Cancel from elevation modal" test removed in v1.13.32 — relied on
  // data-testid="switch-to-admin" (removed in v1.13.18 by active-role selector).
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

// v1.13.32: removed "UserMenu — Switch to admin (v1.9.0e.2 routing)" describe
// block — the single test relied on data-testid="switch-to-admin", removed in
// v1.13.18 active-role selector cycle. Equivalent post-elevation routing is
// now covered by the active-role mutation tests in
// src/shared/api/__tests__/mutations.test.ts.

// v1.13.0a (ADR-0008) — pluggable-skins foundation. The Theme submenu
// replaces the standalone ThemeToggle button that used to sit in
// AppShell / UserShell. Three radio options (System / Light / Dark);
// clicking each persists via the same useTheme cookie hook as the
// retired ThemeToggle.
describe("UserMenu — Theme submenu (v1.13.0a)", () => {
  beforeEach(() => {
    navigateMock.mockReset();
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
    // Each test starts from a clean cookie slate so the radio's
    // initial "system" default is observable.
    document.cookie = "basement_theme=; Path=/; Max-Age=0; SameSite=Lax";
  });

  it("renders the Theme submenu trigger inside the open dropdown", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    const trigger = await screen.findByTestId("theme-submenu-trigger");
    expect(trigger).toBeInTheDocument();
    expect(trigger).toHaveTextContent("Theme");
  });

  it("Theme submenu exposes System / Light / Dark radio options", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    const trigger = await screen.findByTestId("theme-submenu-trigger");
    fireEvent.click(trigger);

    expect(await screen.findByTestId("theme-system")).toHaveTextContent("System");
    expect(await screen.findByTestId("theme-light")).toHaveTextContent("Light");
    expect(await screen.findByTestId("theme-dark")).toHaveTextContent("Dark");
  });

  it("clicking Light persists the choice through the same cookie the old ThemeToggle wrote", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    const trigger = await screen.findByTestId("theme-submenu-trigger");
    fireEvent.click(trigger);

    const lightOption = await screen.findByTestId("theme-light");
    fireEvent.click(lightOption);

    await waitFor(() => {
      expect(document.cookie).toMatch(/basement_theme=light/);
    });
  });

  it("clicking Dark persists dark", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    const trigger = await screen.findByTestId("theme-submenu-trigger");
    fireEvent.click(trigger);

    const darkOption = await screen.findByTestId("theme-dark");
    fireEvent.click(darkOption);

    await waitFor(() => {
      expect(document.cookie).toMatch(/basement_theme=dark/);
    });
  });

  it("clicking System persists system", async () => {
    // Seed a non-system value first so the click is a real change.
    document.cookie = "basement_theme=light; Path=/; SameSite=Lax";

    render(<UserMenu />, { wrapper: Wrapper });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    const trigger = await screen.findByTestId("theme-submenu-trigger");
    fireEvent.click(trigger);

    const systemOption = await screen.findByTestId("theme-system");
    fireEvent.click(systemOption);

    await waitFor(() => {
      expect(document.cookie).toMatch(/basement_theme=system/);
    });
  });
});

// v1.13.32: removed "UserMenu — Switch to user (v1.9.0e.2)" describe block.
// All three tests relied on data-testid="switch-to-admin" / "switch-to-user",
// removed in v1.13.18 when the active-role selector replaced the binary
// mode switch. The drop-elevation flow is now exercised via the role
// selector tests at the bottom of this file + logout-elevation network
// behavior in src/shared/api/__tests__/mutations.test.ts.

describe("UserMenu — conditional rendering by role (v1.11.0.26)", () => {
  beforeEach(() => {
    navigateMock.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // v1.13.32: removed two tests that asserted the v1.x binary "Switch
  // to admin view" / "Switch to user view" buttons. The v1.13.18
  // active-role selector replaced those buttons and the equivalent
  // conditional visibility is now covered by the role-selector tests
  // at the bottom of this file.

  it("UI admin sees 'System settings' link", async () => {
    // v1.13.31 split admin nav by activeRole.kind: System settings only
    // renders when activeRole.kind === "ui-admin". The mock must set it.
    setUserMock({
      username: "matthew",
      role: "admin",
      uiAdmin: true,
      oidcUser: false,
      activeRole: { kind: "ui-admin" },
      availableRoles: [
        { kind: "user", label: "User" },
        { kind: "ui-admin", label: "UI Admin" },
      ],
    });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    expect(await screen.findByText("System settings")).toBeInTheDocument();
  });

it("non-UI admin does NOT see 'System settings' link", async () => {
    setUserMock({ username: "matthew", role: "admin", uiAdmin: false, oidcUser: false });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    expect(screen.queryByText("System settings")).not.toBeInTheDocument();
  });

it("user role does NOT see 'System settings' link", async () => {
    setUserMock({ username: "matthew", role: "user", uiAdmin: false, oidcUser: false });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    expect(screen.queryByText("System settings")).not.toBeInTheDocument();
  });
});

describe("UserMenu — Role selector (v1.13.18)", () => {
  beforeEach(() => {
    navigateMock.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("shows Role submenu with available roles", async () => {
    setUserMock({ 
      username: "matthew", 
      role: "user", 
      uiAdmin: true, 
      oidcUser: false,
      activeRole: { kind: "user" },
      availableRoles: [
        { kind: "user", label: "User" },
        { kind: "cluster-admin", cluster: "classe", label: "Cluster Admin: classe" },
        { kind: "ui-admin", label: "UI Admin" }
      ]
    });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "user", expiresAt: 0 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    expect(await screen.findByTestId("role-submenu-trigger")).toBeInTheDocument();
  });

  it("Role submenu shows User, Cluster Admin options, and UI Admin when eligible", async () => {
    setUserMock({ 
      username: "matthew", 
      role: "user", 
      uiAdmin: true, 
      oidcUser: false,
      activeRole: { kind: "cluster-admin", cluster: "classe" },
      availableRoles: [
        { kind: "user", label: "User" },
        { kind: "cluster-admin", cluster: "classe", label: "Cluster Admin: classe" },
        { kind: "ui-admin", label: "UI Admin" }
      ]
    });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "user", expiresAt: 0 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));
    
    // Open role submenu
    const roleTrigger = await screen.findByTestId("role-submenu-trigger");
    fireEvent.click(roleTrigger);

    expect(await screen.findByText("User")).toBeInTheDocument();
    expect(await screen.findByText("Cluster Admin: classe")).toBeInTheDocument();
    expect(await screen.findByText("UI Admin")).toBeInTheDocument();
  });

  it("Role submenu shows only User when not eligible for admin roles", async () => {
    setUserMock({ 
      username: "matthew", 
      role: "user", 
      uiAdmin: false, 
      oidcUser: false,
      activeRole: { kind: "user" },
      availableRoles: [
        { kind: "user", label: "User" }
      ]
    });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "user", expiresAt: 0 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));
    
    // Open role submenu
    const roleTrigger = await screen.findByTestId("role-submenu-trigger");
    fireEvent.click(roleTrigger);

    expect(await screen.findByText("User")).toBeInTheDocument();
    expect(screen.queryByText("Cluster Admin")).not.toBeInTheDocument();
    expect(screen.queryByText("UI Admin")).not.toBeInTheDocument();
  });

  it("Role selector displays current role in trigger", async () => {
    setUserMock({ 
      username: "matthew", 
      role: "user", 
      uiAdmin: true, 
      oidcUser: false,
      activeRole: { kind: "cluster-admin", cluster: "lsi" },
      availableRoles: [
        { kind: "user", label: "User" },
        { kind: "cluster-admin", cluster: "classe", label: "Cluster Admin: classe" },
        { kind: "cluster-admin", cluster: "lsi", label: "Cluster Admin: lsi" },
        { kind: "ui-admin", label: "UI Admin" }
      ]
    });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "user", expiresAt: 0 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    const roleTrigger = await screen.findByTestId("role-submenu-trigger");
    expect(roleTrigger).toHaveTextContent(/Role.*Cluster Admin: lsi/);
  });
});

