import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { Logo } from "@/shared/ui/Logo";

// Mock useVersion to return a specific version for all tests in this file
vi.mock("@/shared/api/queries", () => ({
  useVersion: vi.fn(() => ({
    data: {
      version: "v0.4.5",
      commit: "abc123def",
      builtAt: new Date().toISOString(),
    },
    isLoading: false,
    error: null,
  })),
}));

function Wrapper({ children }: { children: React.ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={queryClient}>
      {children}
    </QueryClientProvider>
  );
}

describe("Logo with version (Fix 7)", () => {
  it("renders Basement wordmark and version label when useVersion returns data", async () => {
    render(<Logo href="/" />, { wrapper: Wrapper });

    expect(screen.getByText("Basement")).toBeInTheDocument();
    expect(screen.getByText("v0.4.5")).toBeInTheDocument();
    
    const versionElement = screen.getByText("v0.4.5");
    expect(versionElement).toHaveClass("text-[10px]");
  });

  it("renders icon-only version without wordmark or version", async () => {
    render(<Logo href="/" iconOnly />, { wrapper: Wrapper });

    expect(screen.queryByText("Basement")).not.toBeInTheDocument();
    expect(screen.queryByText(/^v\d+\.\d+\.\d+$/)).not.toBeInTheDocument();
  });
});
