package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/monitor"
)

// MonitorInfoInput is the input for the `monitor_info` tool.
type MonitorInfoInput struct {
	// Enable, when true, starts the monitoring dashboard on a dynamic
	// localhost port if it isn't already running, then returns its URL.
	// Idempotent — a second call returns the same URL.
	Enable bool `json:"enable,omitempty" jsonschema:"When true, start the monitoring dashboard on an OS-assigned localhost port if it isn't already running, and return its URL. Idempotent."`
}

// MonitorInfoOutput reports this server's monitoring dashboard state plus
// the registry of sibling file-search-on instances that also have a
// dashboard running, so an agent / operator can open or switch between
// them.
type MonitorInfoOutput struct {
	CommonOutput
	// Enabled is true when this server's dashboard is currently serving.
	Enabled bool `json:"enabled"`
	// URL is this server's dashboard URL (empty when not enabled).
	URL string `json:"url,omitempty"`
	// IndexPath is the absolute path of this server's persistent index
	// file when IndexBackend == "persistent", empty otherwise.
	IndexPath string `json:"index_path,omitempty"`
	// IndexBackend is "persistent" when this server is backed by a
	// bbolt file, "in-memory" when it's running with process-lifetime
	// cache only (either by --no-index opt-out or by graceful fallback
	// because another instance held the writer lock).
	IndexBackend string `json:"index_backend,omitempty"`
	// IndexFallbackReason explains why IndexBackend is "in-memory" when
	// the user did not explicitly request that. Values: "" (happy path
	// — IndexBackend is "persistent"), "no_index_flag" (user passed
	// --no-index), "lock_contention" (another file-search-on instance
	// holds the writer lock on the default index file).
	IndexFallbackReason string `json:"index_fallback_reason,omitempty"`
	// Peers is every live dashboard instance discovered via the shared
	// registry, including this one (flagged is_self). Newest-startup
	// last. Empty when peer discovery is unavailable. Each peer entry
	// carries the same index_path / index_backend / index_fallback_reason
	// fields, so an agent can identify which sibling PID holds the
	// writer lock when this instance is in fallback mode.
	Peers []monitor.Entry `json:"peers"`
	// Note carries a human-readable hint when monitoring isn't wired at
	// all (e.g. the server was built without a controller).
	Note string `json:"note,omitempty"`
}

func (h *handlers) monitorInfoHandler(_ context.Context, _ *mcp.CallToolRequest, in MonitorInfoInput) (*mcp.CallToolResult, MonitorInfoOutput, error) {
	out := MonitorInfoOutput{CommonOutput: CommonOutput{ServerVersion: h.version}}

	if h.monitorCtl == nil {
		out.Note = "monitoring is not available for this server; relaunch with --monitor (dynamic port) or --monitor-addr :PORT"
		out.Peers = []monitor.Entry{}
		return nil, out, nil
	}

	if in.Enable {
		if _, err := h.monitorCtl.EnsureStarted(); err != nil {
			return nil, MonitorInfoOutput{}, fmt.Errorf("start monitor dashboard: %w", err)
		}
	}

	url, running := h.monitorCtl.Info()
	out.Enabled = running
	out.URL = url
	out.IndexPath, out.IndexBackend, out.IndexFallbackReason = h.monitorCtl.IndexInfo()
	out.Peers = monitor.Peers()
	if out.Peers == nil {
		out.Peers = []monitor.Entry{}
	}
	for i := range out.Peers {
		if out.Peers[i].URL == url && url != "" {
			out.Peers[i].IsSelf = true
		}
	}
	if !running {
		out.Note = "dashboard not started; call monitor_info with enable=true to start it on a dynamic localhost port"
	}
	return nil, out, nil
}
