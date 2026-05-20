import { createFileRoute, Navigate } from "@tanstack/react-router";
import { useUser } from "@/shared/auth/useUser";

export const Route = createFileRoute("/")({
  component: RoleGatedRoot,
});

function RoleGatedRoot() {
  const { data: user, isLoading } = useUser();
  if (isLoading) return null;
  // Three-axis model: UI Admin OR any Cluster Admin grant → admin
  // shell. Otherwise → user shell. Today everyone is admin so this
  // always routes to /admin/clusters; gate goes live with RBAC v0.6.1.
  const isAdmin = user?.role === "admin"; // RBAC-aware in v0.6.1
  return <Navigate to={isAdmin ? "/admin/clusters" : "/files"} replace />;
}
