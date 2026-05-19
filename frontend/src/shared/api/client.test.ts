import { describe, it, expect } from "vitest";
import { client } from "./client";

describe("API client", () => {
  it("exports a typed object", () => {
    expect(client).toBeDefined();
    expect(typeof client).toBe("object");
  });

  it("has GET method", () => {
    expect(client.GET).toBeDefined();
    expect(typeof client.GET).toBe("function");
  });

  it("has POST method", () => {
    expect(client.POST).toBeDefined();
    expect(typeof client.POST).toBe("function");
  });
});
