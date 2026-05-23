import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { subscribe, __stopHeartbeatForTests, getBuildCommit, observeResponse } from "@/shared/api/buildWatcher";

const originalBuildCommit = (globalThis as any).__BUILD_COMMIT__;

describe("buildWatcher", () => {
  beforeEach(() => {
    vi.resetModules();
    (globalThis as any).__BUILD_COMMIT__ = "test-build-123";
    vi.clearAllMocks();
  });

  afterEach(() => {
    __stopHeartbeatForTests();
    (globalThis as any).__BUILD_COMMIT__ = originalBuildCommit;
    vi.restoreAllMocks();
    vi.clearAllMocks();
  });

  it("heartbeat starts on subscribe", () => {
    expect.assertions(1);
    const mockFn = vi.fn();
    
    subscribe(mockFn);
    
    expect(mockFn).toHaveBeenCalledWith(false);
  });

  it("heartbeat stops when last subscriber unsubscribes", async () => {
    const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
    
    const unsub1 = subscribe(() => {});
    const unsub2 = subscribe(() => {});
    
    expect(clearIntervalSpy).not.toHaveBeenCalled();
    
    unsub1();
    expect(clearIntervalSpy).not.toHaveBeenCalled();
    
    unsub2();
    await new Promise((resolve) => setTimeout(resolve, 10));
    
    expect(clearIntervalSpy).toHaveBeenCalled();
    
    clearIntervalSpy.mockRestore();
  });

  it("heartbeat stops once mismatch detected", async () => {
    (globalThis as any).__BUILD_COMMIT__ = "new-build-456";
    
    const mockFetch = vi.fn().mockResolvedValueOnce(
      new Response(JSON.stringify({ version: "v1.0.0", commit: "new-build-456" }), {
        headers: { "X-Build": "different-build" },
      })
    );
    
    Object.defineProperty(globalThis, "fetch", {
      value: mockFetch,
      writable: true,
      configurable: true,
    });
    
    const mismatchCallback = vi.fn();
    subscribe(mismatchCallback);
    
    await new Promise((resolve) => setTimeout(resolve, 70));
    
    expect(mismatchCallback).toHaveBeenCalledWith(true);
    await new Promise((resolve) => setTimeout(resolve, 65));
    
    expect(mockFetch).toHaveBeenCalledTimes(1);
    
    mockFetch.mockRestore();
  });

  it("fetch failure doesn't break the polling loop", async () => {
    (globalThis as any).__BUILD_COMMIT__ = "dev";
    
    const mockFetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));
    
    Object.defineProperty(globalThis, "fetch", {
      value: mockFetch,
      writable: true,
      configurable: true,
    });
    
    const mockFn = vi.fn();
    const unsub = subscribe(mockFn);
    
    await new Promise((resolve) => setTimeout(resolve, 70));
    
    unsub();
    
    expect(() => unsub()).not.toThrow();
    
    mockFetch.mockRestore();
  });

  it("observeResponse triggers mismatch on X-Build header", () => {
    const mockFn = vi.fn();
    subscribe(mockFn);
    
    const fakeResponse = new Response(null, {
      headers: { "X-Build": "newer-build" },
    });
    
    observeResponse(fakeResponse);
    
    expect(mockFn).toHaveBeenCalledWith(true);
  });

  it("subscribe returns cleanup function that removes listener", () => {
    const mockFn = vi.fn();
    const unsub = subscribe(mockFn);
    
    expect(mockFn).toHaveBeenCalledTimes(1);
    
    unsub();
    
    const mockFn2 = vi.fn();
    subscribe(mockFn2);
    
    expect(mockFn2).not.toHaveBeenCalledWith(true);
  });

  it("getBuildCommit returns __BUILD_COMMIT__", () => {
    expect(getBuildCommit()).toBe("test-build-123");
  });
});
