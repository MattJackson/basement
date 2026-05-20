import { createFileRoute, Outlet } from "@tanstack/react-router";
import { adminPage } from "@/shared/layout/adminPage";

// Layout for /admin/users and children (/admin/users/new). Renders
// only the Outlet wrapped in AppShell chrome. Without this layout,
// /admin/users/new fell back to the parent which had no Outlet, so
// the child route never mounted.
export const Route = createFileRoute("/admin/users")({
  component: adminPage(UsersLayout),
});

function UsersLayout() {
  return <Outlet />;
}
