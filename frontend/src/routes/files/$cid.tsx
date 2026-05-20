import { createFileRoute, Outlet } from "@tanstack/react-router";

// Layout for /files/$cid and its children. Renders only the Outlet;
// the bucket list lives at index.tsx, the bucket browser at b/$bid.tsx.
// Before this restructure, $cid.tsx was a leaf component with no Outlet,
// so clicking a bucket changed the URL to /files/$cid/b/$bid but the
// child route never mounted — destination kept showing the bucket list.
export const Route = createFileRoute("/files/$cid")({
  component: ClusterLayout,
});

function ClusterLayout() {
  return <Outlet />;
}
