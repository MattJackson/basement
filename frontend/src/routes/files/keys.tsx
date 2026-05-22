import { createFileRoute, Outlet } from "@tanstack/react-router";
import { userPage } from "@/shared/layout/userPage";

// Layout for /files/keys and its children (/files/keys/new). Same
// Outlet-only pattern as /files/shares — the parent route needs an
// Outlet so the child route can mount. Page content lives in
// keys/index.tsx (list) and keys/new.tsx (form). v1.2.0d.
export const Route = createFileRoute("/files/keys")({
  component: userPage(KeysLayout),
});

function KeysLayout() {
  return <Outlet />;
}
