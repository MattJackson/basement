# basement-mcp

The `basement-mcp` binary is a [Model Context Protocol][mcp] (MCP)
stdio server that exposes basement storage operations as tools an AI
agent can call from inside a Claude / Cursor / Continue session.
Combined with v1.7.0a service-account credentials, it lets you ask
your assistant questions like:

- "List the buckets in the `lsi` region and show me their object
  counts."
- "Mint a 24-hour share link for `quarterly-q1.pdf` in the `reports`
  bucket."
- "Trigger the nightly broadcom backup right now."

The server is read-mostly in v1.8.0c: ten tools cover discovery
(regions / buckets / objects / federations / backups / audit) plus
two write actions (share creation, on-demand backup runs). Full-text
search ships as a placeholder that returns `NOT_IMPLEMENTED` — the
real index lands in v1.9.

## Prerequisites

1. A running basement deployment (the `basement-server` binary from
   the same release).
2. A v1.7.0a service-account with capabilities for whichever tools
   you plan to call. The MCP server inherits the service account's
   permissions — it does **not** define its own role model. Suggested
   starting point: `host:list_*`, `objects:share_create`, and
   `backups:*` on the buckets you want to manage.
3. A profile in `~/.config/basement/config.yaml` (see below).

## Configure a profile

`basement-mcp` reads `~/.config/basement/config.yaml` (or
`$XDG_CONFIG_HOME/basement/config.yaml`, or whatever
`$BASEMENT_CONFIG` points at). The schema is defined in
`internal/clilib`:

```yaml
profiles:
  default:
    endpoint: https://basement.your-domain.com
    access_key_id: BMNT00000000abcd
    secret_key: redacted-bcrypt-output
```

File mode **must** be `0600`. The fastest way to generate the file
is to mint a service account in the web UI at
`/admin/service-accounts/new` and copy the YAML snippet from the
"Use with MCP" card on the shown-once dialog (Download config.yaml
writes the same block with the plaintext secret inlined). If you're
authoring it by hand, `chmod 600 ~/.config/basement/config.yaml`.

CI environments can override the secret without rewriting the file:

```bash
export BASEMENT_SECRET_KEY=…  # secret only — endpoint and AKID still come from disk
export BASEMENT_PROFILE=ci    # pick a profile by env, fall back to "default"
```

## Install

### Build from source

```bash
go build -o basement-mcp ./cmd/basement-mcp/...
sudo install basement-mcp /usr/local/bin/
```

The binary is statically linked Go — no runtime dependencies.

### Smoke-test

The MCP transport is JSON-RPC over stdio. You can hand-drive it
with a heredoc:

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"manual","version":"0"},"capabilities":{}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"basement_list_regions","arguments":{}}}' | basement-mcp
```

Three response frames should come back on stdout, the third
containing your regions catalog.

## Wire to your MCP client

Each client below uses the same JSON shape. Every MCP-aware client
will spawn the binary, write to its stdin, and read its stdout.

### Claude Desktop

Edit the config file (paths per platform):

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "basement": {
      "command": "basement-mcp",
      "args": ["--profile=default"],
      "env": {
        "BASEMENT_PROFILE": "default"
      }
    }
  }
}
```

Restart Claude Desktop. The basement tools appear in the tool
picker (paperclip-icon menu).

### Claude Code (the CLI)

Add an MCP server to your project or user config:

```bash
claude mcp add basement basement-mcp -- --profile=default
```

Or edit `~/.claude/mcp.json` directly with the same JSON shape as
Claude Desktop. Claude Code shows MCP tools in the slash-command
catalog the first time you start a session in the project.

### Cursor

Edit `~/.cursor/mcp.json` (global) or `.cursor/mcp.json` in your
project root:

```json
{
  "mcpServers": {
    "basement": {
      "command": "basement-mcp",
      "args": ["--profile=default"]
    }
  }
}
```

Cursor exposes MCP tools in the Composer agent panel.

## Tool catalog

All tool names are prefixed `basement_` so they sort together in
the agent UI and don't collide with other MCP servers.

| Tool                          | Purpose                                                                  |
|-------------------------------|--------------------------------------------------------------------------|
| `basement_list_regions`       | All regions the service account can see.                                 |
| `basement_list_buckets`       | Buckets inside a region.                                                 |
| `basement_list_objects`       | Objects in a bucket — prefix + delimiter aware.                          |
| `basement_get_object_metadata`| Size / contentType / lastModified / etag for one object.                 |
| `basement_search`             | Placeholder — returns `NOT_IMPLEMENTED` until v1.9.                      |
| `basement_list_backups`       | User-owned backup configurations and last-run status.                    |
| `basement_list_federations`   | Cross-region replication pairs.                                          |
| `basement_list_audit`         | Admin — query the audit log with filters.                                |
| `basement_create_share`       | Mint a public share token for a prefix or single object.                 |
| `basement_create_backup_run`  | Trigger a one-shot backup run (bypasses the cron schedule).              |

Each tool's full input schema is returned by `tools/list`; AI
clients render it automatically.

## Diagnostics

`basement-mcp` writes structured logs to **stderr** — everything on
stdout is reserved for JSON-RPC frames, so any stray print would
corrupt the transport. Claude Code's MCP inspector surfaces stderr
in its tool-output panel; Claude Desktop captures it in
`~/Library/Logs/Claude/mcp-server-basement.log` (macOS).

The `--version` flag prints the build version and exits without
touching the transport — useful for sanity-checking which binary a
host is actually spawning:

```bash
basement-mcp --version
# basement-mcp v1.8.0c
```

## Security notes

- The MCP server holds plaintext bearer credentials in
  `~/.config/basement/config.yaml` (mode 0600). Treat it the same
  way you treat `~/.aws/credentials`.
- Tool calls go out under the **service account's** identity, not
  whatever human is talking to the assistant. Audit events on the
  basement side show `actor=sa:{ID}` for every MCP-driven action.
- Write-side tools (`basement_create_share`, `basement_create_backup_run`)
  succeed only if the underlying service account has the
  corresponding capability. Scope the SA tightly — read-only is a
  safe default.
- LLMs occasionally hallucinate arguments. Tool inputs are
  validated server-side (the same capability gates the web UI
  uses), but if the assistant invents a region ID it'll get an
  `INVALID_REQUEST` back rather than a silent no-op.

## Protocol notes

- Protocol version advertised: `2024-11-05` (the long-stable MCP
  baseline). Newer client versions are accepted via the handshake's
  version-negotiation field.
- Transport: stdio + newline-delimited JSON-RPC 2.0. There is no
  HTTP variant of `basement-mcp`.
- Capabilities advertised: `tools` only. We don't currently expose
  `resources/*` or `prompts/*`; calls to those return
  `Method not found`.

[mcp]: https://modelcontextprotocol.io/specification
