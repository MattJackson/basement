// v1.10.0.2 — ThemeToggle tap-target test. Pre-fix the toggle
// rendered at the Button `size="icon"` default (size-8 → 32px), below
// the WCAG/iOS HIG 44×44 mobile tap-target threshold flagged in the
// v1.10.0.1 smoke audit. Class-based assertion (rather than computed
// size) because jsdom does not run Tailwind's stylesheet.

import { describe, it, expect, beforeEach, vi } from "vitest";
import { render, screen } from "@testing-library/react";

import { ThemeToggle } from "@/shared/theme/ThemeToggle";

beforeEach(() => {
  Object.defineProperty(globalThis.window, "matchMedia", {
    writable: true,
    value: vi.fn().mockImplementation((query) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
});

describe("ThemeToggle tap-target (v1.10.0.2)", () => {
  it("carries the 44px min-height + min-width tap-target utilities on mobile", () => {
    render(<ThemeToggle />);
    const btn = screen.getByRole("button", { name: /Theme:/i });
    expect(btn.className).toMatch(/min-h-\[44px\]/);
    expect(btn.className).toMatch(/min-w-\[44px\]/);
  });
});
