// ADR-0009 — route → required-capability map.
//
// ProtectedRoute reads this instead of pattern-matching pathname
// against activeRole.kind. Each protected route declares the
// capability (or any-of set) the active role must hold. Adding a route
// is ONE entry here + ONE RequireCapability on the backend mount.
//
// Matching is longest-prefix: the most specific matching pattern wins,
// so /admin/clusters/$cid/buckets/$id (contents.read) takes precedence
// over /admin/clusters (wiring.list). Patterns use `$seg` to denote a
// single dynamic path segment.
//
// Defense-in-depth ONLY — the backend independently enforces. A
// mismatch here is a UX bug (wrong toast / over-eager bounce), not a
// breach. Capability choices follow the role→capability matrix in
// internal/auth/capabilities.go.

import { CAP } from "./capabilities";

export interface RouteCapabilityRule {
  /** Route pattern. `$seg` matches exactly one path segment. */
  pattern: string;
  /** Passes if the active role holds AT LEAST ONE of these. */
  anyOf: string[];
}

// Ordering does not matter — resolution picks the longest pattern that
// matches. Keep grouped by surface for readability.
export const ROUTE_CAPABILITIES: RouteCapabilityRule[] = [
  // ── Platform (UI Admin) ──────────────────────────────────────────
  { pattern: "/admin/system", anyOf: [CAP.PLATFORM_SYSTEM_READ] },
  { pattern: "/admin/audit", anyOf: [CAP.PLATFORM_AUDIT_READ] },
  { pattern: "/admin/policies", anyOf: [CAP.PLATFORM_POLICIES_LIST] },
  { pattern: "/admin/users", anyOf: [CAP.PLATFORM_USERS_LIST] },
  { pattern: "/admin/users/new", anyOf: [CAP.PLATFORM_USERS_CREATE] },
  { pattern: "/admin/service-accounts", anyOf: [CAP.PLATFORM_SERVICE_ACCOUNTS_LIST] },
  { pattern: "/admin/service-accounts/new", anyOf: [CAP.PLATFORM_SERVICE_ACCOUNTS_WRITE] },
  { pattern: "/admin/service-accounts/$seg", anyOf: [CAP.PLATFORM_SERVICE_ACCOUNTS_LIST] },
  { pattern: "/admin/skins", anyOf: [CAP.PLATFORM_SKINS_READ] },
  { pattern: "/admin/oidc", anyOf: [CAP.PLATFORM_OIDC_READ] },
  { pattern: "/admin/onboarding", anyOf: [CAP.PLATFORM_ONBOARDING_READ] },
  { pattern: "/admin/first-run", anyOf: [CAP.PLATFORM_ONBOARDING_READ] },

  // ── Cross-cluster aggregates (UI Admin) ──────────────────────────
  { pattern: "/admin/buckets", anyOf: [CAP.CLUSTER_BUCKETS_AGGREGATE] },
  { pattern: "/admin/usage", anyOf: [CAP.CLUSTER_USAGE_AGGREGATE] },
  // Migrate moves buckets across clusters — a UI-Admin wiring op.
  { pattern: "/admin/migrate", anyOf: [CAP.CLUSTER_BUCKETS_AGGREGATE] },

  // ── Cluster wiring (UI Admin) ────────────────────────────────────
  // List page: cross-cluster, UI-Admin-only.
  { pattern: "/admin/clusters", anyOf: [CAP.CLUSTER_WIRING_LIST] },
  // Register a new cluster — wiring.create (UI Admin only).
  { pattern: "/admin/clusters/new", anyOf: [CAP.CLUSTER_WIRING_CREATE] },
  // Edit one cluster's connection — wiring.update (UI Admin only).
  { pattern: "/admin/clusters/$seg/edit", anyOf: [CAP.CLUSTER_WIRING_UPDATE] },

  // ── Cluster detail + contents ────────────────────────────────────
  // Detail page renders the wiring section for anyone with
  // wiring.read (UI Admin on any cluster, cluster-admin on theirs) and
  // lazily fetches contents only when the user also holds
  // contents.read. So the route itself gates on wiring.read.
  { pattern: "/admin/clusters/$seg", anyOf: [CAP.CLUSTER_WIRING_READ] },
  // Contents sub-pages (buckets/keys/lifecycle) are cluster-admin's:
  // they need contents.read. UI Admin (no contents.read) is bounced.
  { pattern: "/admin/clusters/$seg/buckets/$seg", anyOf: [CAP.CLUSTER_CONTENTS_READ] },
  { pattern: "/admin/clusters/$seg/buckets/$seg/lifecycle/new", anyOf: [CAP.CLUSTER_BUCKETS_LIFECYCLE_WRITE] },
  { pattern: "/admin/clusters/$seg/buckets/$seg/lifecycle/$seg/edit", anyOf: [CAP.CLUSTER_BUCKETS_LIFECYCLE_WRITE] },
  { pattern: "/admin/clusters/$seg/keys/$seg", anyOf: [CAP.CLUSTER_CONTENTS_READ] },
  // Layout (Garage topology) + scrub maintenance — cluster-admin.
  { pattern: "/admin/clusters/$seg/layout", anyOf: [CAP.CLUSTER_LAYOUT_WRITE] },
  { pattern: "/admin/clusters/$seg/scrub", anyOf: [CAP.CLUSTER_SCRUB_WRITE] },
];

/**
 * Split a pathname into non-empty segments.
 */
function segments(path: string): string[] {
  return path.split("/").filter(Boolean);
}

/**
 * matchPattern reports whether a concrete pathname matches a pattern,
 * treating `$seg` as a single-segment wildcard. Segment counts must be
 * equal (no implicit prefix/suffix matching — that's handled by
 * choosing the longest matching rule in resolveRequiredCapabilities).
 */
function matchPattern(path: string, pattern: string): boolean {
  const p = segments(path);
  const q = segments(pattern);
  if (p.length !== q.length) return false;
  for (let i = 0; i < q.length; i++) {
    if (q[i] === "$seg") continue;
    if (q[i] !== p[i]) return false;
  }
  return true;
}

/**
 * resolveRequiredCapabilities returns the any-of capability set for the
 * route that best (most specifically) matches `pathname`, or null when
 * no protected rule applies (route is open to any authenticated user
 * once the /admin gate below is satisfied).
 *
 * "Best" = the matching pattern with the most segments; ties broken by
 * declaration order (first wins, but ties shouldn't occur in practice).
 */
export function resolveRequiredCapabilities(pathname: string): string[] | null {
  let best: RouteCapabilityRule | null = null;
  let bestLen = -1;
  for (const rule of ROUTE_CAPABILITIES) {
    if (!matchPattern(pathname, rule.pattern)) continue;
    const len = segments(rule.pattern).length;
    if (len > bestLen) {
      best = rule;
      bestLen = len;
    }
  }
  return best ? best.anyOf : null;
}
