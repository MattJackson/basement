import { createFileRoute, redirect } from "@tanstack/react-router";

// Legacy URL alias for the canonical Add-a-key route. ADR-0002 +
// v1.2.0d shifted the user-facing noun from "region" to "key" (each
// access key is a card on /files; multiple keys per endpoint allowed).
// Anything that still points at /files/regions/new — bookmarks, the
// older empty-state CTA, freshman cycle docs — redirects to
// /files/keys/new so we don't break in-flight tabs.
//
// The redirect runs at route-load time via beforeLoad so the user
// never sees a frame of stale chrome. Component is a no-op fallback
// in case redirect is bypassed somehow (defensive — should never
// execute).
export const Route = createFileRoute("/files/regions/new")({
  beforeLoad: () => {
    throw redirect({
      to: "/files/keys/new",
      replace: true,
    });
  },
  component: () => null,
});

export default Route;
