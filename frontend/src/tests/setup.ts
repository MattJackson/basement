import "@testing-library/jest-dom";

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
