# Refactor plan – llama-mcp-bridge → official go-sdk

Goal: keep external CLI / env contract identical, produce a single static binary that can be launched as a subprocess by llama.cpp / localai. Replace hand-rolled transport code with `github.com/modelcontextprotocol/go-sdk/mcp` v1.8.0.

## Current interface to preserve
```
mcp-bridge <prefix> <url>
MCP_TRANSPORT=sse|streamable|http   # default sse
MCP_BEARER_TOKEN=...
```
Binary reads JSON-RPC from stdin, writes to stdout, logs to stderr.

## Architecture
* Local side: SDK `mcp.Server` on `mcp.StdioTransport`. This is the stdio interface llama.cpp sees.
* Remote side: SDK `mcp.Client` connected to remote server via transport chosen by env.
* Proxy layer: local server does not implement real tools; it forwards:
  - `tools/list` → remote `session.ListTools`, prefix names `prefix_<name>`
  - `tools/call` → strip prefix, forward `session.CallTool`
  - `initialize` / `server/discover` / notifications passed through
  SDK handles session ID, MCP-Protocol-Version, Last-Event-ID, modern per-request headers.

## Steps

### 1. Bootstrap & args parsing
* Parse `os.Args[1]` prefix, `os.Args[2]` url.
* Read `MCP_TRANSPORT`, default `sse`. Read `MCP_BEARER_TOKEN`.
* Build remote transport:
  - sse → `mcp.SSETransport{URL, Headers}`
  - streamable/http → `mcp.StreamableHTTPTransport{URL, Headers}`
  Inject Authorization header if token present.

### 2. Remote client connection
* `client := mcp.NewClient(&mcp.Implementation{Name:"mcp-bridge",Version:"1.0"}, nil)`
* `session, err := client.Connect(ctx, transport, nil)`
* Keep session for lifetime; SDK manages Mcp-Session-Id, protocol version negotiation, resumability.

### 3. Local server & proxy
* `localServer := mcp.NewServer(&mcp.Implementation{Name:"mcp-bridge-proxy",Version:"1.0"}, nil)`
* Implement dynamic tool proxy:
  - On `ListTools` from local client, call `session.ListTools`, rename tools with prefix, cache mapping.
  - On `CallTool`, strip prefix and call `session.CallTool`.
* Run `localServer.Run(ctx, &mcp.StdioTransport{})`

### 4. Name-spacing
Preserve existing prefix behavior. Prefix applied on list response, stripped on call.

### 5. Session teardown
Close `session.Close()` on shutdown; SDK performs DELETE for session-era servers. No manual DELETE needed.

## SDK advantages vs current code
* Correct modern header sentinel `=?base64?{b64}?=`
* Proper era detection, per-request `_meta` propagation
* Last-Event-ID resumability for Streamable HTTP
* Mandatory MCP-Protocol-Version on first request
* Server/discover handling, Mcp-Method/Name/Param-* mirroring
* Session creation/teardown handled by SDK

## Deliverables
* Rewrite `main.go` using go-sdk imports
* Keep binary name `mcp-bridge`, static build with `-ldflags="-w -s"`
* Update README to note SDK v1.8.0, transport support
* Validation: unit test with local server, integration with real SSE/Streamable endpoints, ensure stdout clean, stderr logs only

## Validation checklist
- [ ] Binary accepts same args/env
- [ ] tools/list returns prefix_<name>
- [ ] tools/call forwards correctly
- [ ] Works with SSE and Streamable HTTP
- [ ] Bearer token passed through
- [ ] Stderr only, stdout is pure JSON-RPC
