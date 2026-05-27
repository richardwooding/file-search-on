package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/monitor"
)

// sessionWithMonitor builds an in-memory MCP session whose server has a
// monitor controller attached (dynamic port).
func sessionWithMonitor(t *testing.T, ctl *monitor.Controller) (context.Context, *mcp.ClientSession) {
	t.Helper()
	ctx := t.Context()
	server := New("test", index.NewMemory(), 0, EmbedDefaults{}, WithMonitor(ctl))
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return ctx, cs
}

func TestMonitorInfo_LazyEnableAndPeers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("LocalAppData", dir)

	serveCtx := t.Context()
	ctl := monitor.NewController(serveCtx, monitor.Config{Version: "test", Mode: "mcp-stdio", Index: index.NewMemory()}, ":0")
	ctx, cs := sessionWithMonitor(t, ctl)

	// Before enable: reports disabled, no URL, but valid response.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "monitor_info", Arguments: MonitorInfoInput{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var before MonitorInfoOutput
	mustDecodeStructured(t, res, &before)
	if before.Enabled {
		t.Errorf("expected disabled before enable, got enabled=%v url=%q", before.Enabled, before.URL)
	}

	// enable=true starts the dashboard and returns its URL.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "monitor_info", Arguments: MonitorInfoInput{Enable: true}})
	if err != nil {
		t.Fatalf("CallTool enable: %v", err)
	}
	var after MonitorInfoOutput
	mustDecodeStructured(t, res, &after)
	if !after.Enabled || after.URL == "" {
		t.Fatalf("after enable: enabled=%v url=%q, want running with a URL", after.Enabled, after.URL)
	}
	// This instance should appear in its own peer list, flagged is_self.
	var selfSeen bool
	for _, p := range after.Peers {
		if p.URL == after.URL && p.IsSelf {
			selfSeen = true
		}
	}
	if !selfSeen {
		t.Errorf("self not found (flagged is_self) in peers: %+v", after.Peers)
	}
}

func TestMonitorInfo_NoControllerReportsUnavailable(t *testing.T) {
	ctx, cs := newSession(t) // built without WithMonitor
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "monitor_info", Arguments: MonitorInfoInput{}})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out MonitorInfoOutput
	mustDecodeStructured(t, res, &out)
	if out.Enabled {
		t.Errorf("expected disabled with no controller")
	}
	if out.Note == "" {
		t.Errorf("expected a note explaining monitoring is unavailable")
	}
}
