# mcp-bridge

A lightweight Go bridge that connects `llama.cpp` and other stdio-only MCP clients to remote MCP servers via SSE or Streamable HTTP. Rewritten to use the official [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0 for full spec compliance.

> **Stdio interface**: The bridge presents a JSON-RPC stdio interface to the calling client. Compile with `-tags nostderr` to disable stderr logging if the client treats stderr as an error channel.

## Features

- **Protocol Agnostic:** Supports legacy MCP SSE and modern Streamable HTTP. Transport is selected via `MCP_TRANSPORT`.
- **Automatic Namespacing:** Prefixes tool names `prefix_<name>` to prevent collisions.
- **Secure Authentication:** Bearer token via `MCP_BEARER_TOKEN` injected via HTTP client transport.
- **Zero Runtime Dependencies:** Single static binary, no external runtime required.
- **Spec Compliant:** Uses official go-sdk for transport, session management, protocol version negotiation, and per-request headers.

## Building

```bash
docker run --rm -v "${PWD}:/app" -w /app golang:1.22-alpine \
  sh -c 'go mod download && go build -ldflags="-w -s" -o mcp-bridge .'
```

To disable stderr logging:
```bash
go build -ldflags="-w -s" -tags nostderr -o mcp-bridge .
```

## Usage

```bash
mcp-bridge <prefix> <url>
```

Environment variables:
- `MCP_TRANSPORT` = `sse` | `streamable` | `http`  (default `sse`)
- `MCP_BEARER_TOKEN` = Bearer token for auth

### llama.cpp example

`mcp.json`:
```json
{
  "mcpServers": {
    "wikipedia": {
      "command": "/app/mcp-bridge",
      "args": ["wikipedia", "https://example.com/sse"],
      "env": { "MCP_TRANSPORT": "sse" }
    }
  }
}
```

Mount the binary into the llama.cpp container and pass `--mcp-servers-config`.

## Architecture

- Local side: reads JSON-RPC from stdin, writes to stdout
- Remote side: `mcp.Client` with `SSEClientTransport` or `StreamableClientTransport`
- Tools are listed via SDK, names are prefixed, calls are forwarded with prefix stripped

## Notes

- Built with `github.com/modelcontextprotocol/go-sdk v1.8.0`
- Bearer token is injected via custom HTTP roundtripper
- `-tags nostderr` disables stderr logging for clients that treat stderr as error

## Limitations

- Only `tools/list` and `tools/call` are proxied. Other MCP methods are passed through only if the client uses the SDK server path; the manual stdio parser is intentionally minimal for the llama.cpp use-case.
- Modern 2026-07-28 per-request `Mcp-Param-*` mirroring from `x-mcp-header` annotations is handled by the SDK defaults; custom annotation extraction is not implemented.
- Standalone SSE stream is disabled via `DisableStandaloneSSE:true` for compatibility with test servers that reject GET without session.
