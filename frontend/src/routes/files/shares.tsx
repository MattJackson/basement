import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/shares and children (/files/shares/new). Renders
// only the Outlet wrapped in UserShell chrome — same pattern as
// /files/$cid (Outlet-only layout, content lives in index.tsx).
// Without this layout, /files/shares/new fell back to the parent's
// component which had no Outlet, so the child route never mounted.
export const Route = createFileRoute("/files/shares")({
  component: userPage(SharesLayout),
});

function SharesLayout() {
  return <Outlet />;
}
