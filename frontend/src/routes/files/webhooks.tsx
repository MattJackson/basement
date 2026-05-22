import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/webhooks and children (/files/webhooks/new and
// /files/webhooks/$id). Same Outlet-only pattern as backups.tsx and
// federated-buckets.tsx — the child routes own their own page content,
// this just wraps everything in the UserShell chrome. Lands in v1.7.0e
// alongside the user-owned webhook subscription endpoints from v1.7.0d.
export const Route = createFileRoute("/files/webhooks")({
  component: userPage(WebhooksLayout),
});

function WebhooksLayout() {
  return <Outlet />;
}
