// v1.11.0.28 — NewVersionBanner with version display.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import React from "react";
import { render, screen } from "@testing-library/react";

describe("NewVersionBanner", () => {
  let subscribeMock: ReturnType<typeof vi.fn>;
  let NewVersionBanner: React.FC;

  beforeEach(async () => {
    vi.resetModules();
    subscribeMock = vi.fn().mockReturnValue(() => {});
    
    const mockModule = await import("@/shared/api/buildWatcher");
    vi.spyOn(mockModule, "subscribe").mockImplementation((fn: any) => {
      subscribeMock.mockImplementation(fn);
      return () => {};
    });

    const module = await import("@/shared/ui/NewVersionBanner");
    NewVersionBanner = module.NewVersionBanner;
    
    render(<NewVersionBanner />);
  });

  it("shows generic message when version is undefined", async () => {
    const callback = subscribeMock.mock.calls[0]?.[0];
    if (callback) {
      callback({ mismatched: true, serverVersion: undefined });
      
      expect(screen.getByText(/A new version of basement is available/)).toBeInTheDocument();
      expect(screen.queryByText(/Basement v\d+\.\d+\.\d+\.\d+/)).not.toBeInTheDocument();
    }
  });

  it("shows version when present", async () => {
    const callback = subscribeMock.mock.calls[0]?.[0];
    if (callback) {
      callback({ mismatched: true, serverVersion: "v1.11.0.28" });
      
      expect(screen.getByText(/Basement v1\.11\.0\.28 now available — Refresh/)).toBeInTheDocument();
    }
  });

  it("does not render when mismatched is false", async () => {
    const callback = subscribeMock.mock.calls[0]?.[0];
    if (callback) {
      callback({ mismatched: false, serverVersion: undefined });
      
      expect(screen.queryByText(/Basement/)).not.toBeInTheDocument();
      expect(screen.queryByText(/A new version of basement/)).not.toBeInTheDocument();
    }
  });

  afterEach(() => {
    subscribeMock.mockClear();
    vi.clearAllMocks();
  });
});
