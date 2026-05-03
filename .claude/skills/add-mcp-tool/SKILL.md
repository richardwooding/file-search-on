---
name: add-mcp-tool
description: Adds a new tool to file-search-on's MCP server in `internal/mcpserver/server.go` by defining JSON-tagged input/output structs (with `jsonschema` field tags), writing a handler that matches the SDK's `ToolHandlerFor[In, Out]` generic signature, registering it with `mcp.AddTool` inside `New()`, and adding an in-memory transport test in `server_test.go`. Use when extending the MCP server with a new tool — for example, a `read_attributes` tool that returns attributes for a single file without running a CEL filter, or any other agent-callable capability beyond the existing `search` and `list_attributes` tools.
---

# Add MCP Tool

The MCP server in `internal/mcpserver/server.go` exposes tools to MCP clients (Claude Desktop, IDE plugins) via the official Go SDK at `github.com/modelcontextprotocol/go-sdk@v1.5.0`. Adding a tool is four small things in one file plus a test in another. The schema is generated from struct tags — you don't write JSON schema by hand.

Prefer **extending the existing `search` tool's input** over forking a new tool when the new capability is "search but with one more filter / option". MCP clients see fewer entry points and the model picks the right one more reliably.

## The four parts of a tool

1. **Input/output structs** with `json` and `jsonschema` tags. The SDK derives the JSON schema from these.
2. **Handler function** matching `ToolHandlerFor[In, Out]`:
   ```go
   func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error)
   ```
3. **Registration** via `mcp.AddTool(s, &mcp.Tool{...}, handler)` inside `New(version)`.
4. **Test** in `server_test.go` driving the new tool through `mcp.NewInMemoryTransports()`.

## Quick start

Adding a hypothetical `read_attributes` tool that returns the full attribute set for a single file:

1. **Add the structs** in `internal/mcpserver/server.go`, near the existing `SearchInput` / `SearchOutput`:
   ```go
   type ReadAttributesInput struct {
       Path string `json:"path" jsonschema:"Path to a single file. Required."`
   }

   type ReadAttributesOutput struct {
       Path        string         `json:"path"`
       ContentType string         `json:"content_type"`
       Size        int64          `json:"size"`
       Attributes  map[string]any `json:"attributes"`
   }
   ```

2. **Write the handler**, near the existing `searchHandler`:
   ```go
   func readAttributesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReadAttributesInput) (*mcp.CallToolResult, ReadAttributesOutput, error) {
       attrs, err := celexpr.BuildAttributes(in.Path, content.DefaultRegistry())
       if err != nil {
           return nil, ReadAttributesOutput{}, fmt.Errorf("build attributes: %w", err)
       }
       return nil, ReadAttributesOutput{
           Path:        attrs.Path,
           ContentType: attrs.ContentType,
           Size:        attrs.Size,
           Attributes:  attrs.Extra,
       }, nil
   }
   ```

3. **Register** inside `New(version)`, alongside the existing `mcp.AddTool` calls:
   ```go
   mcp.AddTool(s, &mcp.Tool{
       Name:        "read_attributes",
       Description: "Return the full attribute set for a single file by path, without running a CEL filter.",
   }, readAttributesHandler)
   ```

4. **Test** in `internal/mcpserver/server_test.go` — copy `TestSearchTool` as a template; it already wires up the in-memory transport via `newSession(t)`. Decode `StructuredContent` with the existing `mustDecodeStructured(t, res, &out)` helper.

5. **Run** `go test -race ./internal/mcpserver/...` — the test file is the verification. There's no separate audit script for this skill.

## Handler signature, exactly

```go
func handlerName(
    ctx context.Context,
    req *mcp.CallToolRequest,
    in InputType,
) (*mcp.CallToolResult, OutputType, error)
```

Notes:

- The first return value (`*mcp.CallToolResult`) is usually `nil`. The SDK builds one automatically from the structured `Output` value, populating `Content` with a JSON text representation. Return a non-nil `*mcp.CallToolResult` only when you need to attach unstructured `Content` (e.g. plain text or images alongside structured output).
- Errors returned from the handler become tool errors visible to the client (`res.GetError() != nil`). They are not transport errors and don't tear down the session.
- `req.Session` gives access to the server session if you need it (for progress notifications, sampling, etc.). Most tools don't.

## Schema tags

```go
type ToolInput struct {
    Path    string `json:"path"    jsonschema:"Path to a file. Required."`
    Verbose bool   `json:"verbose,omitempty" jsonschema:"Include verbose output. Defaults to false."`
}
```

- The `json` tag controls the field name on the wire. Use snake_case to match the rest of the file-search-on attribute namespace.
- The `jsonschema` tag is the human-readable description that surfaces to MCP clients (and to the LLM driving the tool call).
- For optional fields, add `,omitempty` to the `json` tag and document the default in the `jsonschema` description.

The SDK uses `github.com/google/jsonschema-go` to derive the full JSON schema from the struct, supporting the 2020-12 draft only — don't reach for unusual schema features like `oneOf`/`anyOf`.

## Testing pattern

The existing `TestSearchTool` in `server_test.go` is the canonical pattern. Key points:

- `newSession(t)` builds the server with `mcpserver.New("test")`, wires it to a client through `mcp.NewInMemoryTransports()`, and returns `(ctx, *mcp.ClientSession)`. Reuse this helper for new tool tests.
- Call `cs.CallTool(ctx, &mcp.CallToolParams{Name: "your_tool", Arguments: YourInputStruct{...}})`.
- Decode the structured output with `mustDecodeStructured(t, res, &out)` — the helper handles the JSON re-marshal needed because `res.StructuredContent` arrives as `map[string]any`, not your typed struct.
- Don't try to assert on `res.Content` — the SDK auto-populates it from `StructuredContent` and the format isn't load-bearing for clients that read structured output.

## References

- [references/sdk-cheatsheet.md](references/sdk-cheatsheet.md) — quick map of the SDK's relevant types (`Tool`, `CallToolRequest`, `CallToolResult`, `ToolHandlerFor`, `StdioTransport`, `InMemoryTransport`) with one-liners on what each is for.

## Conventions

- **Tool name** is `snake_case` and a verb or short noun: `search`, `list_attributes`, `read_attributes`. Avoid prefixes like `file_` or `fs_` — clients see the tool name in the context of the server's name (`file-search-on`).
- **Description** is one sentence, third person, names the inputs and outputs concretely. Bad: "Reads attributes." Good: "Returns the full attribute set for a single file by path, without running a CEL filter."
- **Reuse existing code.** `search.Walk`, `celexpr.BuildAttributes`, `celexpr.Schema()`, `content.DefaultRegistry()` are the shared building blocks. Don't duplicate them — wire your handler to them and let the existing logic do the work.
- **Update CLAUDE.md and the README.** Add the new tool to the "MCP server" tables in both. The README's section is short on purpose; one row per tool.
- **Don't add a CLI flag for the new tool.** The `mcp` subcommand is intentionally flagless — all configuration is per-tool-call from the MCP client.
