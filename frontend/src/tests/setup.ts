import "@testing-library/jest-dom";

// Node 26 + Vitest 4 + jsdom 29: neither the Node experimental
// localStorage (gated on --localstorage-file) nor jsdom's
// window.localStorage is reliably exposed as the bare `localStorage`
// global, and even `window.localStorage` is undefined in non-DOM test
// files like src/shared/i18n/__tests__/i18n.test.ts. Shim a minimal
// Storage on globalThis + window so callers can use either reference
// without an env probe. Added v2.0.0-beta.3.1 alongside the i18n
// scaffolding cycle.
if (typeof (globalThis as { localStorage?: Storage }).localStorage === "undefined") {
  const store: Record<string, string> = {};
  const shim: Storage = {
    get length() {
      return Object.keys(store).length;
    },
    clear() {
      for (const k of Object.keys(store)) delete store[k];
    },
    getItem(key) {
      return Object.prototype.hasOwnProperty.call(store, key) ? store[key] ?? null : null;
    },
    key(index) {
      return Object.keys(store)[index] ?? null;
    },
    removeItem(key) {
      delete store[key];
    },
    setItem(key, value) {
      store[key] = String(value);
    },
  };
  (globalThis as { localStorage?: Storage }).localStorage = shim;
  if (typeof window !== "undefined") {
    Object.defineProperty(window, "localStorage", { value: shim, configurable: true });
  }
}

// jsdom doesn't ship ResizeObserver; @tanstack/react-virtual (v1.4.0a)
// needs it to measure the scroll element. The mock is intentionally
// inert — initialRect on the virtualizer call provides the viewport,
// and our tests don't exercise resize events.
if (typeof globalThis.ResizeObserver === "undefined") {
  class MockResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).ResizeObserver = MockResizeObserver;
}

// jsdom's getBoundingClientRect returns 0×0; the virtualizer relies
// on it to size the scroll element. Patch a sensible default so the
// virtualizer renders items in unit tests.
if (typeof Element !== "undefined") {
  const origGetRect = Element.prototype.getBoundingClientRect;
  Element.prototype.getBoundingClientRect = function () {
    const rect = origGetRect.call(this);
    if (rect.width === 0 && rect.height === 0) {
      return {
        x: 0,
        y: 0,
        top: 0,
        left: 0,
        right: 800,
        bottom: 600,
        width: 800,
        height: 600,
        toJSON: () => ({}),
      } as DOMRect;
    }
    return rect;
  };
}
