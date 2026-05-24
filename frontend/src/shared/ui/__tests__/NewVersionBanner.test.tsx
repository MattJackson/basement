// v1.11.0.29 — NewVersionBanner with SW unregister + cache clear on refresh.

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

  it("unregisters SW + clears caches on refresh click", async () => {
    const callback = subscribeMock.mock.calls[0]?.[0];
    if (!callback) return;
    
    callback({ mismatched: true, serverVersion: "v1.11.0.29" });
    
    const button = screen.getByRole("button", { name: /Refresh/i });
    expect(button).toBeInTheDocument();

    const unregisterMock = vi.fn().mockResolvedValue(true);
    const keysMock = vi.fn().mockResolvedValue(["basement"]);
    const deleteMock = vi.fn().mockResolvedValue(true);

    (navigator.serviceWorker as any) = {
      getRegistrations: vi.fn().mockResolvedValue([
        { unregister: unregisterMock },
      ]),
    };

    (globalThis.caches as any) = {
      keys: keysMock,
      delete: deleteMock,
    };

    const originalLocationReload = window.location.reload;
    const reloadMock = vi.fn();
    Object.defineProperty(window, "location", {
      value: { reload: reloadMock },
      writable: true,
    });

    await button.click();

    expect(unregisterMock).toHaveBeenCalled();
    expect(keysMock).toHaveBeenCalled();
    expect(deleteMock).toHaveBeenCalledWith("basement");
    expect(reloadMock).toHaveBeenCalled();

    Object.defineProperty(window, "location", {
      value: { reload: originalLocationReload },
      writable: true,
    });
  });

  it("reloads even if SW APIs throw", async () => {
    const callback = subscribeMock.mock.calls[0]?.[0];
    if (!callback) return;
    
    callback({ mismatched: true, serverVersion: "v1.11.0.29" });
    
    const button = screen.getByRole("button", { name: /Refresh/i });
    expect(button).toBeInTheDocument();

    (navigator.serviceWorker as any) = {
      getRegistrations: vi.fn().mockRejectedValue(new Error("SW error")),
    };

    (globalThis.caches as any) = {
      keys: vi.fn().mockRejectedValue(new Error("Cache error")),
    };

    const originalLocationReload = window.location.reload;
    const reloadMock = vi.fn();
    Object.defineProperty(window, "location", {
      value: { reload: reloadMock },
      writable: true,
    });

    await button.click();

    expect(reloadMock).toHaveBeenCalled();

    Object.defineProperty(window, "location", {
      value: { reload: originalLocationReload },
      writable: true,
    });
  });

  afterEach(() => {
    subscribeMock.mockClear();
    vi.clearAllMocks();
  });
});
