// IMPORTANT: localStorage shim MUST run before @/shared/i18n imports.
// JS hoists imports above statement-level code, so the shim has to
// live in its own module that's imported first — otherwise the
// LanguageDetector captures undefined window.localStorage at i18n
// init time and never sees the shim. v1.13.32 fix.
import "./shims-localstorage";

import "@testing-library/jest-dom";

// Initialize i18n once for all tests so any component that calls
// useTranslation() gets a resolved t() on first render — regardless
// of whether the test file imports @/shared/i18n directly. Must be
// imported AFTER ./shims-localstorage (above) so the detector sees
// a valid localStorage at init.
import "@/shared/i18n";

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
