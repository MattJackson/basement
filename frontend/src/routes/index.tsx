import { createFileRoute, Navigate } from "@tanstack/react-router";

// Bare `/` redirects to `/admin` per design.md routing convention.
export const Route = createFileRoute("/")({
  component: () => <Navigate to="/admin" />,
});
