// Tests for the /files/regions/new → /files/keys/new redirect (v1.2.0d).
//
// The legacy URL is kept as a route so in-flight bookmarks + the old
// empty-state CTA keep working. beforeLoad throws redirect() so the
// user never sees a frame of stale chrome. Asserts a single thing:
// loading the route raises a redirect targeted at /files/keys/new.

import { describe, it, expect, vi } from "vitest";

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    createFileRoute: (_path: string) => (options: any) => options,
    redirect: (opts: any) => {
      const err = new Error(`redirect:${opts.to}`);
      (err as any).__redirect = opts;
      return err;
    },
  };
});

import { Route } from "@/routes/files/regions/new";

describe("/files/regions/new (legacy redirect)", () => {
  it("redirects to /files/keys/new when the route loads", () => {
    // createFileRoute is mocked to echo its options object, so Route
    // exposes the beforeLoad we registered.
    const opts = Route as unknown as {
      beforeLoad: () => void;
    };

    expect(typeof opts.beforeLoad).toBe("function");

    let thrown: any;
    try {
      opts.beforeLoad();
    } catch (e) {
      thrown = e;
    }

    expect(thrown).toBeDefined();
    expect(thrown.__redirect).toEqual({
      to: "/files/keys/new",
      replace: true,
    });
  });
});
