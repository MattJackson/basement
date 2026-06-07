// ADR-0009 — route → capability resolution.
//
// Verifies longest-prefix matching so the contents sub-pages
// (cluster.contents.read) win over the cluster-detail rule
// (cluster.wiring.read), and that unmapped routes return null.

import { describe, it, expect } from "vitest";
import { resolveRequiredCapabilities } from "@/shared/auth/route-capabilities";
import { CAP } from "@/shared/auth/capabilities";

describe("resolveRequiredCapabilities", () => {
  it("maps platform routes to their list/read caps", () => {
    expect(resolveRequiredCapabilities("/admin/system")).toEqual([CAP.PLATFORM_SYSTEM_READ]);
    expect(resolveRequiredCapabilities("/admin/users")).toEqual([CAP.PLATFORM_USERS_LIST]);
    expect(resolveRequiredCapabilities("/admin/policies")).toEqual([CAP.PLATFORM_POLICIES_LIST]);
    expect(resolveRequiredCapabilities("/admin/audit")).toEqual([CAP.PLATFORM_AUDIT_READ]);
  });

  it("cluster list needs wiring.list; new needs wiring.create", () => {
    expect(resolveRequiredCapabilities("/admin/clusters")).toEqual([CAP.CLUSTER_WIRING_LIST]);
    expect(resolveRequiredCapabilities("/admin/clusters/new")).toEqual([CAP.CLUSTER_WIRING_CREATE]);
  });

  it("cluster detail needs only wiring.read (so UI Admin can view it)", () => {
    expect(resolveRequiredCapabilities("/admin/clusters/lsi")).toEqual([CAP.CLUSTER_WIRING_READ]);
  });

  it("cluster edit needs wiring.update", () => {
    expect(resolveRequiredCapabilities("/admin/clusters/lsi/edit")).toEqual([CAP.CLUSTER_WIRING_UPDATE]);
  });

  it("contents sub-pages need contents.read (longest-prefix beats detail rule)", () => {
    expect(resolveRequiredCapabilities("/admin/clusters/lsi/buckets/b1")).toEqual([
      CAP.CLUSTER_CONTENTS_READ,
    ]);
    expect(resolveRequiredCapabilities("/admin/clusters/lsi/keys/k1")).toEqual([
      CAP.CLUSTER_CONTENTS_READ,
    ]);
  });

  it("layout + scrub map to their write caps", () => {
    expect(resolveRequiredCapabilities("/admin/clusters/lsi/layout")).toEqual([CAP.CLUSTER_LAYOUT_WRITE]);
    expect(resolveRequiredCapabilities("/admin/clusters/lsi/scrub")).toEqual([CAP.CLUSTER_SCRUB_WRITE]);
  });

  it("lifecycle pages need lifecycle.write", () => {
    expect(resolveRequiredCapabilities("/admin/clusters/lsi/buckets/b1/lifecycle/new")).toEqual([
      CAP.CLUSTER_BUCKETS_LIFECYCLE_WRITE,
    ]);
    expect(resolveRequiredCapabilities("/admin/clusters/lsi/buckets/b1/lifecycle/r1/edit")).toEqual([
      CAP.CLUSTER_BUCKETS_LIFECYCLE_WRITE,
    ]);
  });

  it("aggregate pages map to aggregate caps", () => {
    expect(resolveRequiredCapabilities("/admin/buckets")).toEqual([CAP.CLUSTER_BUCKETS_AGGREGATE]);
    expect(resolveRequiredCapabilities("/admin/usage")).toEqual([CAP.CLUSTER_USAGE_AGGREGATE]);
  });

  it("returns null for unmapped routes", () => {
    expect(resolveRequiredCapabilities("/files")).toBeNull();
    expect(resolveRequiredCapabilities("/admin/totally-new-page")).toBeNull();
  });
});
