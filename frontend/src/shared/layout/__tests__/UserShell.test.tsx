import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthModeProvider } from "@/shared/auth/mode";
import { ElevationProvider } from "@/shared/auth/elevation";

vi.mock("@/shared/api/queries", () => ({
  useVersion: vi.fn(() => ({
    data: {
      version: "v0.5.3",
      commit: "def456ghi",
      builtAt: new Date().toISOString(),
    },
    isLoading: false,
    error: null,
  })),
}));

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = (await importOriginal()) as Record<string, unknown>;
  return {
    ...actual,
    // v1.10.0.1 — forward `className` (and the rest of common anchor
    // props) so the tap-target tests below can assert the nav links
    // carry the min-h-[44px] utility. Pre-fix the mock dropped every
    // prop except children.
    Link: ({
      children,
      className,
      to,
      activeProps,
    }: {
      children: React.ReactNode;
      className?: string;
      to?: string;
      activeProps?: { className?: string };
    }) => (
      <a
        href={typeof to === "string" ? to : undefined}
        className={[className, activeProps?.className].filter(Boolean).join(" ")}
      >
        {children}
      </a>
    ),
    Outlet: () => null,
    useNavigate: vi.fn(() => vi.fn()),
    useSearch: vi.fn(() => ({})),
    useRouter: vi.fn(() => ({ isServer: false })),
  };
});

beforeEach(() => {
  Object.defineProperty(globalThis.window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query) => ({
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

function Wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  // v1.3.0a.3: UserMenu's "Switch to admin" handler reads useAuthMode +
  // useElevationPrompt; the test tree needs both providers mounted or
  // the component throws on first render.
  return (
    <QueryClientProvider client={queryClient}>
      <AuthModeProvider>
        <ElevationProvider>{children}</ElevationProvider>
      </AuthModeProvider>
    </QueryClientProvider>
  );
}

describe("UserShell", () => {
  it("renders Files, Keys, Shares, Backups nav items in Primary nav", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    expect(screen.getByText("Files")).toBeInTheDocument();
    expect(screen.getByText("Keys")).toBeInTheDocument();
    expect(screen.getByText("Shares")).toBeInTheDocument();
    // v1.5.0a: scheduled backups land alongside the other user-tier
    // primary-nav items.
    expect(screen.getByText("Backups")).toBeInTheDocument();
    // v1.6.0d: federations.
    expect(screen.getByText("Federations")).toBeInTheDocument();
    // v1.7.0e: webhook subscriptions.
    expect(screen.getByText("Webhooks")).toBeInTheDocument();
  });

  it("does NOT render Clusters nav item", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    const primaryNav = screen.getByRole("navigation", { name: "Primary" });
    expect(primaryNav).not.toHaveTextContent("Clusters");
  });

  it("renders Logo with href='/files'", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    const logo = screen.getByLabelText("Basement — home");
    expect(logo).toHaveAttribute("href", "/files");
  });

  it("renders ThemeToggle and UserMenu on the right", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    expect(screen.getByLabelText(/theme/i)).toBeInTheDocument();
    expect(screen.getByLabelText("Open admin menu")).toBeInTheDocument();
  });

  it("renders NewVersionBanner", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    const banner = screen.getByText(/^v\d+\.\d+\.\d+$/);
    expect(banner).toBeInTheDocument();
  });

  // v1.10.0.1 — nav links + logo carry the min-h-[44px] tap-target
  // utility so mobile viewports satisfy the WCAG/iOS HIG 44×44
  // threshold (smoke section [E] caught the pre-fix 20px / 40px sizes).
  // Class-based assertion rather than computed style because jsdom
  // does not implement Tailwind's runtime stylesheet.
  it("nav links carry the 44px min-height tap-target utility", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    const primaryNav = screen.getByRole("navigation", { name: "Primary" });
    const links = primaryNav.querySelectorAll("a");
    expect(links.length).toBeGreaterThan(0);
    for (const link of Array.from(links)) {
      expect(link.className).toMatch(/min-h-\[44px\]/);
    }
  });

  it("logo anchor carries the 44px min-height + min-width tap-target utilities", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    const logo = screen.getByLabelText("Basement — home");
    expect(logo.className).toMatch(/min-h-\[44px\]/);
    expect(logo.className).toMatch(/min-w-\[44px\]/);
  });
});
