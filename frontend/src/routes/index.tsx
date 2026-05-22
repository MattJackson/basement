import { createFileRoute, Navigate } from "@tanstack/react-router";

// Post-ADR-0003 (sudo-style elevation): everyone — including UI Admins —
// lands in the user shell after login. To enter the admin section,
// click "Switch to admin" in the UserMenu, which prompts for elevation
// and only then navigates to /admin/clusters. This way a fresh login
// is always USER mode, and admin actions require explicit re-auth.
export const Route = createFileRoute("/")({
  component: () => <Navigate to="/files" replace />,
});
