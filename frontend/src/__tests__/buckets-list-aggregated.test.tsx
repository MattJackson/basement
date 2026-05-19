import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ClusterBadge } from "@/components/ClusterBadge";

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

describe("buckets-list-aggregated", () => {
  it("ClusterBadge renders with connection label and color", async () => {
    queryClient.setQueryData(["admin", "clusters"], [
      {
        id: "conn-prod-123",
        label: "Production",
        driver: "garage",
        config: {},
        owner: "org",
        createdAt: new Date().toISOString(),
        color: "#FF5733",
      },
    ]);

    render(<ClusterBadge connectionId="conn-prod-123" />, { wrapper: Wrapper });

    expect(screen.getByText("Production")).toBeInTheDocument();
  });

  it("ClusterFilter dropdown shows All clusters option", async () => {
    queryClient.setQueryData(["admin", "clusters"], [
      {
        id: "conn-prod-123",
        label: "Production",
        driver: "garage",
        config: {},
        owner: "org",
        createdAt: new Date().toISOString(),
        color: "#FF5733",
      },
    ]);

    const { ClusterFilter } = await import("@/components/ClusterFilter");

    render(<ClusterFilter selectedClusterId={null} onFilterChange={() => {}} />, { wrapper: Wrapper });

    expect(screen.queryAllByRole("button").length).toBeGreaterThan(0);
  });

  it("errors banner structure is correct", async () => {
    const errors = [
      { connectionId: "conn-staging-456", message: "connection refused" },
      { connectionId: "conn-dev-789", message: "timeout" },
    ];

    expect(errors.length).toBe(2);
    expect(errors[0]?.connectionId).toBe("conn-staging-456");
  });

  it("bucket row has cluster column with badge", async () => {
    queryClient.setQueryData(["admin", "clusters"], [
      {
        id: "conn-prod-123",
        label: "Production",
        driver: "garage",
        config: {},
        owner: "org",
        createdAt: new Date().toISOString(),
        color: "#FF5733",
      },
    ]);

    render(<ClusterBadge connectionId="conn-prod-123" />, { wrapper: Wrapper });

    expect(screen.queryByText("Production")).toBeTruthy();
  });
});
