import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import { McpConfigSection } from "@/shared/ui/McpConfigSection";

// v1.8.0d — Coverage for the "Use with MCP" affordance shared by the
// SecretShownOnceDialog and the /admin/service-accounts/$id detail
// page.
//
// Two render modes:
//   - secret: string (mint flow) → YAML inlines the plaintext, no
//     rotate hint.
//   - secret: null (detail flow) → YAML carries <SECRET_FROM_ROTATE>
//     placeholder + rotate hint surfaces.

const writeText = vi.fn().mockResolvedValue(undefined);

beforeEach(() => {
  vi.clearAllMocks();
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
});

describe("McpConfigSection — mint flow (secret supplied)", () => {
  it("renders YAML with the plaintext secret inlined and no rotate hint", () => {
    render(
      <McpConfigSection
        accessKeyId="BMNT1111222233334444"
        secret="s3cret_plaintext"
        endpointOverride="https://basement.example.com"
      />,
    );

    const yaml = screen.getByTestId("mcp-yaml-snippet").textContent ?? "";
    expect(yaml).toContain("profiles:");
    expect(yaml).toContain("default:");
    expect(yaml).toContain("endpoint: https://basement.example.com");
    expect(yaml).toContain("access_key_id: BMNT1111222233334444");
    expect(yaml).toContain("secret_key: s3cret_plaintext");

    // Rotate hint only shows on the detail-page (secret=null) variant.
    expect(screen.queryByTestId("mcp-rotate-hint")).not.toBeInTheDocument();
  });

  it("renders the Claude / Cursor JSON snippet pointing at $BASEMENT_CONFIG", () => {
    render(
      <McpConfigSection
        accessKeyId="BMNT0001"
        secret="x"
        endpointOverride="https://b.example"
      />,
    );

    const json = screen.getByTestId("mcp-client-snippet").textContent ?? "";
    expect(json).toContain("\"mcpServers\"");
    expect(json).toContain("\"basement\"");
    expect(json).toContain("\"command\": \"basement-mcp\"");
    expect(json).toContain("\"BASEMENT_CONFIG\"");
  });

  it("Copy YAML writes the full YAML block to the clipboard", async () => {
    render(
      <McpConfigSection
        accessKeyId="BMNT0001"
        secret="topsecret"
        endpointOverride="https://b.example"
      />,
    );

    await userEvent.click(screen.getByTestId("mcp-yaml-copy"));
    expect(writeText).toHaveBeenLastCalledWith(
      `profiles:
  default:
    endpoint: https://b.example
    access_key_id: BMNT0001
    secret_key: topsecret`,
    );
  });

  it("Copy JSON writes the client config to the clipboard", async () => {
    render(
      <McpConfigSection
        accessKeyId="BMNT0001"
        secret="topsecret"
        endpointOverride="https://b.example"
      />,
    );

    await userEvent.click(screen.getByTestId("mcp-client-copy"));
    expect(writeText).toHaveBeenLastCalledWith(
      `{
  "mcpServers": {
    "basement": {
      "command": "basement-mcp",
      "env": {
        "BASEMENT_CONFIG": "/path/to/config.yaml"
      }
    }
  }
}`,
    );
  });

  it("Download config.yaml triggers an anchor-click with the right filename", async () => {
    // Stub URL.createObjectURL / revokeObjectURL — jsdom doesn't ship
    // them. The component reads the result back to feed the anchor's
    // href; the download attribute is what we actually assert against.
    const createObjectURL = vi.fn().mockReturnValue("blob:fake-url");
    const revokeObjectURL = vi.fn();
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: createObjectURL,
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: revokeObjectURL,
    });

    // Intercept anchor clicks so the test doesn't try to navigate.
    const anchorClicks: HTMLAnchorElement[] = [];
    const realAppend = document.body.appendChild.bind(document.body);
    const appendSpy = vi
      .spyOn(document.body, "appendChild")
      .mockImplementation(((node: Node) => {
        if (node instanceof HTMLAnchorElement) {
          anchorClicks.push(node);
          // Stub the click so jsdom doesn't try to navigate.
          node.click = vi.fn();
        }
        return realAppend(node);
      }) as typeof document.body.appendChild);

    render(
      <McpConfigSection
        accessKeyId="BMNT0001"
        secret="topsecret"
        endpointOverride="https://b.example"
        downloadFilename="my-config.yaml"
      />,
    );

    await userEvent.click(screen.getByTestId("mcp-yaml-download"));

    expect(createObjectURL).toHaveBeenCalledTimes(1);
    const blob = createObjectURL.mock.calls[0]![0] as Blob;
    expect(blob).toBeInstanceOf(Blob);
    expect(blob.type).toContain("yaml");

    expect(anchorClicks).toHaveLength(1);
    expect(anchorClicks[0]!.download).toBe("my-config.yaml");

    appendSpy.mockRestore();
  });
});

describe("McpConfigSection — detail flow (secret = null)", () => {
  it("renders the placeholder in the YAML and surfaces a rotate hint", () => {
    render(
      <McpConfigSection
        accessKeyId="BMNT9999"
        secret={null}
        endpointOverride="https://b.example"
      />,
    );

    const yaml = screen.getByTestId("mcp-yaml-snippet").textContent ?? "";
    expect(yaml).toContain("secret_key: <SECRET_FROM_ROTATE>");
    expect(yaml).not.toContain("topsecret");

    const hint = screen.getByTestId("mcp-rotate-hint");
    expect(hint).toBeInTheDocument();
    expect(hint.textContent).toMatch(/rotate/i);
  });

  it("Copy YAML still works even though the secret is a placeholder", async () => {
    render(
      <McpConfigSection
        accessKeyId="BMNT9999"
        secret={null}
        endpointOverride="https://b.example"
      />,
    );

    await userEvent.click(screen.getByTestId("mcp-yaml-copy"));
    expect(writeText).toHaveBeenLastCalledWith(
      `profiles:
  default:
    endpoint: https://b.example
    access_key_id: BMNT9999
    secret_key: <SECRET_FROM_ROTATE>`,
    );
  });
});
