// v1.11.0.27 — Heartbeat polling for NewVersionBanner.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

describe("buildWatcher", () => {
  let clearIntervalSpy: any;

  beforeEach(async () => {
    vi.resetModules();
    clearIntervalSpy = vi.spyOn(globalThis, "clearInterval").mockImplementation(() => {});
  });

  it("heartbeat starts on subscribe", async () => {
    const { subscribe, __stopHeartbeatForTests } = await import("@/shared/api/buildWatcher");
    
    const mockFn = vi.fn();
    subscribe(mockFn);
    
    expect(mockFn).toHaveBeenCalledWith({ mismatched: false });
    
    __stopHeartbeatForTests();
  });

  it("heartbeat stops when last subscriber unsubscribes", async () => {
    const { subscribe, __stopHeartbeatForTests } = await import("@/shared/api/buildWatcher");
    
    const unsub1 = subscribe(() => {});
    const unsub2 = subscribe(() => {});
    
    expect(clearIntervalSpy).not.toHaveBeenCalled();
    
    unsub1();
    expect(clearIntervalSpy).not.toHaveBeenCalled();
    
    unsub2();
    await new Promise((resolve) => setTimeout(resolve, 10));
    
    expect(clearIntervalSpy).toHaveBeenCalled();
    
    __stopHeartbeatForTests();
  });

  it("subscribe receives serverVersion from X-Version header", async () => {
    const module = await import("@/shared/api/buildWatcher");
    
    const mockFn = vi.fn();
    module.subscribe(mockFn);
    
    expect(mockFn).toHaveBeenCalledWith({ mismatched: false });
    
    // Simulate observeResponse with X-Version header
    const fakeResponse = new Response(null, {
      headers: { "X-Version": "v1.11.0.28" },
    });
    
    module.observeResponse(fakeResponse);
    
    expect(mockFn).toHaveBeenNthCalledWith(2, { mismatched: false, serverVersion: "v1.11.0.28" });
  });

  it("observeResponse triggers mismatch on X-Build header", async () => {
    const { subscribe, observeResponse, __stopHeartbeatForTests } = await import("@/shared/api/buildWatcher");
    
    const mockFn = vi.fn();
    subscribe(mockFn);
    
    const fakeResponse = new Response(null, {
      headers: { "X-Build": "newer-build" },
    });
    
    observeResponse(fakeResponse);
    
    expect(mockFn).toHaveBeenCalledWith({ mismatched: true, serverVersion: undefined });
    
    __stopHeartbeatForTests();
  });

  it("subscribe returns cleanup function that removes listener", async () => {
    const { subscribe, __stopHeartbeatForTests } = await import("@/shared/api/buildWatcher");
    
    const mockFn = vi.fn();
    const unsub = subscribe(mockFn);
    
    expect(mockFn).toHaveBeenCalledTimes(1);
    
    unsub();
    
    const mockFn2 = vi.fn();
    subscribe(mockFn2);
    
    expect(mockFn2).not.toHaveBeenCalledWith({ mismatched: true });
    
    __stopHeartbeatForTests();
  });

  it("fetch failure doesn't break the polling loop", async () => {
    const { subscribe, __stopHeartbeatForTests } = await import("@/shared/api/buildWatcher");
    
    const mockFetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));
    
    Object.defineProperty(globalThis, "fetch", {
      value: mockFetch,
      writable: true,
      configurable: true,
    });
    
    const mockFn = vi.fn();
    const unsub = subscribe(mockFn);
    
    await new Promise((resolve) => setTimeout(resolve, 70));
    
    expect(() => unsub()).not.toThrow();
    
    __stopHeartbeatForTests();
    mockFetch.mockRestore();
  });

  afterEach(() => {
    clearIntervalSpy.mockRestore();
    vi.clearAllMocks();
  });
});
