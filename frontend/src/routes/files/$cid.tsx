import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/$cid and its children. Wraps Outlet in userPage
// so the UserShell chrome (header/nav) appears around BOTH the index
// (bucket list) and child routes (bucket browser at b/$bid). Without
// userPage here, /files/$cid/b/$bid would render bare (no shell).
export const Route = createFileRoute("/files/$cid")({
  component: userPage(ClusterLayout),
});

function ClusterLayout() {
  return <Outlet />;
}
