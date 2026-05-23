// v1.9.0d GATEWAYS card — registry-driven smoke tests.
//
// Covers what the cycle spec calls out:
//   1. The card renders one row per registry entry (5 rows from the
//      stub roster: webdav implemented + smb/nfs/ftp/s3 stubs).
//   2. WebDAV row shows the enable toggle; stubs show "coming soon"
//      and no toggle.
//   3. Per-row capability chips render from the registry data.
//   4. Toggle round-trips through onChange + persists via onSave.
//   5. Loading + error states render distinct affordances so the
//      operator sees more than a blank card when /admin/gateways
//      stalls or 503s.
//
// We mock the useGatewaysRegistry hook so tests don't depend on a
// live backend, and stub @tanstack/react-router's Link so the card
// renders without a RouterProvider.

import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

vi.mock("@tanstack/react-router", async () => {
  const actual = await vi.importActual<
    typeof import("@tanstack/react-router")
  >("@tanstack/react-router");
  return {
    ...actual,
    Link: ({
      to,
      children,
      ...rest
    }: { to: string; children: React.ReactNode } & Record<string, unknown>) => (
      <a href={to} {...rest}>
        {children}
      </a>
    ),
  };
});

// useGatewaysRegistry is the registry hook the card reads; mock it
// per-test via `mockReturnValue` below so each scenario picks its
// own roster shape (loading / error / 5-row roster).
const useGatewaysRegistryMock = vi.fn();
vi.mock("@/shared/api/queries", async () => {
  const actual =
    await vi.importActual<typeof import("@/shared/api/queries")>(
      "@/shared/api/queries",
    );
  return {
    ...actual,
    useGatewaysRegistry: () => useGatewaysRegistryMock(),
  };
});

import { GatewaysCard } from "@/routes/admin/system";
import type { GatewayInfo } from "@/shared/api/queries";

// Build a default roster matching what main.go registers in
// v1.9.0c: webdav (real) + smb / nfs / ftp / s3 (stubs). Sorted by
// name so it lines up with the backend's alphabetical order.
function defaultRoster(): GatewayInfo[] {
  return [
    {
      name: "ftp",
      displayName: "FTP / SFTP",
      description: "Legacy file transfer.",
      capabilities: {
        read: true,
        write: true,
        delete: true,
        move: false,
        lock: false,
        basicAuth: true,
        bearerAuth: false,
        sigV4Auth: false,
      },
      status: { running: false },
      implemented: false,
      enabled: false,
    },
    {
      name: "nfs",
      displayName: "NFS",
      description: "Unix Network File System.",
      capabilities: {
        read: true,
        write: true,
        delete: true,
        move: false,
        lock: false,
        basicAuth: false,
        bearerAuth: false,
        sigV4Auth: false,
      },
      status: { running: false },
      implemented: false,
      enabled: false,
    },
    {
      name: "s3",
      displayName: "S3 API",
      description: "Native S3 endpoint with SigV4.",
      capabilities: {
        read: true,
        write: true,
        delete: true,
        move: true,
        lock: false,
        basicAuth: false,
        bearerAuth: false,
        sigV4Auth: true,
      },
      status: { running: false },
      implemented: false,
      enabled: false,
    },
    {
      name: "smb",
      displayName: "SMB / CIFS",
      description: "Native Windows + macOS file shares.",
      capabilities: {
        read: true,
        write: true,
        delete: true,
        move: true,
        lock: false,
        basicAuth: true,
        bearerAuth: false,
        sigV4Auth: false,
      },
      status: { running: false },
      implemented: false,
      enabled: false,
    },
    {
      name: "webdav",
      displayName: "WebDAV",
      description: "HTTP-mounted gateway.",
      capabilities: {
        read: true,
        write: true,
        delete: true,
        move: true,
        lock: false,
        basicAuth: true,
        bearerAuth: true,
        sigV4Auth: false,
      },
      status: { running: true, totalRequests: 42, activeConnections: 0 },
      implemented: true,
      enabled: true,
    },
  ];
}

function mockRegistry(opts: {
  data?: GatewayInfo[];
  isLoading?: boolean;
  isError?: boolean;
  errorMessage?: string;
}) {
  useGatewaysRegistryMock.mockReturnValue({
    data: opts.data,
    isLoading: opts.isLoading ?? false,
    isError: opts.isError ?? false,
    error: opts.isError ? new Error(opts.errorMessage ?? "boom") : null,
  });
}

describe("GatewaysCard (v1.9.0d)", () => {
  it("renders implemented gateways first, then stubs alphabetically (v1.11.0.19)", () => {
    mockRegistry({ data: defaultRoster() });
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );

    expect(screen.getByText("Gateways")).toBeInTheDocument();
    // All five rows render.
    for (const name of ["ftp", "nfs", "s3", "smb", "webdav"]) {
      expect(
        screen.getByTestId(`gateways-${name}-section`),
      ).toBeInTheDocument();
    }

    // v1.11.0.19: webdav (implemented) sorts first; stubs follow in
    // alpha order. Pin the order by reading the rendered sections in
    // DOM order and comparing names.
    const sections = screen.getAllByTestId(/^gateways-[a-z0-9]+-section$/);
    const names = sections.map((el) =>
      (el.getAttribute("data-testid") ?? "")
        .replace(/^gateways-/, "")
        .replace(/-section$/, ""),
    );
    expect(names).toEqual(["webdav", "ftp", "nfs", "s3", "smb"]);
  });

  it("WebDAV row shows enable toggle + capability chips", () => {
    mockRegistry({ data: defaultRoster() });
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );

    // Toggle present + checked.
    const toggle = screen.getByTestId("gateways-webdav-enabled");
    expect(toggle).toBeChecked();

    // Implemented badge.
    const badges = screen.getAllByTestId("gateway-badge-implemented");
    expect(badges.length).toBe(1);

    // Capabilities row includes "read", "write", "bearer-auth".
    const caps = screen.getByTestId("gateways-webdav-capabilities");
    expect(caps).toHaveTextContent("read");
    expect(caps).toHaveTextContent("write");
    expect(caps).toHaveTextContent("bearer-auth");

    // Mount URL renders to /webdav on the origin.
    expect(screen.getByTestId("gateways-webdav-mount-url")).toHaveValue(
      `${window.location.origin}/webdav`,
    );
  });

  it("stub rows show coming-soon badge and hide the enable toggle", () => {
    mockRegistry({ data: defaultRoster() });
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );

    // Four stubs → four coming-soon badges.
    expect(
      screen.getAllByTestId("gateway-badge-coming-soon").length,
    ).toBe(4);

    // No enable toggle for stubs; the row renders the em-dash
    // placeholder instead.
    for (const name of ["ftp", "nfs", "s3", "smb"]) {
      expect(screen.queryByTestId(`gateways-${name}-enabled`)).toBeNull();
      expect(
        screen.getByTestId(`gateways-${name}-no-toggle`),
      ).toBeInTheDocument();
    }
  });

  it("stub rows omit the tracking docs link and version-commit copy (v1.11.0.20)", () => {
    // Per feedback_dont_announce_v2 the stub rows no longer link to
    // a docs/integrations/<name>.md tracking page (those docs were
    // also scrubbed of "planned for v2.x" copy in the same cycle).
    // The coming-soon badge + capability chips carry the message
    // for unimplemented protocols.
    mockRegistry({ data: defaultRoster() });
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );

    for (const name of ["ftp", "nfs", "s3", "smb"]) {
      // No docs link rendered for stubs.
      expect(screen.queryByTestId(`gateways-${name}-docs`)).toBeNull();

      const section = screen.getByTestId(`gateways-${name}-section`);
      // No "Implementation planned" / "planned for v" / "implementation tracking" copy.
      expect(section.textContent ?? "").not.toMatch(/Implementation planned/i);
      expect(section.textContent ?? "").not.toMatch(/planned for v/i);
      expect(section.textContent ?? "").not.toMatch(/implementation tracking/i);
    }

    // WebDAV (implemented) still surfaces its integration-guide link.
    expect(screen.getByTestId("gateways-webdav-docs")).toBeInTheDocument();
  });

  it("toggling WebDAV off writes BOTH the legacy webdav field and the protocols map", () => {
    mockRegistry({ data: defaultRoster() });
    const onChange = vi.fn();
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
        onChange={onChange}
        onSave={async () => {}}
      />,
    );

    fireEvent.click(screen.getByTestId("gateways-webdav-enabled"));

    expect(onChange).toHaveBeenCalledTimes(1);
    const next = onChange.mock.calls[0]?.[0];
    // Legacy webdav field flips — this is what v1.9.0b's backend
    // tie-break consumes, and it must agree with the new map.
    expect(next.webdav.enabled).toBe(false);
    // Generic protocols map flips too, keyed by gateway Name().
    expect(next.protocols.webdav.enabled).toBe(false);
  });

  it("Save button on the WebDAV row calls onSave", async () => {
    mockRegistry({ data: defaultRoster() });
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
        onChange={() => {}}
        onSave={onSave}
      />,
    );

    fireEvent.click(screen.getByTestId("gateways-webdav-save"));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
  });

  it("renders a loading affordance while the registry call is in flight", () => {
    mockRegistry({ isLoading: true });
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true } }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );
    expect(screen.getByTestId("gateways-loading")).toBeInTheDocument();
  });

  it("renders an error banner when /admin/gateways fails", () => {
    mockRegistry({ isError: true, errorMessage: "GATEWAYS_NOT_WIRED: nope" });
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true } }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );
    const banner = screen.getByTestId("gateways-error");
    expect(banner).toHaveTextContent(/Gateway registry unavailable/i);
    expect(banner).toHaveTextContent(/GATEWAYS_NOT_WIRED/);
  });

  // v1.11.0.21 polish — hero/stub visual split.
  //
  // The card now renders implemented rows as the hero (status pill,
  // mount URL, connect hints, save) and stubs in a muted single-line
  // treatment. These tests pin the contracts the polish depends on so
  // a future cycle can't regress them without noticing:
  //
  //   - Implemented row carries a status pill that reads "Active" when
  //     the gateway is running, "Stopped" when not.
  //   - Stub row keeps the "not implemented yet" status copy so the
  //     em-dash placeholder + status testid still anchor it.
  //   - Save button label is "Save" (no protocol name) — the row's
  //     heading already names the gateway.
  //   - Stubs no longer carry the verbose StatusBlock prose run-on.
  describe("v1.11.0.21 polish", () => {
    it("WebDAV status pill reads Active when the gateway is running", () => {
      mockRegistry({ data: defaultRoster() });
      render(
        <GatewaysCard
          gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
          onChange={() => {}}
          onSave={async () => {}}
        />,
      );
      const pill = screen.getByTestId("gateways-webdav-status");
      expect(pill).toHaveTextContent(/Active/i);
      // Old StatusBlock prose ("running, X active connections, last
      // activity ...") must not survive the cleanup.
      expect(pill.textContent ?? "").not.toMatch(/active connections/i);
      expect(pill.textContent ?? "").not.toMatch(/total requests/i);
    });

    it("WebDAV status pill reads Stopped when the gateway is not running", () => {
      const roster = defaultRoster().map((g) =>
        g.name === "webdav" ? { ...g, status: { running: false } } : g,
      );
      mockRegistry({ data: roster });
      render(
        <GatewaysCard
          gateways={{ webdav: { enabled: false }, protocols: { webdav: { enabled: false } } }}
          onChange={() => {}}
          onSave={async () => {}}
        />,
      );
      const pill = screen.getByTestId("gateways-webdav-status");
      expect(pill).toHaveTextContent(/Stopped/i);
    });

    it("save button on the WebDAV row reads just 'Save'", () => {
      mockRegistry({ data: defaultRoster() });
      render(
        <GatewaysCard
          gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
          onChange={() => {}}
          onSave={async () => {}}
        />,
      );
      const save = screen.getByTestId("gateways-webdav-save");
      expect(save).toHaveTextContent(/^Save$/);
    });

    it("stub rows keep the 'not implemented yet' status placeholder", () => {
      mockRegistry({ data: defaultRoster() });
      render(
        <GatewaysCard
          gateways={{ webdav: { enabled: true }, protocols: { webdav: { enabled: true } } }}
          onChange={() => {}}
          onSave={async () => {}}
        />,
      );
      for (const name of ["ftp", "nfs", "s3", "smb"]) {
        const status = screen.getByTestId(`gateways-${name}-status`);
        expect(status).toHaveTextContent(/not implemented yet/i);
      }
    });
  });
});
