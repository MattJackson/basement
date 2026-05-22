// v1.8.0e — install-to-home-screen hint banner.
//
// Verifies the three gates that decide whether the banner shows:
//   1. localStorage flag must not be set
//   2. display-mode must be browser (not standalone)
//   3. viewport must be narrow (mobile)
// Plus the dismiss action persists the flag so re-rendering hides it.

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  InstallToHomeScreenHint,
  detectShouldShowHint,
} from "@/shared/ui/InstallToHomeScreenHint";

type MatchMediaCases = {
  displayModeBrowser?: boolean;
  maxWidth767?: boolean;
};

function stubMatchMedia(cases: MatchMediaCases) {
  const original = window.matchMedia;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (window as any).matchMedia = vi.fn().mockImplementation((query: string) => {
    let matches = false;
    if (query.includes("display-mode: browser")) {
      matches = !!cases.displayModeBrowser;
    } else if (query.includes("max-width: 767px")) {
      matches = !!cases.maxWidth767;
    }
    return {
      matches,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    };
  });
  return () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).matchMedia = original;
  };
}

let teardown: (() => void) | null = null;

// jsdom in vitest 4.x doesn't ship localStorage by default. Provide a
// minimal in-memory shim so the dismiss persistence path is testable.
// Returning null when the key is missing matches the spec.
function ensureLocalStorage() {
  if (typeof window.localStorage === "undefined" || window.localStorage === null) {
    const store = new Map<string, string>();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).localStorage = {
      getItem: (k: string) => (store.has(k) ? (store.get(k) as string) : null),
      setItem: (k: string, v: string) => {
        store.set(k, v);
      },
      removeItem: (k: string) => {
        store.delete(k);
      },
      clear: () => store.clear(),
      key: (i: number) => Array.from(store.keys())[i] ?? null,
      get length() {
        return store.size;
      },
    };
  }
}

beforeEach(() => {
  ensureLocalStorage();
  window.localStorage.clear();
});

afterEach(() => {
  if (teardown) {
    teardown();
    teardown = null;
  }
});

describe("detectShouldShowHint", () => {
  it("returns true when mobile + browser-mode + not dismissed", () => {
    teardown = stubMatchMedia({ displayModeBrowser: true, maxWidth767: true });
    expect(detectShouldShowHint()).toBe(true);
  });

  it("returns false when already installed (standalone display mode)", () => {
    teardown = stubMatchMedia({ displayModeBrowser: false, maxWidth767: true });
    expect(detectShouldShowHint()).toBe(false);
  });

  it("returns false on desktop viewport", () => {
    teardown = stubMatchMedia({ displayModeBrowser: true, maxWidth767: false });
    expect(detectShouldShowHint()).toBe(false);
  });

  it("returns false once the user has dismissed the hint", () => {
    teardown = stubMatchMedia({ displayModeBrowser: true, maxWidth767: true });
    window.localStorage.setItem("basement.pwaHintDismissed", "1");
    expect(detectShouldShowHint()).toBe(false);
  });
});

describe("<InstallToHomeScreenHint />", () => {
  it("renders the hint when conditions match", () => {
    teardown = stubMatchMedia({ displayModeBrowser: true, maxWidth767: true });
    render(<InstallToHomeScreenHint />);
    expect(screen.getByTestId("install-to-home-screen-hint")).toBeInTheDocument();
    expect(screen.getByText(/Add to Home Screen/)).toBeInTheDocument();
  });

  it("hides immediately and persists when dismissed", async () => {
    teardown = stubMatchMedia({ displayModeBrowser: true, maxWidth767: true });
    render(<InstallToHomeScreenHint />);
    await userEvent.click(screen.getByTestId("install-hint-dismiss"));
    expect(screen.queryByTestId("install-to-home-screen-hint")).not.toBeInTheDocument();
    expect(window.localStorage.getItem("basement.pwaHintDismissed")).toBe("1");
  });

  it("does not render on desktop", () => {
    teardown = stubMatchMedia({ displayModeBrowser: true, maxWidth767: false });
    render(<InstallToHomeScreenHint />);
    expect(
      screen.queryByTestId("install-to-home-screen-hint"),
    ).not.toBeInTheDocument();
  });
});
