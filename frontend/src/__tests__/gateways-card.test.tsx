// v1.9.0b GATEWAYS card — FE smoke tests.
//
// Covers the three things the cycle spec calls out:
//   1. The card renders with WebDAV section + the SMB explainer.
//   2. The Enabled toggle round-trips through onChange.
//   3. The Copy button writes the displayed URL to the clipboard.
//
// We mock window.location.origin so the test is independent of where
// vitest is hosting the JSDOM document, and stub navigator.clipboard
// so the Copy assertion doesn't depend on a real browser API.

import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

// Stub the @tanstack/react-router Link so the card renders without a
// RouterProvider in the test tree. The mock has to land BEFORE the
// system.tsx import, hence the static placement at module scope.
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

import { GatewaysCard } from "@/routes/admin/system";

describe("GatewaysCard (v1.9.0b)", () => {
  it("renders the WebDAV section + SMB not-supported explainer", () => {
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true } }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );

    // Card heading + the WebDAV sub-section heading.
    expect(screen.getByText("Gateways")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "WebDAV" })).toBeInTheDocument();

    // SMB section is explicit about not-supported.
    expect(
      screen.getByRole("heading", { name: /SMB .* not supported natively/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", {
        name: /Read the Time Machine integration notes/i,
      }),
    ).toHaveAttribute("href", "/docs/integrations/time-machine.md");

    // Mount URL displays origin + /webdav by default.
    const url = screen.getByTestId("gateways-webdav-mount-url");
    expect(url).toHaveValue(`${window.location.origin}/webdav`);

    // Enabled toggle is on by default.
    expect(screen.getByTestId("gateways-webdav-enabled")).toBeChecked();
  });

  it("flipping the Enabled toggle calls onChange with the new shape", () => {
    const onChange = vi.fn();
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true } }}
        onChange={onChange}
        onSave={async () => {}}
      />,
    );

    fireEvent.click(screen.getByTestId("gateways-webdav-enabled"));

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange.mock.calls[0]?.[0]).toEqual({
      webdav: { enabled: false },
    });
  });

  it("uses the BaseURL override when set instead of the origin", () => {
    render(
      <GatewaysCard
        gateways={{
          webdav: {
            enabled: true,
            baseUrl: "https://files.example.test/webdav",
          },
        }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );

    expect(screen.getByTestId("gateways-webdav-mount-url")).toHaveValue(
      "https://files.example.test/webdav",
    );
  });

  it("Copy button writes the displayed URL to the clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
      writable: true,
    });

    render(
      <GatewaysCard
        gateways={{
          webdav: {
            enabled: true,
            baseUrl: "https://files.example.test/webdav",
          },
        }}
        onChange={() => {}}
        onSave={async () => {}}
      />,
    );

    fireEvent.click(screen.getByTestId("gateways-webdav-copy"));

    await waitFor(() =>
      expect(writeText).toHaveBeenCalledWith(
        "https://files.example.test/webdav",
      ),
    );

    // The button label flips to "Copied" for the brief confirmation
    // window; we don't assert the flip-back since the timer is async.
    await waitFor(() =>
      expect(screen.getByTestId("gateways-webdav-copy")).toHaveTextContent(
        /Copied/i,
      ),
    );
  });

  it("Save button calls onSave", async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    render(
      <GatewaysCard
        gateways={{ webdav: { enabled: true } }}
        onChange={() => {}}
        onSave={onSave}
      />,
    );

    fireEvent.click(screen.getByTestId("gateways-webdav-save"));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
  });
});
