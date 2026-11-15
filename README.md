# unkindled-mcp

A small, standalone [Model Context Protocol](https://modelcontextprotocol.io/) (MCP) server for
[Unkindled](https://unkindled.dev) workstations. It exposes workstation inspection, lifecycle
control, checkpoints, events, and connection details to Claude, Claude Code, Codex, Cursor,
OpenCode, or any MCP client over stdio.

The server is written in Go using only the standard library. It is a single static binary with no
runtime dependencies, and it talks to the existing Unkindled JSON API with bearer authentication.

## Features

- Stdio MCP server with newline-delimited JSON-RPC 2.0.
- Eleven tools covering workstations, connections, lifecycle, checkpoints, and events.
- Explicit tool input schemas with `additionalProperties: false`.
- Destructive operations require an explicit boolean `confirm: true`.
- Compact JSON results plus a short human-readable summary, including HTTP status codes.
- Readable API and tool errors returned as MCP tool errors (`isError: true`), never server crashes.
- Credential redaction and defense-in-depth filtering of private key and provider secret fields.
- Configurable API URL, token, and HTTP timeout.
- Server starts without a token so the MCP client can display a useful configuration error.
- Multi-stage distroless Docker image.

## Requirements

- Go 1.22 or newer to build from source.
- An Unkindled API token with access to the workstations you want to manage.
- An MCP client that supports stdio servers.

## Install

### `go install`

```sh
go install github.com/arthuRHD/unkindled-mcp/cmd/unkindled-mcp@latest
```

Make sure `$(go env GOPATH)/bin` is on your `PATH`. To pin a version, use `@v0.1.0` (once tagged) or
build from a checkout.

### Build from source

```sh
git clone https://github.com/arthuRHD/unkindled-mcp.git
cd unkindled-mcp
make build
./bin/unkindled-mcp
```

The binary speaks MCP on stdin/stdout. Running it directly without input is expected to wait for a
client. Use an absolute binary path in client configuration.

### Docker

```sh
make docker
# or
docker build -t unkindled-mcp:dev .
```

The runtime stage is `gcr.io/distroless/static-debian12:nonroot`: a static binary running as a
non-root user, with CA certificates included for HTTPS.

## Configuration

Configuration comes from environment variables:

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `UNKINDLED_API_URL` | No | `https://api.unkindled.dev` | Base URL of the Unkindled API. |
| `UNKINDLED_API_TOKEN` | Yes, for tool calls | — | Bearer token for the Unkindled API. |
| `UNKINDLED_HTTP_TIMEOUT` | No | `30s` | Per-request HTTP timeout. Accepts Go duration syntax such as `10s`, `1m`, or `1m30s`, or a whole number of seconds such as `45`. |

If `UNKINDLED_API_TOKEN` is missing, the server still completes `initialize`, so the client can show
the problem. Every tool call then returns a clear configuration error, and no request is sent to the
API. An invalid timeout produces a warning on stderr and falls back to 30 seconds.

Logs go to stderr only. Stdout is reserved for MCP protocol messages, and the token is never logged
or included in responses.

Pass the token through the MCP client's environment configuration, not through command-line
arguments (which may be visible in process listings).

## Client setup

The examples use `/usr/local/bin/unkindled-mcp` as the binary path. Replace it with the absolute path
returned by `command -v unkindled-mcp`, your `go install` location, or a source build such as
`/home/you/unkindled-mcp/bin/unkindled-mcp`.

### Claude Desktop

Edit the Claude Desktop configuration file:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "unkindled": {
      "command": "/usr/local/bin/unkindled-mcp",
      "env": {
        "UNKINDLED_API_TOKEN": "replace-with-your-token",
        "UNKINDLED_API_URL": "https://api.unkindled.dev",
        "UNKINDLED_HTTP_TIMEOUT": "30s"
      }
    }
  }
}
```

Restart Claude Desktop after saving. The `unkindled` tools should appear in the tools menu.

### Claude Code

Add a project-scoped `.mcp.json` at the repository root:

```json
{
  "mcpServers": {
    "unkindled": {
      "command": "/usr/local/bin/unkindled-mcp",
      "env": {
        "UNKINDLED_API_TOKEN": "${UNKINDLED_API_TOKEN}"
      }
    }
  }
}
```

Claude Code expands `${UNKINDLED_API_TOKEN}` from its environment. Export the variable in the shell
that starts Claude Code, or put the literal token in the `env` block. Do not commit a literal token;
`.mcp.json` is often shared with a repository.

You can also register the server from the CLI:

```sh
claude mcp add --transport stdio unkindled --env UNKINDLED_API_TOKEN="$UNKINDLED_API_TOKEN" -- /usr/local/bin/unkindled-mcp
```

### Cursor

Add the server to the global `~/.cursor/mcp.json`, or to `.cursor/mcp.json` for a project:

```json
{
  "mcpServers": {
    "unkindled": {
      "command": "/usr/local/bin/unkindled-mcp",
      "env": {
        "UNKINDLED_API_TOKEN": "replace-with-your-token"
      }
    }
  }
}
```

Restart Cursor, then enable the server in Cursor's MCP settings if necessary.

### OpenCode

Add an MCP entry to `opencode.json` (project) or `~/.config/opencode/opencode.json` (global):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "unkindled": {
      "type": "local",
      "command": ["/usr/local/bin/unkindled-mcp"],
      "enabled": true,
      "environment": {
        "UNKINDLED_API_TOKEN": "{env:UNKINDLED_API_TOKEN}"
      }
    }
  }
}
```

OpenCode reads `{env:UNKINDLED_API_TOKEN}` from the environment. Add `UNKINDLED_API_URL` or
`UNKINDLED_HTTP_TIMEOUT` to the same `environment` block if needed.

### Any other MCP client

Use a stdio server with:

- Command: `/absolute/path/to/unkindled-mcp`
- Arguments: none
- Environment: `UNKINDLED_API_TOKEN` (and optionally `UNKINDLED_API_URL`,
  `UNKINDLED_HTTP_TIMEOUT`)

### Docker as a client command

With a locally built `unkindled-mcp:dev` image:

```json
{
  "mcpServers": {
    "unkindled": {
      "command": "docker",
      "args": [
        "run",
        "--rm",
        "-i",
        "-e",
        "UNKINDLED_API_TOKEN",
        "unkindled-mcp:dev"
      ],
      "env": {
        "UNKINDLED_API_TOKEN": "replace-with-your-token"
      }
    }
  }
}
```

The `-i` flag is required: MCP uses stdin/stdout. `-e UNKINDLED_API_TOKEN` forwards the token from
the client environment into the container without putting it in the image.

## Tools

| Tool | Method and path | Notes |
| --- | --- | --- |
| `list_workstations` | `GET /v1/workstations` | Optional `include_deleted` boolean. |
| `get_workstation` | `GET /v1/workstations/{id}` | |
| `get_connection` | `GET /v1/workstations/{id}/connection` | SSH/Herdr connection info. Never returns private keys or provider secrets. |
| `start_workstation` | `POST /v1/workstations/{id}/start` | May be asynchronous; HTTP 202 is reported as pending. |
| `stop_workstation` | `POST /v1/workstations/{id}/stop` | May be asynchronous. |
| `restart_workstation` | `POST /v1/workstations/{id}/restart` | May be asynchronous. |
| `create_checkpoint` | `POST /v1/workstations/{id}/checkpoints` | Optional `label` and `reason`. |
| `list_checkpoints` | `GET /v1/workstations/{id}/checkpoints` | |
| `restore_checkpoint` | `POST /v1/workstations/{id}/checkpoints/{checkpoint_id}/restore` | Destructive. Requires `confirm: true`. |
| `delete_workstation` | `DELETE /v1/workstations/{id}` | Destructive. Requires `confirm: true`. |
| `list_events` | `GET /v1/workstations/{id}/events` | Recent workstation events. |

All tools take a flat JSON object. Unknown fields are rejected. Workstation IDs and checkpoint IDs
are strings.

### Result format

Successful calls return two text content blocks:

1. A short summary including the HTTP status, for example:
   `Fetched workstation "ws_123" (HTTP 200 OK).`
2. A compact JSON block containing the API response.

An accepted asynchronous operation reports the pending state, for example
`Start requested for workstation "ws_123" (HTTP 202 Accepted, pending).`, and includes the API's
response JSON (`status`, operation ID, and so on) in the second block.

API failures are tool results with `isError: true`. The text includes the HTTP status code and the
API's error message, for example:

```text
Unkindled API returned HTTP 404: workstation ws_123 not found
```

The server does not turn these into a process crash or an opaque JSON-RPC transport error.

### Destructive operations

`restore_checkpoint` and `delete_workstation` require a boolean `confirm` argument in their input
schema. If `confirm` is `false`, the tool makes no API request and returns a message explaining that
nothing was done:

```text
Nothing was done: deleting workstation "ws_123" requires "confirm": true.
```

This guardrail forces an explicit confirmation decision into the conversation. It is not a substitute
for authorization: the API token still controls what the account can do, and MCP clients should
require user approval for destructive tools.

## Example prompts

- "List my Unkindled workstations, including deleted ones."
- "Show details and connection information for workstation `ws_123`. Do not print any private keys."
- "Start workstation `ws_123`, then show me its latest events."
- "Create a checkpoint on `ws_123` labeled `before-upgrade` with the reason `before upgrading Go`."
- "What checkpoints exist for `ws_123`?"
- "Restore checkpoint `cp_456` on `ws_123`. Ask me to confirm first, and do not set confirm to true
  until I explicitly say to proceed."
- "Delete workstation `ws_123`." The model should ask for confirmation before setting
  `confirm: true`.

## Protocol

The server implements MCP over stdio using newline-delimited JSON-RPC 2.0:

- `initialize` returns the `2024-11-05` protocol version, `serverInfo`, and
  `capabilities: {"tools": {}}`.
- `notifications/initialized` is accepted and produces no response.
- `ping` returns an empty result.
- `tools/list` returns the tool definitions.
- `tools/call` executes a tool by name with an arguments object.
- Unknown methods return JSON-RPC error `-32601`.
- Malformed JSON returns `-32700`, invalid requests return `-32600`, and invalid params return
  `-32602` where applicable.
- Tool execution and API failures return a normal JSON-RPC result with `isError: true`, not a
  transport error.
- Batch requests are not supported and return `-32600`.

Stdout contains protocol messages only. Diagnostics go to stderr.

## Security notes

- **Token handling.** The token is read from `UNKINDLED_API_TOKEN` and sent only in the
  `Authorization: Bearer` header. It is never written to stdout, tool results, error messages, or
  logs. Prefer environment injection over command-line arguments, and use a token scoped to the
  workstations this server should manage.
- **No secret-bearing tools.** There is no tool that reveals SSH private keys or provider secrets.
  `get_connection` is limited to connection information. As defense in depth, response fields with
  secret-looking names (`private_key`, `password`, `api_token`, `provider_secret`, `credentials`, and
  similar) are redacted, as are token occurrences in strings.
- **Destructive actions.** Restore and delete require `confirm: true`. The server refuses and sends
  no request when it is false. Users should still review the workstation and checkpoint IDs.
- **Transport.** The default API URL uses HTTPS. Do not point `UNKINDLED_API_URL` at an untrusted
  proxy; the bearer token is sent to that host.
- **Logging.** The server writes only to stderr for warnings and fatal startup errors. It never logs
  request headers, response bodies, or tokens.
- **Container.** The Docker image uses a distroless runtime and runs as a non-root user.
- **Local trust boundary.** This is a stdio MCP server. The MCP client and the process environment are
  trusted to hold the token. Do not expose it through a network transport without adding
  authentication and authorization.

## Development

```sh
make check     # gofmt check, go vet, go test
make build     # bin/unkindled-mcp
make test
make race
make docker
```

The test suite uses `httptest` fake APIs and covers:

- initialization, notifications, tool listing, ping, and unknown methods;
- all tool-to-API method/path mappings;
- success, pending (HTTP 202), API error, and malformed JSON-RPC handling;
- destructive `confirm` guards with assertions that the API is not called;
- missing-token configuration errors;
- token redaction in output and errors;
- private key and provider secret redaction;
- argument validation and unknown tools.

Project layout:

```text
cmd/unkindled-mcp/   entry point and stderr logging
internal/api/        Unkindled API client, error parsing, redaction
internal/config/     environment configuration
internal/mcp/        JSON-RPC transport, MCP methods, tools
```

### Manual smoke test

```sh
UNKINDLED_API_TOKEN="your-token" ./bin/unkindled-mcp
```

Paste this line into the terminal and press Enter:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"manual","version":"0"}}}
```

The response is written to stdout. Send `Ctrl-D` to exit.

## Troubleshooting

| Symptom | Likely cause |
| --- | --- |
| Tool calls return `UNKINDLED_API_TOKEN is not set` | Add the token to the MCP server's `env`/`environment` block and restart the client. |
| `HTTP 401` | The token is invalid, expired, or lacks access. |
| `HTTP 404` | The workstation, checkpoint, or endpoint does not exist for this account. |
| Timeouts | Increase `UNKINDLED_HTTP_TIMEOUT` (for example `60s`) and check network access to the API. |
| The server does not appear in the client | Use an absolute binary path, ensure the binary is executable, check the client's MCP logs, and try the manual smoke test. |
| Docker server exits immediately | Use `docker run -i`; MCP requires interactive stdin. |

To inspect startup warnings, run the binary directly from a terminal. All diagnostics appear on
stderr.

## License

MIT. See [LICENSE](LICENSE).
