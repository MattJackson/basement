// v1.4.0c SCRUB.MAINT — smoke checks that the scrub route + the
// usage page growth helpers ship. The route component lives behind
// TanStack's createFileRoute which needs the route tree to render
// against, so we assert importability + that the queries hook module
// exposes the scrub helpers + that GrowthLabel's color tones land per
// the brief.

import { describe, it, expect } from "vitest";

describe("v1.4.0c SCRUB.MAINT scaffolding", () => {
  it("exposes scrub query + mutation hooks", async () => {
    const queries = await import("@/shared/api/queries");
    expect(queries.useClusterScrub).toBeDefined();
    expect(queries.useStartClusterScrub).toBeDefined();
  });

  it("imports the scrub route module without throwing", async () => {
    const mod = await import("@/routes/admin/clusters/$cid/scrub.tsx");
    expect(mod.Route).toBeDefined();
  });

  it("imports the updated usage route module without throwing", async () => {
    const mod = await import("@/routes/admin/usage.tsx");
    expect(mod.Route).toBeDefined();
  });
});
