// v1.10.0c — ObjectVersionsPanel: per-version Object Lock UI.
//
// Coverage:
//   - retention badge renders for versions with active retention
//   - legal hold badge renders when legal hold is on
//   - Delete button is disabled for compliance retention OR legal hold
//   - "Set retention" + "Set hold" actions only render when the bucket
//     has Object Lock enabled and the row isn't a delete-marker

import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, afterEach, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ObjectVersionsPanel } from "@/shared/ui/ObjectVersionsPanel";

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function Wrapper({
  client,
  children,
}: {
  client: QueryClient;
  children: React.ReactNode;
}) {
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

type FetchCase = {
  versions: Array<{
    versionId: string;
    key: string;
    size: number;
    isLatest: boolean;
    isDeleteMarker?: boolean;
    lastModified?: string;
  }>;
  objectLock: { enabled: boolean; supported: boolean };
  retentions: Record<string, { mode: string; retainUntilDate: string } | null>;
  legalHolds: Record<string, boolean>;
};

function installFetch(c: FetchCase) {
  globalThis.fetch = vi.fn(async (url: RequestInfo | URL) => {
    const s = String(url);
    if (s.includes("/versions") && !s.includes("/retention") && !s.includes("/legal-hold")) {
      return new Response(JSON.stringify({ versions: c.versions }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    if (s.includes("/object-lock") && !s.includes("/o/")) {
      return new Response(JSON.stringify(c.objectLock), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    if (s.includes("/retention")) {
      const versionId = new URL(s, "http://x").searchParams.get("versionId") ?? "";
      const ret = c.retentions[versionId];
      return new Response(JSON.stringify({ retention: ret ?? null }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    if (s.includes("/legal-hold")) {
      const versionId = new URL(s, "http://x").searchParams.get("versionId") ?? "";
      const on = !!c.legalHolds[versionId];
      return new Response(JSON.stringify({ on }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;
}

describe("ObjectVersionsPanel — Object Lock per-version UI (v1.10.0c)", () => {
  it("renders the compliance retention badge AND disables delete", async () => {
    const future = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString();
    installFetch({
      versions: [
        { versionId: "v1", key: "k1", size: 10, isLatest: true },
      ],
      objectLock: { enabled: true, supported: true },
      retentions: { v1: { mode: "COMPLIANCE", retainUntilDate: future } },
      legalHolds: {},
    });

    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectVersionsPanel
          regionId="r1"
          bucket="b1"
          objectKey="k1"
          onClose={() => {}}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-version-retention-badge-v1")).toBeInTheDocument();
    });
    expect(screen.getByTestId("object-version-retention-badge-v1").textContent).toMatch(/Compliance/);

    // Delete button must be disabled due to active compliance retention.
    const delBtn = screen.getByTestId("object-version-delete-v1");
    expect(delBtn).toBeDisabled();
  });

  it("renders the legal-hold badge and disables delete when hold is on", async () => {
    installFetch({
      versions: [
        { versionId: "v1", key: "k1", size: 10, isLatest: true },
      ],
      objectLock: { enabled: true, supported: true },
      retentions: {},
      legalHolds: { v1: true },
    });

    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectVersionsPanel
          regionId="r1"
          bucket="b1"
          objectKey="k1"
          onClose={() => {}}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-version-legal-hold-badge-v1")).toBeInTheDocument();
    });
    expect(screen.getByTestId("object-version-delete-v1")).toBeDisabled();
  });

  it("DOES NOT render the Set retention / Set hold actions when Object Lock isn't enabled on the bucket", async () => {
    installFetch({
      versions: [
        { versionId: "v1", key: "k1", size: 10, isLatest: true },
      ],
      // Driver supports Object Lock but the bucket doesn't have it on yet.
      objectLock: { enabled: false, supported: true },
      retentions: {},
      legalHolds: {},
    });

    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectVersionsPanel
          regionId="r1"
          bucket="b1"
          objectKey="k1"
          onClose={() => {}}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-version-row-v1")).toBeInTheDocument();
    });
    expect(
      screen.queryByTestId("object-version-retention-action-v1"),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByTestId("object-version-legal-hold-toggle-v1"),
    ).not.toBeInTheDocument();
    // Delete must still work — bucket has no lock, so the button is enabled.
    expect(screen.getByTestId("object-version-delete-v1")).not.toBeDisabled();
  });

  it("renders the Set retention + Set hold actions when Object Lock is enabled", async () => {
    installFetch({
      versions: [
        { versionId: "v1", key: "k1", size: 10, isLatest: true },
      ],
      objectLock: { enabled: true, supported: true },
      retentions: {},
      legalHolds: {},
    });

    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectVersionsPanel
          regionId="r1"
          bucket="b1"
          objectKey="k1"
          onClose={() => {}}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-version-retention-action-v1")).toBeInTheDocument();
    });
    expect(screen.getByTestId("object-version-legal-hold-toggle-v1")).toBeInTheDocument();
    // No retention / no hold => delete enabled.
    expect(screen.getByTestId("object-version-delete-v1")).not.toBeDisabled();
  });
});
