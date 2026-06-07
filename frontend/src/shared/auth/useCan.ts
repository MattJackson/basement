// ADR-0009 — capability gating hook.
//
// Reads the active role's flat capability list off /auth/me (via
// useUser) and returns memoized helpers for checking capabilities.
//
// Defense-in-depth ONLY: the backend independently enforces every
// capability. A mapping mistake here is a UX bug, not a breach.
//
// Fail-closed: while /auth/me is loading, errored, or omits the
// capabilities field (older backend), every check returns false
// (default-deny). Callers that want to distinguish "loading" from
// "denied" can read `isLoading`.

import { useMemo } from "react";
import { useUser } from "./useUser";

export interface CanHelpers {
  /** True iff the active role grants the given capability. */
  can: (capability: string) => boolean;
  /** True iff the active role grants AT LEAST ONE of the capabilities. */
  canAny: (...capabilities: string[]) => boolean;
  /** True iff the active role grants ALL of the capabilities. */
  canAll: (...capabilities: string[]) => boolean;
  /** True while /auth/me is still resolving (checks default-deny). */
  isLoading: boolean;
}

export function useCan(): CanHelpers {
  const { data, isLoading } = useUser();

  // Build a Set once per capability-list identity. When loading or the
  // field is absent, the Set is empty → every check default-denies.
  const capabilities = data?.capabilities;

  return useMemo<CanHelpers>(() => {
    const set = new Set(capabilities ?? []);
    const can = (capability: string) => set.has(capability);
    return {
      can,
      canAny: (...caps: string[]) => caps.some((c) => set.has(c)),
      canAll: (...caps: string[]) => caps.length > 0 && caps.every((c) => set.has(c)),
      isLoading,
    };
  }, [capabilities, isLoading]);
}
