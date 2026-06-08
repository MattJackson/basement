import type { ReactNode } from "react";
import { useEffect } from "react";
import { useNavigate, useLocation } from "@tanstack/react-router";
import { toast } from "sonner";
import { useUser } from "./useUser";
import { useCan } from "./useCan";
import { resolveRequiredCapabilities } from "./route-capabilities";
import { resolveAdminLanding } from "./adminLanding";
import { CAP } from "./capabilities";
import LoadingSpinner from "@/shared/ui/LoadingSpinner";

interface ProtectedRouteProps {
  children: ReactNode;
}

/**
 * ProtectedRoute (ADR-0009 Phase D) — capability-based route gate.
 *
 * Replaces the old pathname-prefix / activeRole.kind switch with a
 * single lookup in route-capabilities.ts. Behaviour:
 *
 *   - Unauthenticated → /login with a safe ?next= (admin paths preserve
 *     the deep link; everything else falls back to /files).
 *   - A protected /admin/* route the active role lacks the capability
 *     for → toast + bounce to /files.
 *   - Cluster-admin convenience redirects (surface-switching, NOT
 *     permission enforcement — ADR-0009 keeps ar.Kind valid for "which
 *     shell renders"):
 *       · /admin/clusters list (needs cluster.wiring.list, which a
 *         cluster-admin lacks) → silently route to their own cluster
 *         detail instead of a denial toast.
 *       · /admin/clusters/{other}/... → silently route to their own
 *         cluster (cross-cluster deep link).
 *       · bare /admin landing → route to the surface their role owns.
 *
 * Client gating is defense-in-depth; the backend enforces every
 * capability independently.
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { data, isLoading } = useUser();
  const { can, canAny } = useCan();

  useEffect(() => {
    if (isLoading) return;
    if (location.pathname === "/login") return;

    const pathname = location.pathname;

    if (!data) {
      const next = pathname.startsWith("/admin") ? pathname : "/files";
      navigate({ to: "/login", search: { next } });
      return;
    }

    // Only /admin/* routes are capability-gated here; /files and the
    // public surface are open to any authenticated session.
    if (!pathname.startsWith("/admin")) return;

    const activeRole = data.activeRole;
    const isClusterAdmin = activeRole?.kind === "cluster-admin";
    const ownCluster = isClusterAdmin ? activeRole?.cluster : undefined;

    const bounceToFiles = (message: string) => {
      toast.info(message);
      navigate({ to: "/files" });
    };

    // ── Surface-switching convenience redirects ──────────────────────
    // The bare /admin landing routes to the surface the active role
    // owns (kind drives WHICH shell, not permission). The decision is
    // delegated to the shared resolveAdminLanding() so this stays in
    // lockstep with the "/" entry in RootRouteRedirect — the two used
    // to disagree for a UI Admin (system vs clusters).
    if (pathname === "/admin" || pathname === "/admin/") {
      // A cluster-admin's cluster id comes off their active role; the
      // resolver falls back to it for the contents.read branch.
      const landing = resolveAdminLanding(can, ownCluster);
      if (landing.to === "/files") {
        bounceToFiles("Switch to an admin role to access this page.");
        return;
      }
      if (landing.to === "/admin/clusters/$cid") {
        navigate({ to: `/admin/clusters/${landing.cid}` });
        return;
      }
      navigate({ to: landing.to });
      return;
    }

    // A cluster-admin only has ONE cluster: the cross-cluster list
    // page (cluster.wiring.list, which they lack) and any other
    // cluster's detail page silently route to their own cluster so the
    // backend 403 never surfaces. This runs BEFORE the capability gate
    // so it wins over the denial toast.
    if (isClusterAdmin && ownCluster) {
      if (pathname === "/admin/clusters") {
        navigate({ to: `/admin/clusters/${ownCluster}` });
        return;
      }
      const clusterSeg = pathname.match(/^\/admin\/clusters\/([^/]+)/);
      const targetCluster = clusterSeg ? clusterSeg[1] : null;
      if (targetCluster && targetCluster !== "new" && targetCluster !== ownCluster) {
        navigate({ to: `/admin/clusters/${ownCluster}` });
        return;
      }
    }

    // ── Capability gate ─────────────────────────────────────────────
    const required = resolveRequiredCapabilities(pathname);
    if (required === null) {
      // Unmapped /admin/* route (e.g. a future page not yet in the
      // map): require any admin capability so a plain user is still
      // bounced, but don't over-restrict between the two admin roles.
      const anyAdmin = canAny(
        CAP.PLATFORM_SYSTEM_READ,
        CAP.CLUSTER_WIRING_LIST,
        CAP.CLUSTER_WIRING_READ,
        CAP.CLUSTER_CONTENTS_READ,
      );
      if (!anyAdmin) {
        bounceToFiles("Switch to an admin role to access this page.");
      }
      return;
    }

    if (!canAny(...required)) {
      // Tailor the toast: platform/wiring caps are UI-Admin territory;
      // everything else just needs "an admin role".
      const platformOrWiring = required.some(
        (c) => c.startsWith("platform.") || c.startsWith("cluster.wiring.") ||
          c === CAP.CLUSTER_BUCKETS_AGGREGATE || c === CAP.CLUSTER_USAGE_AGGREGATE,
      );
      bounceToFiles(
        platformOrWiring
          ? "Switch to UI Admin role to access this page."
          : "Switch to an admin role to access this page.",
      );
      return;
    }
  }, [isLoading, data, navigate, location, can, canAny]);

  if (isLoading || !data) {
    return <LoadingSpinner />;
  }

  return <>{children}</>;
}
