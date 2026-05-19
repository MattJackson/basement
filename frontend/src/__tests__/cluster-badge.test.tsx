import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { ClusterBadge } from "@/components/ClusterBadge";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
});

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      {children}
    </QueryClientProvider>
  );
}

describe("ClusterBadge", () => {
  it("renders label + color when connection exists in cache", async () => {
    queryClient.setQueryData(["admin", "clusters"], [
      {
        id: "conn-123",
        label: "Production",
        driver: "garage",
        config: {},
        owner: "org",
        createdAt: new Date().toISOString(),
        color: "#FF5733",
      },
    ]);

    render(<ClusterBadge connectionId="conn-123" />, { wrapper: Wrapper });

    expect(screen.getByText("Production")).toBeInTheDocument();
  });

  it("falls back to id-slice when connection missing from cache", async () => {
    render(<ClusterBadge connectionId="unknown-id-123" />, { wrapper: Wrapper });

    expect(screen.getByText(/unknown-/)).toBeInTheDocument();
  });

  it("renders sm size correctly", async () => {
    queryClient.setQueryData(["admin", "clusters"], [
      {
        id: "conn-456",
        label: "Staging",
        driver: "garage-v1",
        config: {},
        owner: "org",
        createdAt: new Date().toISOString(),
        color: "#33FF57",
      },
    ]);

    render(<ClusterBadge connectionId="conn-456" size="sm" />, { wrapper: Wrapper });

    expect(screen.getByText("Staging")).toBeInTheDocument();
  });
});
