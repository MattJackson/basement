// v1.8.0e — install-to-home-screen hint banner.
//
// Renders on /files for mobile users still in browser-mode (i.e. not
// yet installed as a PWA). Safari iOS does not auto-prompt; the
// operator must tap Share → "Add to Home Screen" manually. Chrome /
// Edge on Android auto-prompt via the beforeinstallprompt event, but
// we still surface the same banner there for consistency and because
// auto-prompt timing is browser-controlled.
//
// One-time dismissible: stored as `basement.pwaHintDismissed=1` in
// localStorage. Once dismissed it never re-shows for that device until
// the operator manually clears storage. We deliberately key on the
// host-and-not-user so a USER → ADMIN role flip on the same device
// doesn't bring the banner back.
//
// Detection rules:
//   1. localStorage flag not set (one-time)
//   2. window.matchMedia('(display-mode: browser)').matches — i.e. the
//      page is NOT already running standalone (already installed)
//   3. window.matchMedia('(max-width: 767px)').matches — mobile-shaped
//      viewport
// All three must be true. Server-side renders / jsdom tests where
// matchMedia is mocked inert default to false (banner stays hidden).

import { useEffect, useState } from "react";

const DISMISSED_STORAGE_KEY = "basement.pwaHintDismissed";

// detectShouldShowHint runs the three matchMedia gates. Exported so
// the unit test can drive it directly without rendering.
export function detectShouldShowHint(): boolean {
  if (typeof window === "undefined") return false;
  try {
    if (window.localStorage?.getItem(DISMISSED_STORAGE_KEY) === "1") {
      return false;
    }
  } catch {
    // Storage may throw under strict privacy modes; fall through and
    // show the banner — worst case it appears every load, no data lost.
  }
  if (typeof window.matchMedia !== "function") return false;
  const inBrowserMode = window.matchMedia("(display-mode: browser)").matches;
  if (!inBrowserMode) return false;
  const isMobileViewport = window.matchMedia("(max-width: 767px)").matches;
  if (!isMobileViewport) return false;
  return true;
}

export function InstallToHomeScreenHint() {
  const [show, setShow] = useState(false);

  useEffect(() => {
    setShow(detectShouldShowHint());
  }, []);

  const handleDismiss = () => {
    setShow(false);
    try {
      window.localStorage?.setItem(DISMISSED_STORAGE_KEY, "1");
    } catch {
      // Same fallthrough as detectShouldShowHint — banner stays
      // dismissed for this session at minimum.
    }
  };

  if (!show) return null;

  return (
    <div
      className="rounded-lg border border-primary/30 bg-primary/5 p-4 text-sm"
      role="status"
      data-testid="install-to-home-screen-hint"
    >
      <div className="flex items-start gap-3">
        <ShareIcon className="h-5 w-5 text-primary flex-shrink-0 mt-0.5" />
        <div className="flex-1 min-w-0">
          <p className="font-medium">Install Basement on your phone</p>
          <p className="mt-1 text-muted-foreground">
            Tap the share icon in your browser, then choose{" "}
            <strong className="text-foreground">Add to Home Screen</strong> to
            open Basement like a native app.
          </p>
        </div>
        <button
          type="button"
          onClick={handleDismiss}
          aria-label="Dismiss install hint"
          // Touch-target ≥44px per iOS HIG. The X icon itself stays
          // small (16px) but the hit area is the full button.
          className="inline-flex h-11 w-11 -my-1 -mr-1 items-center justify-center rounded-md text-muted-foreground hover:bg-primary/10 hover:text-foreground"
          data-testid="install-hint-dismiss"
        >
          <CloseIcon className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

function ShareIcon({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      <path d="M12 16V4" />
      <path d="m7 9 5-5 5 5" />
      <path d="M21 14v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-6" />
    </svg>
  );
}

function CloseIcon({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      <path d="M18 6 6 18" />
      <path d="m6 6 12 12" />
    </svg>
  );
}
