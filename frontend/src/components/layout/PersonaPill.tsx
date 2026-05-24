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
// v1.9.0e.2 — tight mode/view coupling reverts the v1.9.0e.1 mode-vs-
// view experiment. The pill is the source of truth for MODE only; the
// URL surface no longer factors into the visual variant. With the
// AppShell redirect blocking USER on /admin/* and admin allowed to
// dip into /files/*, the operator's "what mode am I in" question is
// answered by the pill alone, independent of URL.
//
// Countdown re-renders once per second. At expiry the AuthModeProvider's
// auto-downgrade kicks in independently — and AppShell's redirect
// effect navigates the operator to /files when the falling-edge fires
// while they're on /admin/*.
//
// "Drop privileges" hits POST /api/v1/auth/logout-elevation, then
// pushes mode=user into the provider so the pill flips instantly
// without waiting for a /auth/me refetch. v1.7.0a.2: also invalidate
// the cached ["auth","me"] query so AuthModeHydrator's next sync
// reads the freshly-rotated cookie (mode=user) and doesn't snap the
// pill back to ADMIN from a stale React Query cache. v1.9.0e.2:
// also navigate to /files — under the tight coupling, drop = go user.

import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useAuthMode, useSetAuthMode } from "@/shared/auth/mode";
import { useElevationPrompt } from "@/shared/auth/elevation";
import { useUser } from "@/shared/auth/useUser";

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

// Module-level guard to prevent duplicate "Admin session ended" toasts
// (v1.13.14: keyed on a time-window, not expiresAt — the two PersonaPill
// mounts in AppShell + UserShell can see slightly different expiresAt
// during the admin→user transition because auth-mode propagation is
// async, so keying on expiresAt let both fire.)
// when multiple PersonaPill instances exist (AppShell + UserShell).
// Only fires once per admin→user transition, keyed on expiresAt.
// v1.13.22: keep ONE "Admin session ended" toast per admin→user
// transition, regardless of trigger (user-initiated drop OR natural
// expiry). Operator wants the notification, just not 3×.
//
// The two PersonaPill mounts (AppShell + UserShell) both run this
// effect on the same render commit. JS single-thread + module-level
// flag is atomic: first effect run sets the sentinel + fires; second
// run sees the sentinel matches the current transition + skips.
// Rising-edge (user→admin) resets the sentinel so the next drop will
// fire fresh.
let sessionEndedHandledFor: number | null = null;

export function PersonaPill() {
  const { mode, expiresAt } = useAuthMode();
  const setAuthMode = useSetAuthMode();
  const promptForElevation = useElevationPrompt();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const { data: user } = useUser();

  const username = user?.username ?? "";
  const activeRole = user?.activeRole;
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
      sessionEndedHandledFor = null;
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setFlashing(false);
    }
    if (!elevated && wasElevated) {
      // expiresAt is captured at the moment of the falling edge — a
      // stable sentinel that both PersonaPill mounts agree on within
      // the same render commit. First mount fires + claims it; second
      // mount sees it's already claimed + skips.
      if (sessionEndedHandledFor !== expiresAt) {
        sessionEndedHandledFor = expiresAt;
        toast.info("Admin session ended");
        warnedRef.current = "ended";
      }
    }
    prevModeRef.current = mode;
  }, [mode, expiresAt]);

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
      // 3. v1.9.0e.2: drop = go user. Mode change drives navigation
      //    under the tight coupling, so the operator always ends up
      //    on /files after dropping privileges (even if they were
      //    mid-flow on /admin/clusters). Replaces the v1.7.0a-era
      //    behaviour where the URL stayed put and the AdminUserMode
      //    banner sat over the page asking the operator to elevate
      //    they just dropped from.
      void navigate({ to: "/files" });
      toast.success("Privileges dropped");
    } catch {
      toast.error("Failed to drop privileges — network error");
    } finally {
      setDropping(false);
    }
  };

  const pillContent = useMemo(() => {
    if (!activeRole) {
      // No active role set yet — fall back to mode-based display
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
    }

    // v1.13.18: Display active role dynamically
    const roleLabel = activeRole.kind === "cluster-admin" && activeRole.cluster
      ? `Cluster Admin: ${activeRole.cluster}`
      : activeRole.kind === "ui-admin"
        ? "UI Admin"
        : "User";

    // For UI Admin with elevated mode, show countdown + admin-session-TTL hint
    const isElevated = mode === "admin" || mode === "elevated";
    const classes = inRedWindow
      ? "bg-red-300 text-red-950 dark:bg-red-500/40 dark:text-red-50 animate-pulse"
      : inAmberWindow && isElevated
        ? "bg-amber-300 text-amber-950 dark:bg-amber-500/30 dark:text-amber-100"
        : inAmberWindow
          ? "bg-blue-300 text-blue-950 dark:bg-blue-500/30 dark:text-blue-100"
          : isElevated
            ? "bg-amber-200 text-amber-900 dark:bg-amber-500/20 dark:text-amber-200"
            : "bg-muted px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground";

    return (
      <span
        className={
          "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider transition-colors " +
          classes
        }
        data-testid="persona-pill"
        data-mode={activeRole.kind}
        data-role={roleLabel.toLowerCase().replace(/:/g, "")}
      >
        {username && <span className="mr-1 font-medium">{username}</span>}
        <span>{roleLabel}</span>
        {isElevated && activeRole.kind === "ui-admin" && (
          <span className="ml-2 text-xs opacity-75">(elevated)</span>
        )}
      </span>
    );
  }, [mode, flashing, inAmberWindow, inRedWindow, activeRole, username]);

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
