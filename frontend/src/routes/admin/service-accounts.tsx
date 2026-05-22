import { createFileRoute, Outlet } from "@tanstack/react-router";
import { adminPage } from "@/shared/layout/adminPage";

// v1.7.0c — layout for /admin/service-accounts and children
// (/admin/service-accounts/new). Same pattern as
// /admin/users.tsx: parent is Outlet-only wrapped in the admin
// chrome, children own their own page bodies.
export const Route = createFileRoute("/admin/service-accounts")({
  component: adminPage(ServiceAccountsLayout),
});

function ServiceAccountsLayout() {
  return <Outlet />;
}
