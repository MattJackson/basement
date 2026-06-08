// ADR-0009 — canonical post-auth landing resolver.
//
// resolveAdminLanding() is the single source of truth shared by the "/"
// entry (RootRouteRedirect) and the bare "/admin" entry (ProtectedRoute)
// so the same role can't land on a different page depending on which URL
// it hits. These tests lock the capability-driven order.

import { describe, it, expect } from "vitest";

import { resolveAdminLanding } from "@/shared/auth/adminLanding";
import { CAP } from "@/shared/auth/capabilities";

function canFrom(caps: string[]) {
  const set = new Set(caps);
  return (c: string) => set.has(c);
}

// Mirrors the backend role→cap matrix.
const UI_ADMIN_CAPS = [CAP.PLATFORM_SYSTEM_READ, CAP.CLUSTER_WIRING_LIST];
const PLATFORM_ONLY_CAPS = [CAP.PLATFORM_SYSTEM_READ];
const CLUSTER_ADMIN_CAPS = [CAP.CLUSTER_WIRING_READ, CAP.CLUSTER_CONTENTS_READ];
const USER_CAPS = [CAP.SELF_FILES_READ];

describe("resolveAdminLanding", () => {
  it("UI Admin (wiring.list + system.read) → /admin/clusters", () => {
    // wiring.list wins over system.read — the cross-cluster list is the
    // canonical UI Admin landing even though they also hold system.read.
    expect(resolveAdminLanding(canFrom(UI_ADMIN_CAPS), undefined)).toEqual({
      to: "/admin/clusters",
    });
  });

  it("platform-only (system.read, no wiring.list) → /admin/system", () => {
    expect(resolveAdminLanding(canFrom(PLATFORM_ONLY_CAPS), undefined)).toEqual({
      to: "/admin/system",
    });
  });

  it("cluster-admin (contents.read + own cluster) → that cluster's detail", () => {
    expect(resolveAdminLanding(canFrom(CLUSTER_ADMIN_CAPS), "lsi")).toEqual({
      to: "/admin/clusters/$cid",
      cid: "lsi",
    });
  });

  it("cluster-admin with no cluster id falls through to /files", () => {
    // Defensive: contents.read but no surface-routing cluster id.
    expect(resolveAdminLanding(canFrom(CLUSTER_ADMIN_CAPS), undefined)).toEqual({
      to: "/files",
    });
  });

  it("plain user → /files", () => {
    expect(resolveAdminLanding(canFrom(USER_CAPS), undefined)).toEqual({
      to: "/files",
    });
  });

  it("no capabilities (loading / older backend) → /files", () => {
    expect(resolveAdminLanding(canFrom([]), undefined)).toEqual({ to: "/files" });
  });
});
