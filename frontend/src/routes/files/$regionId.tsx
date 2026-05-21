import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/$regionId and its children (ADR-0002, v1.1.0c).
// Replaces the deleted /files/$cid layout. Wraps Outlet in userPage()
// so UserShell chrome wraps BOTH the index (bucket list) and the
// nested bucket browser. Children must NOT call userPage() again
// (caught in v0.8.0d.7 — double-wrapping renders two UserShells).
export const Route = createFileRoute("/files/$regionId")({
  component: userPage(RegionLayout),
});

function RegionLayout() {
  return <Outlet />;
}
