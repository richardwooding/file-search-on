package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/monitor"
)

// TestInstrument_RecordsToolCalls drives a search through the in-memory
// transport against a server built WithCollector, then asserts the
// collector observed the call with its result count.
func TestInstrument_RecordsToolCalls(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# hello\n\nbody body body\n")
	mustWrite(t, filepath.Join(dir, "b.md"), "# world\n\nmore body\n")

	ctx := t.Context()
	coll := monitor.NewCollector()
	server := New("test", index.NewMemory(), 0, EmbedDefaults{}, WithCollector(coll))
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

	if _, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir},
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	snap := coll.Snapshot()
	if snap.TotalCalls != 1 {
		t.Fatalf("TotalCalls = %d, want 1", snap.TotalCalls)
	}
	if len(snap.Tools) != 1 || snap.Tools[0].Tool != "search" {
		t.Fatalf("Tools = %+v, want one 'search'", snap.Tools)
	}
	if snap.Tools[0].Count != 1 {
		t.Errorf("search count = %d, want 1", snap.Tools[0].Count)
	}
	// The callReporter path should surface the 2 markdown matches.
	if len(snap.Recent) != 1 || snap.Recent[0].Count != 2 {
		t.Errorf("recent[0] = %+v, want count 2 via callReport", snap.Recent)
	}
	if snap.Recent[0].Outcome != monitor.OutcomeOK {
		t.Errorf("outcome = %q, want ok", snap.Recent[0].Outcome)
	}
}
