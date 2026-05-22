// ADR-0003 / v1.2.0b — client-side sudo-style mode state.
//
// The backend is the source of truth (the JWT cookie carries `mode` and
// `modeExpiresAt`); this provider mirrors those two fields in React
// state so the persona pill can render a live countdown and the
// elevation modal can flip the value on a successful re-auth without
// waiting for the next /auth/me refetch.
//
// Hydration order:
//   1. useAuthModeHydrate() runs on first mount, reads /auth/me via
//      useUser(), and seeds the context with `{mode, expiresAt}`.
//   2. ElevationModal (success) + the "drop privileges" button call
//      useSetAuthMode() to overwrite the state after their respective
//      backend roundtrips set a fresh cookie.
//   3. A self-running effect auto-downgrades stale modes once a second
//      — when expiresAt < now, ELEVATED drops to ADMIN and ADMIN drops
//      to USER. This mirrors the backend gate's currentMode() rules so
//      the UI doesn't render permissions the next API call would deny.
//
// The provider deliberately does NOT call any endpoints itself; that
// stays in the modal / button / hooks that already own the user-
// initiated transitions.

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";

export type AuthMode = "user" | "admin" | "elevated";

export interface ModeState {
  mode: AuthMode;
  /**
   * Unix milliseconds when the mode reverts. 0 means "never expires at
   * the mode layer" (USER, or the pre-v1.2 cookie grace promotion).
   *
   * NOTE: the wire shape carries seconds (`modeExpiresAt`); the
   * provider normalises to milliseconds at hydration time so all the
   * countdown math uses `Date.now()` directly.
   */
  expiresAt: number;
}

const DEFAULT_STATE: ModeState = { mode: "user", expiresAt: 0 };

type Setter = (next: ModeState) => void;

const ModeContext = createContext<ModeState>(DEFAULT_STATE);
const SetterContext = createContext<Setter>(() => {});

export interface AuthModeProviderProps {
  initial?: ModeState;
  children: ReactNode;
}

/**
 * AuthModeProvider seeds the context from `initial` (typically null on
 * first render; populated by AuthModeHydrator once /auth/me lands) and
 * runs the auto-downgrade tick that mirrors the backend.
 */
export function AuthModeProvider({ initial, children }: AuthModeProviderProps) {
  const [state, setState] = useState<ModeState>(initial ?? DEFAULT_STATE);
  const stateRef = useRef(state);
  // Sync the ref to the latest state in an effect so we don't mutate
  // during render (which the react-hooks/refs lint rule flags). The
  // ref is only read inside the 1Hz interval below, so an effect-
  // synced update is fine — the worst case is a one-tick lag.
  useEffect(() => {
    stateRef.current = state;
  }, [state]);

  const queryClient = useQueryClient();

  // Auto-downgrade ticker. Mirrors policy_gates.currentMode():
  //   - ELEVATED with expired ModeExpiresAt → ADMIN
  //   - ADMIN with expired ModeExpiresAt    → USER
  //   - USER                                → no-op (never expires)
  //
  // Runs at 1Hz which is good enough for the persona pill countdown
  // (the only consumer that cares about sub-minute resolution). When
  // a downgrade fires we also invalidate the auth/me query so any
  // server-side rotation gets pulled fresh on the next render.
  useEffect(() => {
    const tick = () => {
      const current = stateRef.current;
      if (current.expiresAt === 0) return;
      if (Date.now() < current.expiresAt) return;

      if (current.mode === "elevated") {
        // ELEVATED → ADMIN. The backend would mint a new cookie with
        // mode=admin on the next request that touches a fresh
        // sessionTTL; we approximate that here by clearing the expiry
        // (no countdown on a mode whose true window we don't know).
        setState({ mode: "admin", expiresAt: 0 });
        queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      } else if (current.mode === "admin") {
        // ADMIN → USER. Same caveat as above.
        setState({ mode: "user", expiresAt: 0 });
        queryClient.invalidateQueries({ queryKey: ["auth", "me"] });
      }
    };

    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [queryClient]);

  const setter = useCallback<Setter>((next) => {
    setState(next);
  }, []);

  const stableState = useMemo(() => state, [state]);

  return (
    <ModeContext.Provider value={stableState}>
      <SetterContext.Provider value={setter}>{children}</SetterContext.Provider>
    </ModeContext.Provider>
  );
}

/** useAuthMode returns the current mode + expiry. */
export function useAuthMode(): ModeState {
  return useContext(ModeContext);
}

/** useSetAuthMode returns the imperative setter. */
export function useSetAuthMode(): Setter {
  return useContext(SetterContext);
}

/**
 * computeNextStateOnDowngrade is the pure version of the auto-
 * downgrade rule. Exported for tests so we can verify the ladder
 * without standing up a provider + fake timers.
 */
export function computeNextStateOnDowngrade(
  current: ModeState,
  now: number,
): ModeState {
  if (current.expiresAt === 0) return current;
  if (now < current.expiresAt) return current;
  if (current.mode === "elevated") return { mode: "admin", expiresAt: 0 };
  if (current.mode === "admin") return { mode: "user", expiresAt: 0 };
  return current;
}
