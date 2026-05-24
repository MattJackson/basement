import type { ReactNode } from "react";
import { useEffect } from "react";
import { useNavigate, useLocation } from "@tanstack/react-router";
import { useUser } from "./useUser";
import LoadingSpinner from "@/shared/ui/LoadingSpinner";

interface ProtectedRouteProps {
  children: ReactNode;
}

/**
 * ProtectedRoute redirects to /login if the user isn't
 * authenticated. Carries the current pathname as `?next=` so the
 * user lands back where they started after login.
 *
 * Guards against the recursive-next bug:
 *   - never redirect when already on /login (would just stack)
 *   - uses pathname (path-only), not href (full origin URL), so the
 *     next value is short and ergonomic
 *   - rejects `next` values that don't start with /admin (no
 *     open-redirect to attacker-controlled URLs)
 *
 * v1.13.18: Active role gating — redirects based on activeRole:
 *   - UI Admin routes (/admin/system, /admin/policies, etc.): redirect to /files if activeRole !== "ui-admin"
 *   - Cluster Admin routes (/admin/clusters/{cid}/...): redirect to /files if activeRole !== "cluster-admin" OR if URL's {cid} doesn't match activeRole.cluster
 *   - Cross-cluster admin routes: redirect to per-cluster page in active role's cluster
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { data, isLoading } = useUser();

  useEffect(() => {
    if (isLoading || !data) return;
    
    // Already on the login route? Nothing to do — don't recurse.
    if (location.pathname === "/login") return;

    const activeRole = data.activeRole;
    const pathname = location.pathname;

    // If not authenticated, redirect to /login
    if (!data) {
      const next = pathname.startsWith("/admin") ? pathname : "/files";
      navigate({ to: "/login", search: { next } });
      return;
    }

    // v1.13.18: Active role gating for /admin/* routes
    if (pathname.startsWith("/admin")) {
      // Extract cluster ID from path if present (e.g., /admin/clusters/{cid}/...)
      const match = pathname.match(/\/admin\/clusters\/([^/]+)/);
      const pathClusterId = match ? match[1] : null;

      // UI Admin routes: /admin/system, /admin/policies, /admin/skins/*, /admin/policies/*, /admin/oidc-*, /admin/audit
      const isUIAdminRoute = 
        pathname === "/admin/system" ||
        pathname === "/admin/policies" ||
        pathname.startsWith("/admin/skins") ||
        pathname.startsWith("/admin/policies/") ||
        pathname.startsWith("/admin/oidc") ||
        pathname === "/admin/audit" ||
        pathname.startsWith("/admin/users") ||
        pathname.startsWith("/admin/service-accounts") ||
        pathname.startsWith("/admin/gateways") ||
        pathname.startsWith("/admin/onboarding");

      // Cluster Admin routes: /admin/clusters/{cid}/...
      const isClusterAdminRoute = pathname.startsWith("/admin/clusters/");

      if (isUIAdminRoute) {
        // Must be UI Admin - check that activeRole exists and is not user
        if (!activeRole || activeRole.kind === "user") {
          navigate({ to: "/files" });
          return;
        }
        // Now we know activeRole is either cluster-admin or ui-admin, check it's ui-admin
        const roleIsUIAdmin = activeRole.kind === "ui-admin";
        if (!roleIsUIAdmin) {
          navigate({ to: "/files" });
          return;
        }
      } else if (isClusterAdminRoute) {
        // Must be cluster-admin AND cluster ID must match - check that activeRole exists and is not user
        if (!activeRole || activeRole.kind === "user") {
          navigate({ to: "/files" });
          return;
        }
        // Now we know activeRole is either cluster-admin or ui-admin, check it's cluster-admin
        const roleIsClusterAdmin = activeRole.kind === "cluster-admin";
        if (!roleIsClusterAdmin) {
          navigate({ to: "/files" });
          return;
        }

        // If path has a cluster ID, it must match active role's cluster
        if (pathClusterId && activeRole.cluster && pathClusterId !== activeRole.cluster) {
          // Redirect to the cluster in the active role
          navigate({ to: `/admin/clusters/${activeRole.cluster}` });
          return;
        }

        // If no cluster ID in path, redirect to active role's cluster
        if (!pathClusterId && activeRole.cluster) {
          navigate({ to: `/admin/clusters/${activeRole.cluster}` });
          return;
        }
      } else {
        // Any other /admin/* route — check if user has any admin role
        const activeRoleKind = (activeRole?.kind ?? "user") as string | undefined;
        
        // If role is user, redirect to files
        if (activeRoleKind === "user") {
          navigate({ to: "/files" });
          return;
        }

        // For cluster-admin routes without explicit cid in path, redirect to active cluster
        if (pathname === "/admin" || pathname === "/admin/") {
          if (activeRoleKind === "cluster-admin" && activeRole?.cluster) {
            navigate({ to: `/admin/clusters/${activeRole.cluster}` });
            return;
          }
          // Default to clusters list for UI Admin or cluster-admin without specific route
          const roleIsUser = activeRoleKind === "user";
          if (!roleIsUser) {
            navigate({ to: "/admin/clusters" });
            return;
          }
        }
      }
    }

    // If we get here, user has appropriate access — render children
  }, [isLoading, data, navigate, location]);

  if (isLoading || !data) {
    return <LoadingSpinner />;
  }

  return <>{children}</>;
}

