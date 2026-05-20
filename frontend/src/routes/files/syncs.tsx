import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/syncs and children (/files/syncs/new). Renders
// only the Outlet wrapped in UserShell chrome. Without this layout,
// /files/syncs/new fell back to the parent which had no Outlet, so
// the child route never mounted.
export const Route = createFileRoute("/files/syncs")({
  component: userPage(SyncsLayout),
});

function SyncsLayout() {
  return <Outlet />;
}
