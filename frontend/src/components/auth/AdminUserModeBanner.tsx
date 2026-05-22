// ADR-0003 / v1.7.0a.1 amendment — persistent fallback banner for the
// admin-URL-in-user-mode mismatch.
//
// The v1.7.0a.1 auto-elevate flow opens the elevation modal whenever a
// user lands on /admin/* in USER mode. If the operator dismisses that
// modal — or if for any reason the auto-prompt was skipped (race on
// hydration, future routing surprise) — we still need a visible cue
// that admin actions on this page will 403. This banner provides that
// fallback: a sticky amber bar with one click to elevate and one click
// to bail out of the admin context.
//
// Visibility rules:
//   - Renders only on /admin/* AND mode === "user".
//   - Disappears the moment mode flips to admin (rising edge handled
//     by the underlying AuthModeProvider state — the banner just reads
//     the current value).
//   - No-op on /files/*, /login, etc.
//
// This is distinct from ElevationExpiredBanner (v1.3.0a.4) which only
// surfaces on a falling-edge admin→user transition within a session.
// AdminUserModeBanner shows for any USER on /admin/* — including the
// fresh-direct-nav case the ExpiredBanner intentionally ignores.

import { useLocation } from "@tanstack/react-router";
import { useNavigate } from "@tanstack/react-router";
import { useAuthMode } from "@/shared/auth/mode";
import { useElevationPrompt } from "@/shared/auth/elevation";

export function AdminUserModeBanner() {
  const { mode } = useAuthMode();
  const location = useLocation();
  const navigate = useNavigate();
  const promptForElevation = useElevationPrompt();

  const onAdmin = location.pathname.startsWith("/admin");
  if (!onAdmin || mode !== "user") {
    return null;
  }

  const handleElevate = async () => {
    try {
      await promptForElevation(
        "admin",
        "Admin actions require admin re-authentication.",
      );
      // On success, mode flips via AuthModeProvider + the banner self-
      // hides on the next render. No explicit action needed here.
    } catch {
      // ELEVATION_CANCELLED — leave the banner up so the operator can
      // try again, or click the Drop button to bail.
    }
  };

  const handleDrop = () => {
    void navigate({ to: "/files" });
  };

  return (
    <div
      data-testid="admin-user-mode-banner"
      role="status"
      className="border-b border-amber-300 bg-amber-100 text-amber-950 dark:border-amber-700 dark:bg-amber-500/15 dark:text-amber-100"
    >
      <div className="max-w-[1280px] mx-auto px-4 sm:px-6 lg:px-8 py-2 flex items-center justify-between gap-3">
        <p className="text-sm">
          You're in user mode.{" "}
          <span className="text-amber-900/80 dark:text-amber-100/80">
            Admin actions need elevation.
          </span>
        </p>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={handleElevate}
            data-testid="admin-user-mode-elevate"
            className="inline-flex items-center rounded-md bg-amber-500 px-3 py-1.5 text-xs font-semibold text-amber-50 shadow-sm hover:bg-amber-600 focus:outline-none focus:ring-2 focus:ring-amber-400"
          >
            Elevate to admin
          </button>
          <button
            type="button"
            onClick={handleDrop}
            data-testid="admin-user-mode-drop"
            className="inline-flex items-center rounded-md border border-amber-400 bg-transparent px-3 py-1.5 text-xs font-semibold text-amber-900 hover:bg-amber-200/60 focus:outline-none focus:ring-2 focus:ring-amber-400 dark:text-amber-100 dark:hover:bg-amber-500/20"
          >
            Drop to /files
          </button>
        </div>
      </div>
    </div>
  );
}
