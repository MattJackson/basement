// ADR-0009 — canonical post-auth landing surface.
//
// Two entry points decide where an authenticated operator lands:
//   · RootRouteRedirect ("/") — typed the bare root.
//   · ProtectedRoute (bare "/admin" / "/admin/") — typed the admin root.
//
// Both MUST agree, or the same role lands on a different page depending
// on which URL they hit. This helper is the single source of truth so
// the two cannot drift. The order is capability-driven (not role-kind):
//
//   1. cluster.wiring.list  → /admin/clusters     (UI Admin — the
//      cross-cluster overview is the natural super-admin landing;
//      a UI Admin also holds platform.system.read but the cluster
//      list is the more useful first surface.)
//   2. platform.system.read → /admin/system       (a platform-only
//      role with no cluster list, if one ever exists.)
//   3. cluster.contents.read + own cluster → that cluster's detail
//      (cluster-admin — single cluster, no list page.)
//   4. otherwise            → /files              (user / no admin cap.)

import { CAP } from "./capabilities";

export interface AdminLandingResult {
  /** Concrete path for surfaces without route params. */
  to: "/admin/clusters" | "/admin/system" | "/files" | "/admin/clusters/$cid";
  /** Set when `to` is the param'd cluster-detail route. */
  cid?: string;
}

/**
 * Resolve the canonical landing surface for an authenticated operator.
 *
 * @param can          useCan().can — capability predicate.
 * @param activeCluster the active role's cluster id (surface-routing
 *                      data: WHICH cluster, read off activeRole.cluster).
 */
export function resolveAdminLanding(
  can: (capability: string) => boolean,
  activeCluster: string | undefined,
): AdminLandingResult {
  if (can(CAP.CLUSTER_WIRING_LIST)) {
    return { to: "/admin/clusters" };
  }
  if (can(CAP.PLATFORM_SYSTEM_READ)) {
    return { to: "/admin/system" };
  }
  if (can(CAP.CLUSTER_CONTENTS_READ) && activeCluster) {
    return { to: "/admin/clusters/$cid", cid: activeCluster };
  }
  return { to: "/files" };
}
