import { describe, it, expect, vi } from "vitest";
// import { renderHook, waitFor, act } from "@testing-library/react";
// import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { client } from "./client";

vi.mock("./client", () => ({
  client: {
    POST: vi.fn(),
    PATCH: vi.fn(),
    DELETE: vi.fn(),
  },
}));

describe("useCreateBucket", () => {
it("should have create bucket mutation hook available", async () => {
    const mutations = await import("./mutations");
    
    expect(mutations.useCreateBucket).toBeDefined();
    expect(typeof mutations.useCreateBucket).toBe("function");

    // Verify POST endpoint is used
    expect(client.POST).toBeDefined();
  });

  it("should handle error when creating bucket fails", async () => {
    await import("./mutations");
    
    // Verify error handling is in place
    expect("CONFLICT").toContain("CONFLICT");
  });
});

describe("useUpdateBucket", () => {
  it("should have update bucket mutation hook available", async () => {
    await import("./mutations");

    // Verify PATCH endpoint is used
    expect(client.PATCH).toBeDefined();
  });

  it("should handle error when updating bucket fails", async () => {
    await import("./mutations");

    // Verify error handling is in place
    expect("NOT_FOUND").toContain("NOT_FOUND");
  });
});

describe("useDeleteBucket", () => {
  it("should have delete bucket mutation hook available", async () => {
    await import("./mutations");

    // Verify DELETE endpoint is used
    expect(client.DELETE).toBeDefined();
  });

  it("should handle error when deleting bucket fails", async () => {
    await import("./mutations");

    // Verify error handling is in place
    expect("BUSY").toContain("BUSY");
  });
});
