// ADR-0003 + v1.3.0a.4 amendment — persona pill with live mode countdown.
//
// Two states (per the amendment that collapsed ELEVATED into ADMIN):
//
//   USER  → neutral gray pill, no countdown, no drop button.
//   ADMIN → amber pill + countdown chip (mm:ss left) + drop button.
//           At <2 min: pill goes brighter amber as a heads-up.
//           At <30s: pill flips red + flashes + a toast offers a
//                    one-click extend ("Stay admin") that just calls
//                    /auth/elevate again with the same target_mode.
//
// Countdown re-renders once per second. At expiry the AuthModeProvider's
// auto-downgrade kicks in independently — and AuthModeHydrator surfaces
// a drop-in-place banner at /admin/* rather than yanking the user.
//
// "Drop privileges" hits POST /api/v1/auth/logout-elevation, then
// pushes mode=user into the provider so the pill flips instantly
// without waiting for a /auth/me refetch. v1.7.0a.2: also invalidate
// the cached ["auth","me"] query so AuthModeHydrator's next sync
// reads the freshly-rotated cookie (mode=user) and doesn't snap the
// pill back to ADMIN from a stale React Query cache.

import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useAuthMode, useSetAuthMode } from "@/shared/auth/mode";
import { useElevationPrompt } from "@/shared/auth/elevation";

/** formatRemaining renders a countdown as h:mm:ss when ≥1h, otherwise mm:ss. */
export function formatRemaining(msRemaining: number): string {
  const clamped = Math.max(0, Math.floor(msRemaining / 1000));
  const h = Math.floor(clamped / 3600);
  const m = Math.floor((clamped % 3600) / 60);
  const s = clamped % 60;
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  }
  return `${m}:${s.toString().padStart(2, "0")}`;
}

/** Cross icon for the "Drop privileges" button. */
function CrossIcon({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 20 20"
      fill="currentColor"
      className={className ?? "h-3 w-3"}
      aria-hidden="true"
    >
      <path d="M6.28 5.22a.75.75 0 00-1.06 1.06L8.94 10l-3.72 3.72a.75.75 0 101.06 1.06L10 11.06l3.72 3.72a.75.75 0 101.06-1.06L11.06 10l3.72-3.72a.75.75 0 10-1.06-1.06L10 8.94 6.28 5.22z" />
    </svg>
  );
}

// Warning thresholds (ms). The v1.3.0a.4 amendment widened these:
// amber at <2 min (previously <30s) so the operator has actual lead
// time; red + flashing + toast at <30s.
const WARN_AMBER_MS = 2 * 60 * 1000;
const WARN_RED_MS = 30 * 1000;

export function PersonaPill() {
  const { mode, expiresAt } = useAuthMode();
  const setAuthMode = useSetAuthMode();
  const promptForElevation = useElevationPrompt();
  const queryClient = useQueryClient();
  const [now, setNow] = useState<number>(() => Date.now());
  const [flashing, setFlashing] = useState(false);
  // warnedRef tracks the latest expiresAt for which we've fired the
  // 30s toast — keyed on expiresAt so a re-elevation (= new expiresAt)
  // re-arms the warning. The "ended" sentinel is set on the falling
  // edge (admin → user) so subsequent ticks don't re-fire the ended
  // toast. Both consumers only compare for inequality so the union
  // type is safe.
  const warnedRef = useRef<number | "ended" | null>(null);
  const [dropping, setDropping] = useState(false);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  // Reset the warning latch only on RISING-edge mode transitions
  // (user → admin, i.e. a fresh elevation). Falling-edge fires the
  // ended toast once and leaves the latch set so user-mode ticks
  // don't re-fire it.
  const prevModeRef = useRef<string>(mode);
  useEffect(() => {
    const elevated = mode === "admin" || mode === "elevated";
    const wasElevated =
      prevModeRef.current === "admin" || prevModeRef.current === "elevated";
    if (elevated && !wasElevated) {
      warnedRef.current = null;
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setFlashing(false);
    }
    if (!elevated && wasElevated) {
      toast.info("Admin session ended");
      warnedRef.current = "ended";
    }
    prevModeRef.current = mode;
  }, [mode]);

  const remainingMs = expiresAt > 0 ? expiresAt - now : 0;
  const elevated = mode === "admin" || mode === "elevated";
  const showCountdown = expiresAt > 0 && elevated;
  const inAmberWindow = showCountdown && remainingMs > 0 && remainingMs <= WARN_AMBER_MS;
  const inRedWindow = showCountdown && remainingMs > 0 && remainingMs <= WARN_RED_MS;

  // <30s warning + flash + extend toast. Fires once per elevation
  // window. The toast carries an inline action that triggers a
  // re-elevation via the global prompt — no extra hops, the modal
  // opens directly.
  useEffect(() => {
    if (!showCountdown) return;
    if (!inRedWindow) return;
    if (warnedRef.current === expiresAt) return;
    warnedRef.current = expiresAt;
    setFlashing(true);
    toast.warning("Admin session expires in 30s", {
      duration: 25000,
      action: {
        label: "Stay admin",
        onClick: () => {
          // Re-elevate to admin; same target_mode the original
          // elevation used. The modal opens, password (or SSO) goes
          // in, the cookie is reissued with a fresh expiresAt and the
          // countdown picks up the new value via /auth/me.
          promptForElevation("admin", "Extend your admin session.").catch(
            () => {
              // user cancelled — nothing to do; the original toast
              // already gave them the warning and the existing
              // countdown will run to zero on its own.
            },
          );
        },
      },
    });
  }, [showCountdown, inRedWindow, expiresAt, promptForElevation]);

  const handleDrop = async () => {
    if (dropping) return;
    setDropping(true);
    try {
      const res = await fetch("/api/v1/auth/logout-elevation", {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) {
        toast.error(`Failed to drop privileges (HTTP ${res.status})`);
        return;
      }
      // 1. Snap local state to USER so the pill flips instantly.
      setAuthMode({ mode: "user", expiresAt: 0 });
      // 2. Invalidate the /auth/me cache so AuthModeHydrator's next
      //    sync sees the just-rotated cookie (mode=user) instead of
      //    the stale ADMIN payload — without this, the hydrator
      //    detects current.mode !== user.mode and snaps the pill
      //    back to ADMIN within one tick.
      queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      toast.success("Privileges dropped");
    } catch {
      toast.error("Failed to drop privileges — network error");
    } finally {
      setDropping(false);
    }
  };

  const pillContent = useMemo(() => {
    if (mode === "user") {
      return (
        <span
          className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground"
          data-testid="persona-pill"
          data-mode="user"
        >
          USER
        </span>
      );
    }
    // admin (incl. legacy "elevated" alias)
    const classes = inRedWindow
      ? "bg-red-300 text-red-950 dark:bg-red-500/40 dark:text-red-50 animate-pulse"
      : inAmberWindow
        ? "bg-amber-300 text-amber-950 dark:bg-amber-500/30 dark:text-amber-100"
        : "bg-amber-200 text-amber-900 dark:bg-amber-500/20 dark:text-amber-200";
    return (
      <span
        className={
          "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider transition-colors " +
          classes +
          (flashing && inRedWindow ? " ring-2 ring-red-400" : "")
        }
        data-testid="persona-pill"
        data-mode="admin"
        data-warn={inRedWindow ? "red" : inAmberWindow ? "amber" : "none"}
      >
        ADMIN
      </span>
    );
  }, [mode, flashing, inAmberWindow, inRedWindow]);

  return (
    <div className="flex items-center gap-1.5">
      {pillContent}
      {showCountdown && (
        <>
          <span
            className={
              "inline-flex items-center rounded-md px-1.5 py-0.5 font-mono text-[10px] tabular-nums " +
              (inRedWindow
                ? "bg-red-100 text-red-900 dark:bg-red-500/20 dark:text-red-100"
                : inAmberWindow
                  ? "bg-amber-100 text-amber-900 dark:bg-amber-500/20 dark:text-amber-100"
                  : "bg-muted/60 text-muted-foreground")
            }
            data-testid="persona-countdown"
            aria-label={`Admin session ending in ${formatRemaining(remainingMs)}`}
          >
            {formatRemaining(remainingMs)} left
          </span>
          <button
            type="button"
            onClick={handleDrop}
            disabled={dropping}
            title="Drop privileges back to USER"
            aria-label="Drop privileges"
            // v1.8.0e: tap target hits 44px on touch devices via the
            // sm-and-down sizing; desktop stays at the compact 20px
            // chip so the persona pill cluster doesn't blow out the
            // header height. The icon stays 12px in both cases so the
            // visual chrome matches.
            className="inline-flex h-11 w-11 sm:h-5 sm:w-5 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
            data-testid="persona-drop"
          >
            <CrossIcon />
          </button>
        </>
      )}
    </div>
  );
}
