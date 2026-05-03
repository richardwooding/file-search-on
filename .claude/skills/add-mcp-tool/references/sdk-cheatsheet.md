# MCP Go SDK cheat-sheet

Quick map of the types and functions in `github.com/modelcontextprotocol/go-sdk@v1.5.0/mcp` that this project uses, with one-liners on what each is for. Read the SDK's own source for full signatures — this file is a navigation aid.

## Server construction

| Symbol | What it is |
| --- | --- |
| `mcp.NewServer(impl *Implementation, opts *ServerOptions) *Server` | Builds a server. `Implementation{Name, Version}` identifies the server to clients. `opts` may be `nil`. |
| `mcp.AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])` | Registers a typed tool. The generic types come from your input/output structs; the SDK derives JSON schema from their `json`/`jsonschema` struct tags. |
| `(*Server).Run(ctx, transport) error` | Blocking; runs the server until the transport closes or `ctx` is cancelled. Used for stdio in production. |
| `(*Server).Connect(ctx, transport, opts) (*ServerSession, error)` | Returns a session without blocking. Used for in-memory transports in tests. |

## Tool types

| Symbol | What it is |
| --- | --- |
| `mcp.Tool` | Metadata: `Name`, `Description`, `InputSchema` (auto-derived by `AddTool`), `Annotations`. |
| `mcp.CallToolRequest` | Server-side request struct. Has `Session *ServerSession`. The handler receives `*CallToolRequest` plus the typed input. |
| `mcp.CallToolResult` | Optional unstructured response wrapper. Most handlers return `nil` and let the SDK build it from the typed output. |
| `mcp.ToolHandlerFor[In, Out any]` | The handler signature: `func(ctx, *CallToolRequest, In) (*CallToolResult, Out, error)`. |

## Transports

| Symbol | What it is |
| --- | --- |
| `mcp.StdioTransport` | JSON-RPC over stdin/stdout. Used by `mcpserver.Run` for the `mcp` subcommand. |
| `mcp.NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport)` | Returns two paired transports for in-process server↔client. Used by `server_test.go`. |
| `mcp.CommandTransport` | Drives a server as a subprocess. Useful for end-to-end tests that exercise the binary; this project uses in-memory transports instead. |

## Client (used in tests)

| Symbol | What it is |
| --- | --- |
| `mcp.NewClient(impl *Implementation, opts *ClientOptions) *Client` | Builds a client. |
| `(*Client).Connect(ctx, transport, opts) (*ClientSession, error)` | Returns a session. Closes when `Close()` is called. |
| `(*ClientSession).CallTool(ctx, *CallToolParams) (*CallToolResult, error)` | Invokes a tool. `CallToolParams.Arguments` is `any` — pass your typed input struct; the SDK marshals it. |
| `(*ClientSession).ListTools(ctx, *ListToolsParams) (*ListToolsResult, error)` | Lists tools the server registered. Useful for assertions in tests. |

## Decoding `StructuredContent`

Tool results carry structured output in `res.StructuredContent` as `map[string]any` (not your typed struct). The pattern from `server_test.go`:

```go
func mustDecodeStructured(t *testing.T, res *mcp.CallToolResult, into any) {
    t.Helper()
    raw, _ := json.Marshal(res.StructuredContent)
    if err := json.Unmarshal(raw, into); err != nil { t.Fatal(err) }
}
```

The double-marshal is necessary — there is no `mapstructure`-style decode helper in the SDK as of v1.5.0. If/when one ships, this file is the place to update.

## What's NOT here

- Resources (`mcp.Resource`, `mcp.AddResource`) — file-search-on doesn't expose any. If we add one, document it here.
- Prompts (`mcp.Prompt`, `mcp.AddPrompt`) — same.
- Sampling / elicitation — server-initiated client calls. Not used here.
- OAuth — the SDK ships `oauthex/` for client-side OAuth; the README marks it experimental.
- HTTP/SSE / Streamable transports — the SDK supports them, but the `mcp` subcommand is stdio-only.

## Useful imports for a new tool file

```go
import (
    "context"
    "fmt"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    "github.com/richardwooding/file-search-on/internal/celexpr"
    "github.com/richardwooding/file-search-on/internal/content"
    "github.com/richardwooding/file-search-on/internal/search"
)
```

`celexpr` for `BuildAttributes` and `Schema()`; `content` for `DefaultRegistry()`; `search` for `Walk` and `Options`. Existing tools wire to these directly — re-use rather than duplicate.
