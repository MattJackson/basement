import { createFileRoute, Navigate } from "@tanstack/react-router";
import { useUser } from "@/shared/auth/useUser";
import LoadingSpinner from "@/shared/ui/LoadingSpinner";

// v1.13.35: root route is activeRole-aware. Operator was landing
// on /files even when their activeRole was UI Admin or Cluster
// Admin, which then rendered the user-tier nav (Files / Keys /
// Shares / Backups / Federations / Webhooks) — confusing for
// someone who just elevated. New behavior:
//
//   - unauthenticated         → /login
//   - activeRole: ui-admin    → /admin/clusters  (UI Admin is super-admin
//                                                  on every cluster; the
//                                                  cluster list is the
//                                                  natural overview)
//   - activeRole: cluster-admin (with cid)
//                              → /admin/clusters/<cid>
//   - activeRole: user / none → /files          (the user shell)
//
// Pre-v1.13.35 this route used a synchronous localStorage check
// against "basement_auth_token" — that key was never written under
// the current HttpOnly-cookie flow, so the branch was effectively
// dead. The component below intentionally waits for /auth/me to
// resolve before deciding; the LoadingSpinner pass is brief because
// useUser caches the response for 5 minutes (staleTime).
function RootRouteRedirect() {
  const { data, isLoading, isError } = useUser();

  if (isLoading) {
    return <LoadingSpinner />;
  }

  // Unauthenticated (or auth check failed) → /login. /login is
  // public and handles the post-auth redirect on its own.
  if (isError || !data) {
    return <Navigate to="/login" replace />;
  }

  const activeRole = data.activeRole;

  if (activeRole?.kind === "ui-admin") {
    return <Navigate to="/admin/clusters" replace />;
  }

  if (activeRole?.kind === "cluster-admin" && activeRole.cluster) {
    return (
      <Navigate
        to="/admin/clusters/$cid"
        params={{ cid: activeRole.cluster }}
        replace
      />
    );
  }

  // Default: user shell. Covers activeRole.kind === "user", missing
  // activeRole (pre-v1.13.18 sessions during the grace window), and
  // unknown future kinds.
  return <Navigate to="/files" replace />;
}

export const Route = createFileRoute("/")({
  component: RootRouteRedirect,
});
