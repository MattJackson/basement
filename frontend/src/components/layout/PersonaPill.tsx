// ADR-0003 / v1.2.0b — persona pill with live mode countdown.
//
// Renders one of three states next to the user menu in the AppShell:
//
//   USER     → neutral gray pill, no countdown, no drop button.
//   ADMIN    → amber pill + countdown chip (mm:ss left) + drop button.
//   ELEVATED → orange pill + lightning bolt SVG + countdown + drop.
//
// Countdown re-renders once per second. At <30s before expiry the pill
// flashes amber for 2s and a toast warns the operator. At expiry the
// AuthModeProvider's auto-downgrade kicks in independently; here we
// only render the live timer.
//
// "Drop privileges" hits POST /api/v1/auth/logout-elevation, then
// pushes mode=user into the provider so the pill flips instantly
// without waiting for a /auth/me refetch.

import { useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { useAuthMode, useSetAuthMode } from "@/shared/auth/mode";

/** formatRemaining renders a countdown as mm:ss (clamped at 0). */
export function formatRemaining(msRemaining: number): string {
  const clamped = Math.max(0, Math.floor(msRemaining / 1000));
  const m = Math.floor(clamped / 60);
  const s = clamped % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

/** LightningBolt is the ELEVATED indicator — SVG, never an emoji. */
function LightningBolt({ className }: { className?: string }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 24 24"
      fill="currentColor"
      className={className ?? "h-3 w-3"}
      aria-hidden="true"
    >
      <path d="M13 2L4.5 13.5h6L9 22l9.5-12h-6L13 2z" />
    </svg>
  );
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

export function PersonaPill() {
  const { mode, expiresAt } = useAuthMode();
  const setAuthMode = useSetAuthMode();
  const [now, setNow] = useState<number>(() => Date.now());
  const [flashing, setFlashing] = useState(false);
  // warnedRef tracks whether we've already fired the <30s toast for
  // this particular expiry window — without it the 1Hz tick would
  // spam toasts every second from t-29 down to t-0.
  const warnedRef = useRef<number | null>(null);
  const [dropping, setDropping] = useState(false);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  // Reset the warning latch only on RISING-edge mode transitions
  // (user → admin/elevated, i.e. a fresh elevation). The previous
  // implementation reset on every expiresAt change, which the server
  // can perturb on each /auth/me refetch (the downgrade cookie carries
  // a current-time-ish expiresAt) — that re-armed the "ended" toast
  // every tick and produced an indefinite toast loop.
  const prevModeRef = useRef<string>(mode);
  useEffect(() => {
    const elevated = mode === "admin" || mode === "elevated";
    const wasElevated = prevModeRef.current === "admin" || prevModeRef.current === "elevated";
    // Rising edge: just elevated. Reset the warning latch.
    if (elevated && !wasElevated) {
      warnedRef.current = null;
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setFlashing(false);
    }
    // Falling edge: admin/elevated → user. Fire the ended toast once,
    // then leave the latch set so subsequent ticks with mode=user don't
    // re-fire.
    if (!elevated && wasElevated) {
      toast.info("Admin session ended");
      warnedRef.current = "ended";
    }
    prevModeRef.current = mode;
  }, [mode]);

  const remainingMs = expiresAt > 0 ? expiresAt - now : 0;
  const showCountdown = expiresAt > 0 && (mode === "admin" || mode === "elevated");

  // <30s warning + flash. Trigger once per elevation window. The latch
  // is keyed on expiresAt (a new elevation = new expiresAt = warn again).
  useEffect(() => {
    if (!showCountdown) return;
    if (remainingMs > 0 && remainingMs <= 30_000) {
      if (warnedRef.current !== expiresAt) {
        warnedRef.current = expiresAt;
        toast.warning(
          "Admin session ending in 30s — re-elevate to continue",
          { duration: 5000 },
        );
        setFlashing(true);
        window.setTimeout(() => setFlashing(false), 2000);
      }
    }
  }, [remainingMs, expiresAt, showCountdown]);

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
      setAuthMode({ mode: "user", expiresAt: 0 });
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
    if (mode === "admin") {
      return (
        <span
          className={
            "inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider transition-colors " +
            (flashing
              ? "bg-amber-300 text-amber-950 animate-pulse"
              : "bg-amber-200 text-amber-900 dark:bg-amber-500/20 dark:text-amber-200")
          }
          data-testid="persona-pill"
          data-mode="admin"
        >
          ADMIN
        </span>
      );
    }
    // elevated
    return (
      <span
        className={
          "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider transition-colors " +
          (flashing
            ? "bg-orange-300 text-orange-950 animate-pulse"
            : "bg-orange-200 text-orange-900 dark:bg-orange-500/20 dark:text-orange-200")
        }
        data-testid="persona-pill"
        data-mode="elevated"
      >
        ADMIN
        <LightningBolt className="h-3 w-3" />
      </span>
    );
  }, [mode, flashing]);

  return (
    <div className="flex items-center gap-1.5">
      {pillContent}
      {showCountdown && (
        <>
          <span
            className="inline-flex items-center rounded-md bg-muted/60 px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground tabular-nums"
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
            className="inline-flex h-5 w-5 items-center justify-center rounded-full text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
            data-testid="persona-drop"
          >
            <CrossIcon />
          </button>
        </>
      )}
    </div>
  );
}
