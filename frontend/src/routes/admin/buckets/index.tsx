import { createFileRoute, Navigate } from "@tanstack/react-router";

/**
 * `/admin/buckets` is preserved only as a redirect — the bucket list
 * is now the primary admin landing at `/admin`. Removes "where do I go
 * for buckets?" confusion without breaking any old bookmarks.
 */
export const Route = createFileRoute("/admin/buckets/")({
  component: () => <Navigate to="/admin" />,
});
