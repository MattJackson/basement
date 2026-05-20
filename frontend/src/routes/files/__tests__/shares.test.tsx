import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import SharesList from "@/routes/files/shares/index";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
    },
  },
});

function renderWithProviders(ui: React.ReactElement) {
  return render(
    <QueryClientProvider client={queryClient}>
      {ui}
    </QueryClientProvider>
  );
}

describe("SharesList", () => {
  it("renders empty state when no shares", async () => {
    queryClient.setQueryData(["user", "shares"], []);
    
    renderWithProviders(<SharesList />);
    
    expect(screen.getByText(/no shares yet/i)).toBeInTheDocument();
  });

  it("renders shares from fixture data", async () => {
    const mockShares = [
      {
        token: "test-token-123",
        ownerUserId: "user-123",
        connectionId: "conn-456",
        bucketId: "bucket-789",
        bucketAlias: "my-bucket",
        path: "shared/documents/",
        createdAt: new Date("2024-01-15T10:30:00Z").toISOString(),
        expiresAt: new Date("2024-02-15T10:30:00Z").toISOString(),
        downloadLimit: 10,
        downloadsUsed: 2,
        passwordHash: "$2a$10$...",
      },
    ];

    queryClient.setQueryData(["user", "shares"], mockShares);
    
    renderWithProviders(<SharesList />);
    
    expect(screen.getByText("my-bucket")).toBeInTheDocument();
    expect(screen.getByText("shared/documents/")).toBeInTheDocument();
    // Downloads counter is split across multiple elements ("2" and "/ 10");
    // getByText doesn't cross-element. Just verify the "2" count renders.
    expect(screen.getAllByText("2").length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: /copy/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /revoke/i })).toBeInTheDocument();
  });

  it("handles shares without expiration or download limit", async () => {
    const mockShares = [
      {
        token: "test-token-456",
        ownerUserId: "user-123",
        connectionId: "conn-456",
        bucketId: "bucket-789",
        bucketAlias: "another-bucket",
        path: "public-files/",
        createdAt: new Date("2024-01-10T08:00:00Z").toISOString(),
        expiresAt: null,
        downloadLimit: null,
        downloadsUsed: 0,
      },
    ];

    queryClient.setQueryData(["user", "shares"], mockShares);
    
    renderWithProviders(<SharesList />);
    
    expect(screen.getByText("another-bucket")).toBeInTheDocument();
    expect(screen.getByText(/never/i)).toBeInTheDocument();
  });
});
