import type { ReactNode } from "react";
import { useEffect } from "react";
import { useNavigate, useLocation } from "@tanstack/react-router";
import { toast } from "sonner";
import { useUser } from "./useUser";
import LoadingSpinner from "@/shared/ui/LoadingSpinner";

interface ProtectedRouteProps {
  children: ReactNode;
}

/**
 * ProtectedRoute redirects to /login if the user isn't
 * authenticated, and to /files when the active role doesn't grant
 * access to an /admin/* route.
 *
 * v2.0.0-beta.6: B2 — /admin/clusters/new + /admin/clusters/{cid}/*
 * now allow UI Admin (org-level operations); cluster-admin remains
 * scoped to their assigned cluster. B4 — bounces fire a toast that
 * tells the user which role they need, so the redirect isn't silent.
 */
export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { data, isLoading } = useUser();

  useEffect(() => {
    if (isLoading) return;
    if (location.pathname === "/login") return;

    const pathname = location.pathname;

    if (!data) {
      const next = pathname.startsWith("/admin") ? pathname : "/files";
      navigate({ to: "/login", search: { next } });
      return;
    }

    const activeRole = data.activeRole;

    if (!pathname.startsWith("/admin")) return;

    const clusterSegMatch = pathname.match(/^\/admin\/clusters\/([^/]+)/);
    const clusterSeg = clusterSegMatch ? clusterSegMatch[1] : null;
    const isClusterNewRoute = pathname === "/admin/clusters/new";
    const pathClusterId = clusterSeg && clusterSeg !== "new" ? clusterSeg : null;

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
      pathname.startsWith("/admin/onboarding") ||
      isClusterNewRoute;

    const isClusterScopedRoute = pathClusterId !== null;

    const bounceToFiles = (message: string) => {
      toast.info(message);
      navigate({ to: "/files" });
    };

    if (isUIAdminRoute) {
      if (!activeRole || activeRole.kind !== "ui-admin") {
        bounceToFiles("Switch to UI Admin role to access this page.");
        return;
      }
      return;
    }

    if (isClusterScopedRoute) {
      if (!activeRole || activeRole.kind === "user") {
        bounceToFiles("Switch to an admin role to access this page.");
        return;
      }
      if (activeRole.kind === "cluster-admin") {
        if (pathClusterId && activeRole.cluster && pathClusterId !== activeRole.cluster) {
          // Cross-cluster redirect — silent (auto-route to own cluster).
          navigate({ to: `/admin/clusters/${activeRole.cluster}` });
          return;
        }
      }
      return;
    }

    // Other /admin/* routes (the bare /admin landing, /admin/clusters list,
    // /admin/buckets, /admin/migrate, /admin/usage, /admin/first-run).
    if (!activeRole || activeRole.kind === "user") {
      bounceToFiles("Switch to an admin role to access this page.");
      return;
    }

    if (pathname === "/admin" || pathname === "/admin/") {
      if (activeRole.kind === "cluster-admin" && activeRole.cluster) {
        navigate({ to: `/admin/clusters/${activeRole.cluster}` });
        return;
      }
      navigate({ to: "/admin/clusters" });
      return;
    }
  }, [isLoading, data, navigate, location]);

  if (isLoading || !data) {
    return <LoadingSpinner />;
  }

  return <>{children}</>;
}
