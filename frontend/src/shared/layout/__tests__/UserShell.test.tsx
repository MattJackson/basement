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
    Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
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
  it("renders Files, Keys, Shares nav items in Primary nav", async () => {
    const { UserShell } = await import("@/shared/layout/UserShell");
    render(<UserShell><div data-testid="child">Content</div></UserShell>, { wrapper: Wrapper });

    expect(screen.getByText("Files")).toBeInTheDocument();
    expect(screen.getByText("Keys")).toBeInTheDocument();
    expect(screen.getByText("Shares")).toBeInTheDocument();
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
});
