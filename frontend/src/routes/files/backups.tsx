import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/backups and children (/files/backups/new and
// /files/backups/$id). Same Outlet-only pattern as syncs.tsx — the
// child routes own their own page content, this just wraps everything
// in the UserShell chrome.
export const Route = createFileRoute("/files/backups")({
  component: userPage(BackupsLayout),
});

function BackupsLayout() {
  return <Outlet />;
}
