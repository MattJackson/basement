import { Button } from "@/components/ui/button";

// v1.8.0d — "Use with MCP" affordance for service-account surfaces.
//
// Renders the wire-format snippets a basement-mcp user needs to drop
// the service account into their MCP-aware client (Claude Desktop,
// Claude Code, Cursor):
//
//   1. A YAML profile for ~/.config/basement/config.yaml — the file
//      basement-mcp reads via internal/clilib (see
//      cmd/basement-mcp/README.md "Configure a profile"). The shape
//      matches `clilib.Config` verbatim so an operator can copy this
//      block straight into the file.
//   2. A JSON snippet for the host's MCP-server config that points at
//      the basement-mcp binary + the YAML file via $BASEMENT_CONFIG.
//
// Two call-sites:
//   - SecretShownOnceDialog (mint flow): the plaintext secret is
//     available exactly once, so we inline it into the YAML so the
//     operator has a single-shot copy/download.
//   - /admin/service-accounts/$id (detail page): no plaintext secret
//     ever again, so we render the YAML with a <SECRET_FROM_ROTATE>
//     placeholder + a "Rotate the secret" instruction. The operator
//     still gets the endpoint + AKID baked in so only one field
//     needs hand-editing.
//
// Endpoint defaults to window.location.origin so an operator standing
// in front of basement.example.com gets the right URL without typing.
// Tests inject endpointOverride to keep the snippets deterministic.

export interface McpConfigSectionProps {
  accessKeyId: string;
  // When null, the YAML renders `<SECRET_FROM_ROTATE>` as the
  // secret_key value + we surface a "rotate to get a fresh value"
  // hint underneath. The detail-page caller always passes null; the
  // mint-dialog caller passes the plaintext.
  secret: string | null;
  // Test seam — production callers omit this and we use
  // window.location.origin.
  endpointOverride?: string;
  // Filename for the downloaded YAML. Defaults to "config.yaml".
  downloadFilename?: string;
}

const SECRET_PLACEHOLDER = "<SECRET_FROM_ROTATE>";

export function McpConfigSection({
  accessKeyId,
  secret,
  endpointOverride,
  downloadFilename = "config.yaml",
}: McpConfigSectionProps) {
  const endpoint =
    endpointOverride ??
    (typeof window !== "undefined" ? window.location.origin : "");

  const secretValue = secret ?? SECRET_PLACEHOLDER;

  // The YAML block matches `clilib.Config` from internal/clilib/config.go
  // — top-level `profiles` map keyed by profile name. We emit a single
  // `default` profile; operators with multiple basements can rename the
  // key + paste additional blocks alongside.
  const yamlSnippet = `profiles:
  default:
    endpoint: ${endpoint}
    access_key_id: ${accessKeyId}
    secret_key: ${secretValue}`;

  // The Claude / Cursor config shape is identical across hosts — see
  // cmd/basement-mcp/README.md "Wire to your MCP client". We point
  // BASEMENT_CONFIG at the same file the YAML snippet above writes,
  // so the two snippets are paired-by-construction.
  const clientSnippet = `{
  "mcpServers": {
    "basement": {
      "command": "basement-mcp",
      "env": {
        "BASEMENT_CONFIG": "/path/to/config.yaml"
      }
    }
  }
}`;

  const handleDownload = () => {
    try {
      const blob = new Blob([yamlSnippet], {
        type: "application/x-yaml;charset=utf-8",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = downloadFilename;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      // Revoking on the next tick lets Safari finish the download
      // before the URL gets pulled from the registry. Empirically a
      // microtask is enough; setTimeout(0) keeps the pattern simple.
      setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch {
      // Download is a nice-to-have on top of the Copy button. The
      // YAML text is still visible on screen if both paths fail.
    }
  };

  return (
    <div className="space-y-3" data-testid="mcp-config-section">
      <div className="text-xs font-medium text-muted-foreground">
        Use with MCP
      </div>
      <p className="text-xs text-muted-foreground">
        Drop this profile into{" "}
        <code className="font-mono">~/.config/basement/config.yaml</code>{" "}
        to use the service account from{" "}
        <code className="font-mono">basement-mcp</code> (Claude Desktop,
        Claude Code, Cursor). See{" "}
        <code className="font-mono">cmd/basement-mcp/README.md</code> for
        the per-client wiring.
      </p>

      <div>
        <div className="mb-1 flex items-center justify-between gap-2">
          <span className="text-xs font-medium text-muted-foreground">
            config.yaml
          </span>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => copyToClipboard(yamlSnippet)}
              data-testid="mcp-yaml-copy"
            >
              Copy YAML
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={handleDownload}
              data-testid="mcp-yaml-download"
            >
              Download config.yaml
            </Button>
          </div>
        </div>
        <pre
          className="rounded bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-all"
          data-testid="mcp-yaml-snippet"
        >
          {yamlSnippet}
        </pre>
        {secret === null && (
          <p
            className="mt-1 text-xs text-amber-700 dark:text-amber-400"
            data-testid="mcp-rotate-hint"
          >
            The plaintext secret is gone — rotate the secret to get a
            fresh value to drop into <code className="font-mono">{SECRET_PLACEHOLDER}</code>.
          </p>
        )}
      </div>

      <div>
        <div className="mb-1 flex items-center justify-between gap-2">
          <span className="text-xs font-medium text-muted-foreground">
            Claude Desktop / Claude Code / Cursor config
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => copyToClipboard(clientSnippet)}
            data-testid="mcp-client-copy"
          >
            Copy JSON
          </Button>
        </div>
        <pre
          className="rounded bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-all"
          data-testid="mcp-client-snippet"
        >
          {clientSnippet}
        </pre>
      </div>
    </div>
  );
}

// copyToClipboard mirrors the helper in SecretShownOnceDialog: best-
// effort, never throws — the on-screen snippet is the source of truth
// if the clipboard API is unavailable (sandboxed iframe, etc.).
function copyToClipboard(value: string) {
  try {
    void navigator.clipboard.writeText(value);
  } catch {
    // No-op; visible text is the fallback.
  }
}
