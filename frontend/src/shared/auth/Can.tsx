// ADR-0009 — declarative capability gate.
//
// Renders children only when the active role passes the capability
// check; otherwise renders `fallback` (default: nothing). Replaces the
// scattered `{activeRole?.kind === "ui-admin" && <Link/>}` conditionals.
//
//   <Can cap={CAP.PLATFORM_USERS_LIST}><Link .../></Can>
//   <Can anyOf={[CAP.CLUSTER_WIRING_READ, CAP.CLUSTER_CONTENTS_READ]}>…</Can>
//   <Can allOf={[CAP.CLUSTER_KEYS_CREATE, CAP.CLUSTER_KEYS_DELETE]}>…</Can>
//
// Exactly one of `cap` / `anyOf` / `allOf` should be supplied; if more
// than one is given they are AND-ed together. Defense-in-depth only —
// the backend enforces. Fail-closed while /auth/me loads.

import type { ReactNode } from "react";
import { useCan } from "./useCan";

export interface CanProps {
  /** Single capability that must be present. */
  cap?: string;
  /** Passes if ANY of these capabilities are present. */
  anyOf?: string[];
  /** Passes if ALL of these capabilities are present. */
  allOf?: string[];
  children: ReactNode;
  /** Rendered when the check fails (default: nothing). */
  fallback?: ReactNode;
}

export function Can({ cap, anyOf, allOf, children, fallback = null }: CanProps): ReactNode {
  const { can, canAny, canAll } = useCan();

  const checks: boolean[] = [];
  if (cap !== undefined) checks.push(can(cap));
  if (anyOf !== undefined) checks.push(canAny(...anyOf));
  if (allOf !== undefined) checks.push(canAll(...allOf));

  // No predicate supplied → fail-closed (deny). Otherwise every
  // supplied predicate must pass.
  const allowed = checks.length > 0 && checks.every(Boolean);

  return <>{allowed ? children : fallback}</>;
}
