import { createFileRoute, Navigate } from "@tanstack/react-router";
import { useUser } from "@/shared/auth/useUser";

export const Route = createFileRoute("/")({
  component: RoleGatedRoot,
});

function RoleGatedRoot() {
  const { data: user, isLoading } = useUser();
  if (isLoading) return null;
  // Three-axis model: UI Admin → admin shell. Otherwise → user shell.
  const isAdmin = user?.uiAdmin === true;
  return <Navigate to={isAdmin ? "/admin/clusters" : "/files"} replace />;
}
