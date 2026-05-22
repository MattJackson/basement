// ADR-0003 / v1.3.0a.4 amendment — drop-in-place expiry banner.
//
// When a session in ADMIN mode expires, the v1.2 behaviour was to
// auto-flip to USER and let the next admin action 403. The amendment
// keeps that flip but ADDS this banner so the operator isn't yanked
// out of context — they finish reading, save the form they were
// editing, and click "Re-elevate" when they're ready.
//
// Rendered at the top of every /admin/* shell. Visibility rules:
//
//   - Show ONLY when:
//       * the current route starts with `/admin`, AND
//       * the auth mode is USER, AND
//       * we just transitioned from ADMIN within this session (i.e.
//         the operator was admin a moment ago — a fresh login that
//         starts in USER doesn't see the banner because they never
//         had an admin session to lose).
//   - Hide when:
//       * the user successfully re-elevates (mode flips to admin), OR
//       * the user navigates away from /admin/* (e.g. clicks "Files"
//         in the nav).
//
// "Mid-form" protection: if any input within the page is dirty the
// banner still shows (the operator needs to see why their next save
// will 403) but the page DOES NOT navigate or unmount — that's the
// whole point of the drop-in-place pattern. Form state lives inside
// the page component which remains mounted as long as the route key
// hasn't changed; the banner is rendered above it, not in place of it.

import { useEffect, useRef, useState } from "react";
import { useLocation } from "@tanstack/react-router";
import { useAuthMode } from "@/shared/auth/mode";
import { useElevationPrompt } from "@/shared/auth/elevation";

export function ElevationExpiredBanner() {
  const { mode } = useAuthMode();
  const location = useLocation();
  const promptForElevation = useElevationPrompt();

  // sawElevatedRef latches once the operator has been in admin/elevated
  // mode during this app lifecycle. Without it, a user who lands on
  // /admin/* directly (e.g. deep-link to /admin/clusters while still in
  // USER mode) would see the banner immediately, which is wrong —
  // they should see the normal elevation prompt the route-guard kicks
  // off, not an "expired" message for a session they never held.
  const sawElevatedRef = useRef<boolean>(false);
  const [expiredVisible, setExpiredVisible] = useState<boolean>(false);

  // Track mode rising + falling edges.
  useEffect(() => {
    if (mode === "admin" || mode === "elevated") {
      sawElevatedRef.current = true;
      // Re-elevation clears any prior expired flag.
      setExpiredVisible(false);
      return;
    }
    // mode === "user". If we'd been admin previously, that means the
    // expiry just fired — surface the banner.
    if (sawElevatedRef.current) {
      setExpiredVisible(true);
    }
  }, [mode]);

  // Navigating away from /admin/* dismisses the banner (the operator
  // chose to leave the admin context). Re-entering /admin/* later
  // would surface the prompt again via the normal entry guard, so we
  // don't need the banner there.
  const onAdmin = location.pathname.startsWith("/admin");
  useEffect(() => {
    if (!onAdmin && expiredVisible) {
      setExpiredVisible(false);
    }
  }, [onAdmin, expiredVisible]);

  if (!expiredVisible || !onAdmin || mode !== "user") {
    return null;
  }

  const handleReElevate = async () => {
    try {
      await promptForElevation(
        "admin",
        "Your admin session expired. Re-enter your password to continue.",
      );
      // On success the AuthModeHydrator + the rising-edge effect above
      // both clear expiredVisible. No explicit action needed here.
    } catch {
      // User cancelled the modal. Leave the banner up so they can try
      // again — or navigate away.
    }
  };

  return (
    <div
      data-testid="elevation-expired-banner"
      role="status"
      className="border-b border-amber-300 bg-amber-100 text-amber-950 dark:border-amber-700 dark:bg-amber-500/15 dark:text-amber-100"
    >
      <div className="max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center justify-between gap-3">
        <p className="text-sm">
          Your admin session expired.{" "}
          <span className="text-amber-900/80 dark:text-amber-100/80">
            Re-elevate to continue editing — your current page stays as-is.
          </span>
        </p>
        <button
          type="button"
          onClick={handleReElevate}
          data-testid="elevation-expired-reelevate"
          className="inline-flex items-center rounded-md bg-amber-500 px-3 py-1.5 text-xs font-semibold text-amber-50 shadow-sm hover:bg-amber-600 focus:outline-none focus:ring-2 focus:ring-amber-400"
        >
          Re-elevate
        </button>
      </div>
    </div>
  );
}
