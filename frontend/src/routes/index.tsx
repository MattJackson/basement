import { createFileRoute, Navigate } from "@tanstack/react-router";

// Post-ADR-0003 (sudo-style elevation): everyone — including UI Admins —
// lands in the user shell after login. To enter the admin section,
// click "Switch to admin" in the UserMenu, which prompts for elevation
// and only then navigates to /admin/clusters. This way a fresh login
// is always USER mode, and admin actions require explicit re-auth.
export const Route = createFileRoute("/")({
  component: () => {
    // v1.13.12 — sync client-side branch for instant redirect when unauthed.
    // The root was timing out on navigation because ProtectedRoute awaited
    // the /auth/me fetch before redirecting, exceeding the 2s threshold.
    // This synchronous check redirects IMMEDIATELY without waiting for any
    // API call — the client-side condition is purely local state (window.location).
    if (!localStorage.getItem("basement_auth_token")) {
      return <Navigate to="/login" replace />;
    }
    return <Navigate to="/files" replace />;
  },
});
