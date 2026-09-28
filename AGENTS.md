# Agent Notes

## Build
- Use `go build -ldflags="-w -s" -buildvcs=false -o mcp-bridge`
- Disable stderr logging: `go build -tags nostderr ...`

## Interface
- Args: `<prefix> <url>`
- Env: `MCP_TRANSPORT` (sse|streamable|http), `MCP_BEARER_TOKEN`
- Stdio JSON-RPC in/out, logs to stderr unless built with nostderr tag

## SDK
- Depends on `github.com/modelcontextprotocol/go-sdk v1.8.0`
- Uses `SSEClientTransport` and `StreamableClientTransport`
- Bearer token injected via custom `authRoundTripper`

## Testing
- Local test servers: `test_server` for Streamable, `test_sse_server` for SSE
- Live validation against servers in `mcp.json`

Do not modify CLI contract. Keep prefix naming behavior.
