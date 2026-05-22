package mcpserver

// CommonOutput is embedded by every MCP tool output struct so each
// response is self-describing about which server version produced
// it. Useful for agents diagnosing "is the running binary the
// version I just upgraded to?" — the MCP initialize handshake
// already carries the server version (via mcp.Implementation), but
// once the session is established per-tool responses are the
// convenient discovery surface.
//
// Populated by each handler with `out.ServerVersion = h.version`.
// `omitempty` keeps the field absent when the server was constructed
// without a stamped version (e.g. `go run` builds without ldflags).
//
// Future build / capability fields can join this struct without
// touching every tool output again.
type CommonOutput struct {
	ServerVersion string `json:"server_version,omitempty"`
}
