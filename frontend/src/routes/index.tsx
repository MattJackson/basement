import { createFileRoute, Navigate } from "@tanstack/react-router";
import { useUser } from "@/shared/auth/useUser";
import { useCan } from "@/shared/auth/useCan";
import { resolveAdminLanding } from "@/shared/auth/adminLanding";
import LoadingSpinner from "@/shared/ui/LoadingSpinner";

// v1.13.35: root route is activeRole-aware. Operator was landing
// on /files even when their activeRole was UI Admin or Cluster
// Admin, which then rendered the user-tier nav (Files / Keys /
// Shares / Backups / Federations / Webhooks) — confusing for
// someone who just elevated. New behavior:
//
//   - unauthenticated         → /login
//   - ui-admin                → /admin/clusters  (cluster.wiring.list)
//   - cluster-admin (with cid)→ /admin/clusters/<cid>
//   - user / none             → /files
//
// ADR-0009: the actual decision lives in resolveAdminLanding() so the
// "/" entry here and the bare "/admin" entry in ProtectedRoute can't
// diverge — both call the same capability-driven helper.
//
// Pre-v1.13.35 this route used a synchronous localStorage check
// against "basement_auth_token" — that key was never written under
// the current HttpOnly-cookie flow, so the branch was effectively
// dead. The component below intentionally waits for /auth/me to
// resolve before deciding; the LoadingSpinner pass is brief because
// useUser caches the response for 5 minutes (staleTime).
function RootRouteRedirect() {
  const { data, isLoading, isError } = useUser();
  const { can } = useCan();

  if (isLoading) {
    return <LoadingSpinner />;
  }

  // Unauthenticated (or auth check failed) → /login. /login is
  // public and handles the post-auth redirect on its own.
  if (isError || !data) {
    return <Navigate to="/login" replace />;
  }

  // ADR-0009: landing surface follows capability via the shared
  // resolver (kept in lockstep with ProtectedRoute's bare-/admin
  // landing). Covers UI Admin → /admin/clusters, cluster-admin →
  // their own cluster detail, and user / missing-activeRole → /files.
  const landing = resolveAdminLanding(can, data.activeRole?.cluster);
  if (landing.to === "/admin/clusters/$cid") {
    return (
      <Navigate to="/admin/clusters/$cid" params={{ cid: landing.cid! }} replace />
    );
  }
  return <Navigate to={landing.to} replace />;
}

export const Route = createFileRoute("/")({
  component: RootRouteRedirect,
});
