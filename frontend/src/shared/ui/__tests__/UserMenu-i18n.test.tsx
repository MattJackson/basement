import { describe, it, expect, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nextProvider } from "react-i18next";
import i18n from "@/shared/i18n";

import { UserMenu } from "@/shared/ui/UserMenu";
import { AuthModeProvider } from "@/shared/auth/mode";
import { ElevationProvider } from "@/shared/auth/elevation";

const navigateMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal() as Record<string, unknown>;
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
    data: { username: "testuser", role: "user" as const, availableRoles: [] },
    isLoading: false,
    isError: false,
  })),
}));

vi.mock("@/shared/api/queries", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    useVersion: vi.fn(() => ({
      data: { version: "v2.0.0-beta.3", commit: "abc1234", builtAt: "" },
      isLoading: false,
      error: null,
    })),
    useOrgCapabilities: () => ({ data: {} }),
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

beforeEach(() => {
  navigateMock.mockReset();
  localStorage.clear();
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

function Wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={qc}>
        <AuthModeProvider initial={{ mode: "user", expiresAt: 0 }}>
          <ElevationProvider>{children}</ElevationProvider>
        </AuthModeProvider>
      </QueryClientProvider>
    </I18nextProvider>
  );
}

describe("Language switcher in UserMenu", () => {
  it("renders the language submenu trigger", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    
    fireEvent.click(screen.getByLabelText("Open admin menu"));
    
    const languageTrigger = await screen.findByTestId("language-submenu-trigger");
    expect(languageTrigger).toBeInTheDocument();
  });

  it("changes language when selecting Spanish", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    
    fireEvent.click(screen.getByLabelText("Open admin menu"));
    
    const languageTrigger = await screen.findByTestId("language-submenu-trigger");
    fireEvent.click(languageTrigger);
    
    const espanolOption = await screen.findByText("Español");
    fireEvent.click(espanolOption);
    
    expect(i18n.language).toBe("es");
  });

  it("persists language choice to localStorage", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    
    fireEvent.click(screen.getByLabelText("Open admin menu"));
    
    const languageTrigger = await screen.findByTestId("language-submenu-trigger");
    fireEvent.click(languageTrigger);
    
    const espanolOption = await screen.findByText("Español");
    fireEvent.click(espanolOption);
    
    expect(localStorage.getItem("basement_language")).toBe("es");
  });

  it("shows both English and Spanish options", async () => {
    render(<UserMenu />, { wrapper: Wrapper });
    
    fireEvent.click(screen.getByLabelText("Open admin menu"));
    
    const languageTrigger = await screen.findByTestId("language-submenu-trigger");
    fireEvent.click(languageTrigger);
    
    expect(await screen.findByText("English")).toBeInTheDocument();
    expect(await screen.findByText("Español")).toBeInTheDocument();
  });
});
