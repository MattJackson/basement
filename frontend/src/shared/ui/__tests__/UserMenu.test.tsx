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

  it("USER mode: clicking 'Switch to admin view' opens the elevation modal and navigates on success", async () => {
    setUserMock({ username: "matthew", role: "user", uiAdmin: true, oidcUser: false });

    render(<UserMenu />, { wrapper: Wrapper });

    // Open the dropdown.
    fireEvent.click(screen.getByLabelText("Open admin menu"));
    
    // Clear auto-navigation that may have fired
    navigateMock.mockClear();

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

    // In admin mode, clicking the user menu should navigate immediately to /admin/clusters
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters" });
    });
    // No modal should be in the DOM.
    expect(screen.queryByTestId("elevation-password")).not.toBeInTheDocument();
  });

  it("Cancel from elevation modal: does NOT navigate", async () => {
    setUserMock({ username: "matthew", role: "user", uiAdmin: true, oidcUser: false });

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
    setUserMock({ username: "matthew", role: "user", uiAdmin: true, oidcUser: false });

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

describe("UserMenu — Switch to user (v1.9.0e.2)", () => {
  // Tight coupling: "Switch to user view" drops privileges AND
  // navigates. USER-mode clicks just navigate (no privileges to
  // drop). On a successful drop the local mode flips to USER and the
  // /auth/me cache invalidates so the next hydrate sees the rotated
  // cookie.
  let fetchSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    navigateMock.mockReset();
    setUserMock({ username: "matthew", role: "admin", uiAdmin: true, oidcUser: false });
    fetchSpy = vi.spyOn(window, "fetch").mockResolvedValue(
      new Response(null, { status: 200 }),
    );
  });

  afterEach(() => {
    fetchSpy.mockRestore();
    setUserMock({ username: "matthew", role: "user", uiAdmin: false, oidcUser: false });
    navigateMock.mockClear();
  });

  it("USER mode: sees 'Switch to admin view' button", async () => {
    setUserMock({ username: "matthew", role: "user", uiAdmin: true, oidcUser: false });

    render(<UserMenu />, { wrapper: Wrapper });

    fireEvent.click(screen.getByLabelText("Open admin menu"));
    const switchToAdmin = await screen.findByTestId("switch-to-admin");
    expect(switchToAdmin).toBeInTheDocument();
    expect(switchToAdmin).toHaveTextContent("Switch to admin view");
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
    
    // Wait for auto-navigation to /admin/clusters
    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith({ to: "/admin/clusters" });
    });
    
    navigateMock.mockClear();
    
    fireEvent.click(await screen.findByTestId("switch-to-user"));

    await waitFor(() => {
      expect(fetchSpy).toHaveBeenCalled();
    });
    // Navigation should not happen when drop fails
    expect(navigateMock).not.toHaveBeenCalledWith({ to: "/files" });
  });
});

describe("UserMenu — conditional rendering by role (v1.11.0.26)", () => {
  beforeEach(() => {
    navigateMock.mockReset();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("user role sees 'Switch to admin view', NOT 'Switch to user view'", async () => {
    setUserMock({ username: "matthew", role: "user", uiAdmin: true, oidcUser: false });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "user", expiresAt: 0 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    expect(await screen.findByTestId("switch-to-admin")).toBeInTheDocument();
    expect(screen.queryByTestId("switch-to-user")).not.toBeInTheDocument();
  });

  it("admin role sees 'Switch to user view', NOT 'Switch to admin view'", async () => {
    setUserMock({ username: "matthew", role: "admin", uiAdmin: true, oidcUser: false });

    render(<UserMenu />, { wrapper: (p) => <Wrapper initialMode={{ mode: "admin", expiresAt: Date.now() + 900_000 }} {...p} /> });
    fireEvent.click(screen.getByLabelText("Open admin menu"));

    await waitFor(() => {
      expect(screen.queryByTestId("switch-to-user")).toBeInTheDocument();
    });
  });

it("UI admin sees 'System settings' link", async () => {
    setUserMock({ username: "matthew", role: "admin", uiAdmin: true, oidcUser: false });

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

