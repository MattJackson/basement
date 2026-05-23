// v1.10.0c — ObjectLockSection coverage.
//
// Three branches:
//   - driver doesn't support Object Lock → "Unsupported" notice
//   - driver supports it but bucket versioning is not enabled →
//     "Versioning required" notice
//   - both supported + versioning enabled → full editor renders

import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { ObjectLockSection } from "@/shared/ui/ObjectLockSection";

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

beforeEach(() => {
  // Default: no responses. Each test overrides per case.
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.restoreAllMocks();
});

function mockObjectLockFetch(payload: {
  enabled: boolean;
  supported: boolean;
  defaultRetention?: { mode: string; retainUntilDate: string } | null;
}) {
  globalThis.fetch = vi.fn(async (url: RequestInfo | URL) => {
    const s = String(url);
    if (s.includes("/object-lock")) {
      return new Response(JSON.stringify(payload), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response("not found", { status: 404 });
  }) as typeof fetch;
}

describe("ObjectLockSection (v1.10.0c)", () => {
  it("renders the unsupported notice when the driver doesn't support Object Lock", async () => {
    mockObjectLockFetch({ enabled: false, supported: false });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectLockSection
          regionId="r1"
          bucket="b1"
          versioningState={{ status: "enabled", supported: true }}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-lock-unsupported")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("object-lock-save")).not.toBeInTheDocument();
  });

  it("renders the 'needs versioning' notice when versioning isn't enabled", async () => {
    mockObjectLockFetch({ enabled: false, supported: true });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectLockSection
          regionId="r1"
          bucket="b1"
          versioningState={{ status: "suspended", supported: true }}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-lock-needs-versioning")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("object-lock-save")).not.toBeInTheDocument();
  });

  it("renders the full editor and a Save button when supported + versioning enabled", async () => {
    mockObjectLockFetch({ enabled: false, supported: true });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectLockSection
          regionId="r1"
          bucket="b1"
          versioningState={{ status: "enabled", supported: true }}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-lock-save")).toBeInTheDocument();
    });
    expect(screen.getByTestId("object-lock-status-badge")).toHaveTextContent(/Disabled/i);
    // The default-retention toggle and the day/mode fields stay hidden
    // until the operator opts in.
    expect(screen.queryByTestId("object-lock-mode-select")).not.toBeInTheDocument();
  });

  it("opening the default-retention block reveals mode + days fields", async () => {
    mockObjectLockFetch({ enabled: false, supported: true });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectLockSection
          regionId="r1"
          bucket="b1"
          versioningState={{ status: "enabled", supported: true }}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-lock-default-retention-toggle")).toBeInTheDocument();
    });

    await userEvent.click(screen.getByTestId("object-lock-default-retention-toggle"));

    expect(screen.getByTestId("object-lock-mode-select")).toBeInTheDocument();
    expect(screen.getByTestId("object-lock-days-input")).toBeInTheDocument();
  });

  it("reflects the existing enabled config on initial load", async () => {
    const future = new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString();
    mockObjectLockFetch({
      enabled: true,
      supported: true,
      defaultRetention: { mode: "GOVERNANCE", retainUntilDate: future },
    });
    const client = makeQueryClient();
    render(
      <Wrapper client={client}>
        <ObjectLockSection
          regionId="r1"
          bucket="b1"
          versioningState={{ status: "enabled", supported: true }}
        />
      </Wrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId("object-lock-status-badge")).toHaveTextContent(/Enabled/i);
    });
    // Default retention should be reflected.
    const toggle = screen.getByTestId("object-lock-default-retention-toggle") as HTMLInputElement;
    expect(toggle.checked).toBe(true);
    expect(screen.getByTestId("object-lock-mode-select")).toHaveValue("GOVERNANCE");
  });
});
