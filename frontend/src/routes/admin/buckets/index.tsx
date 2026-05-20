import { createFileRoute, Navigate } from "@tanstack/react-router";

// `/admin/buckets` is preserved only as a redirect to `/` (the user
// home, My Buckets) after the v0.4.2 persona split. Keeps any
// pre-split bookmarks alive.
export const Route = createFileRoute("/admin/buckets/")({
  component: () => <Navigate to="/" replace />,
});
