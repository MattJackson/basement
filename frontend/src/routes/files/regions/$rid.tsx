import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/regions/$rid and its children. Wraps Outlet in
// userPage so the UserShell chrome (header/nav) appears around both
// the index (bucket list) and child routes (bucket browser at
// b/$bid). Without userPage here, the child would render bare.
export const Route = createFileRoute("/files/regions/$rid")({
  component: userPage(RegionLayout),
});

function RegionLayout() {
  return <Outlet />;
}
