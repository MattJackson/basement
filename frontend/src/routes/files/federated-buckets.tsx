import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/federated-buckets and children
// (/files/federated-buckets/new and /files/federated-buckets/$id). Same
// Outlet-only pattern as backups.tsx + syncs.tsx — the child routes own
// their own page content, this just wraps everything in the UserShell
// chrome. Lesson from v1.5.0c.1: a parent without an Outlet swallows
// every child route, so we ship the layout in the same cycle as the
// children.
export const Route = createFileRoute("/files/federated-buckets")({
  component: userPage(FederatedBucketsLayout),
});

function FederatedBucketsLayout() {
  return <Outlet />;
}
