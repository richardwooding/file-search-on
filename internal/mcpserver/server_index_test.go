package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestIndexStatsTool verifies that the index_stats tool surfaces
// hit/miss counters and that read_attributes feeds those counters.
// This is the cross-call cache-reuse test for MCP mode.
func TestIndexStatsTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	mustWrite(t, path, "# title\nbody body body\n")

	ctx := t.Context()
	idx := index.NewMemory()
	server := New("test", idx, 0, EmbedDefaults{})
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

	// First read_attributes: miss + store.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_attributes",
		Arguments: ReadAttributesInput{Path: path},
	})
	if err != nil {
		t.Fatalf("CallTool #1: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error #1: %v", res.GetError())
	}

	// Second read_attributes: hit.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_attributes",
		Arguments: ReadAttributesInput{Path: path},
	})
	if err != nil {
		t.Fatalf("CallTool #2: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error #2: %v", res.GetError())
	}

	// index_stats: confirm the counters.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "index_stats",
		Arguments: struct{}{},
	})
	if err != nil {
		t.Fatalf("CallTool index_stats: %v", err)
	}
	var stats IndexStatsOutput
	mustDecodeStructured(t, res, &stats)

	if stats.Hits != 1 {
		t.Errorf("Hits=%d want 1 (full stats: %+v)", stats.Hits, stats)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses=%d want 1 (full stats: %+v)", stats.Misses, stats)
	}
	if stats.Puts != 1 {
		t.Errorf("Puts=%d want 1 (full stats: %+v)", stats.Puts, stats)
	}
	// No --watch-index watcher wired on this server, so the watch
	// counters must report zero (nil-safe Snapshot), not be absent.
	if stats.WatchRefreshed != 0 || stats.WatchEvicted != 0 || stats.WatchErrors != 0 {
		t.Errorf("watch counters should be zero without a watcher: refreshed=%d evicted=%d errors=%d",
			stats.WatchRefreshed, stats.WatchEvicted, stats.WatchErrors)
	}
}
