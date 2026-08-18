# mcp-bridge

A lightweight, high-performance, zero-dependency Go bridge that connects `llama.cpp` and other `stdio`-only MCP clients to remote **SSE** and **Streamable HTTP** endpoints.

---

## Features

- **Protocol Agnostic:** Supports both legacy MCP SSE (streaming `GET` + dynamic `POST`) and modern Streamable HTTP (`POST`-only with event-stream responses).
- **Automatic Namespacing:** Prefixes tool names (e.g., `ha_turn_on`, `wiki_search`) to prevent tool collisions across multiple servers.
- **Secure Authentication:** Supports Bearer token authorization injected securely via environment variables.
- **Zero Runtime Dependencies:** Compiles into a single static binary—no Node.js, Python, or external runtime required inside your container.
- **Production Hardened:** Thread-safe atomic URL swapping, safe buffer cloning, Keep-Alive connection reuse, and resilient auto-reconnect loops.

---

## 🛠️ Building the Binary

You do not need Go installed locally. You can build a static Linux binary using a one-line Docker command:

```bash
docker run --rm -v "${PWD}:/app" -w /app golang:1.22-alpine env GOOS=linux go build -ldflags="-w -s" -o mcp-bridge main.go

```

*(If using Windows Command Prompt instead of PowerShell/Linux, replace `${PWD}` with `%cd%`)*

---

## 🚀 Usage with `llama.cpp` Docker

Because `llama.cpp` runs without Node runtimes, the cleanest approach is to bind-mount the compiled `mcp-bridge` binary and your `mcp.json` config directly into the official `llama.cpp` container.

### 1. Configure `mcp.json`

Define your remote MCP servers in standard Cursor/Claude-compatible format:

```json
{
  "mcpServers": {
    "wikipedia": {
      "command": "/app/mcp-bridge",
      "args": [
        "wikipedia",
        "[https://wiki.example.com/sse](https://wiki.example.com/sse)"
      ],
      "env": {
        "MCP_TRANSPORT": "sse"
      }
    },
    "homeassistant": {
      "command": "/app/mcp-bridge",
      "args": [
        "homeassistant",
        "[https://homeassistant.example.com:8123/api/mcp](https://homeassistant.example.com:8123/api/mcp)"
      ],
      "env": {
        "MCP_TRANSPORT": "streamable",
        "MCP_BEARER_TOKEN": "your-long-lived-access-token"
      }
    }
  }
}

```

### 2. Configure `docker-compose.yml`

Mount the binary into `/usr/local/bin/` as read-only (`:ro`) and pass the `--mcp-servers-config` flag:

```yaml
services:
  llamacpp:
    image: ghcr.io/ggml-org/llama.cpp:server
    container_name: llamacpp
    restart: unless-stopped
    volumes:
      - ./mcp-bridge:/app/mcp-bridge:ro
      - ./mcp.json:/models/mcp.json:ro
      - ./models:/models:ro
    command:
      - "-m"
      - "/models/your-model.gguf"
      - "--mcp-servers-config"
      - "/models/mcp.json"
      - "--port"
      - "8080"
    ports:
      - "8080:8080"

```

---

## ⚙️ Configuration Reference

### Command-Line Arguments

```bash
mcp-bridge <prefix> <url>

```

* `<prefix>`: String prepended to tool names (e.g., passing `wiki` renames `search` to `wiki_search`).
* `<url>`: Remote MCP endpoint URL.

### Environment Variables

| Variable | Values | Description |
| --- | --- | --- |
| `MCP_TRANSPORT` | `sse` *(default)*, `streamable` / `http` | Defines the transport protocol to use for the upstream server. |
| `MCP_BEARER_TOKEN` | *string (optional)* | Bearer token sent via the `Authorization: Bearer <token>` header. |

### Protocol Version Negotiation

The bridge automatically handles protocol version for both the 2025-11-25 and 2026-07-28 MCP specs:

- **2025-11-25 style**: Captures the `protocolVersion` from the server's `initialize` response and sets the `MCP-Protocol-Version` header on all subsequent requests.
- **2026-07-28 style**: Extracts `io.modelcontextprotocol/protocolVersion` from each request's `params._meta` and sets the matching `MCP-Protocol-Version` header. This is the per-request version negotiation approach introduced in the 2026-07-28 spec.
- **Fallback**: If neither is available (e.g., before `initialize` response arrives), the header is omitted — servers default to their supported version.

The bridge also tracks `server/discover` requests (the 2026-07-28 replacement for `initialize`).

### Session Management

The bridge captures the `Mcp-Session-Id` header from server responses (including `initialize`/`server/discover` responses) and includes it in all subsequent HTTP requests. This ensures compatibility with stateful servers that require session IDs.

---

## 🔍 How It Works

```
┌─────────────┐   stdio   ┌──────────────┐   HTTP / SSE   ┌────────────────────┐
│  llama.cpp  │ ◄───────► │  mcp-bridge  │ ◄────────────► │ Remote MCP Server  │
│ (MCP Client)│ (JSON-RPC)│  (Go Proxy)  │  (Auth/Stream) │(HA, Wikipedia, etc)│
└─────────────┘           └──────────────┘                └────────────────────┘

```

1. `llama.cpp` starts `mcp-bridge` as a local subprocess via standard input/output (`stdio`).
2. `mcp-bridge` translates incoming JSON-RPC requests to the remote HTTP/SSE endpoint.
3. During tool discovery (`tools/list`), the bridge intercepts the response and prefixes tool names with `<prefix>_`.
4. During tool execution (`tools/call`), the bridge strips the prefix and routes the call to the target server.
5. All diagnostics, connection errors, and HTTP status codes are written to `stderr` to prevent corrupting the `stdio` JSON-RPC stream.

---
```

```
