import { createFileRoute, Navigate } from "@tanstack/react-router";

// /admin = the admin section root. Lands on /admin/clusters since the
// admin's natural entry is "see and manage the clusters." Persona
// split: My Buckets (the user view) lives at / now. See
// memory/persona_split_user_vs_admin.md for the rationale.
export const Route = createFileRoute("/admin/")({
  component: () => <Navigate to="/admin/clusters" replace />,
});
