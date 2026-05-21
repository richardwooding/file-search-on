package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSearchToolRank verifies that the `rank` MCP input drives the
// sort order — files with higher rank expression values come first.
// Uses a `size`-based rank expression so the assertions don't depend
// on Ollama / semantic search.
func TestSearchToolRank(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "small.txt"), "x")
	mustWrite(t, filepath.Join(dir, "medium.txt"), string(make([]byte, 500)))
	mustWrite(t, filepath.Join(dir, "large.txt"), string(make([]byte, 50000)))

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr: "is_text",
			Dir:  dir,
			Rank: "size",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)

	if out.Count != 3 {
		t.Fatalf("expected 3 matches, got %d (%+v)", out.Count, out.Matches)
	}

	// Default order desc — largest first.
	wantOrder := []string{"large.txt", "medium.txt", "small.txt"}
	for i, m := range out.Matches {
		got := filepath.Base(m.Path)
		if got != wantOrder[i] {
			t.Errorf("position %d: got %s, want %s", i, got, wantOrder[i])
		}
	}

	// Rank field should surface in the wire shape — agents can use
	// it to inspect the computed score.
	if out.Matches[0].Rank == 0 {
		t.Errorf("match[0].rank should be populated, got 0")
	}
	if out.Matches[0].Rank != 50000 {
		t.Errorf("match[0].rank = %v, want 50000 (size of large.txt)", out.Matches[0].Rank)
	}
}

// TestSearchToolRankCompileError verifies that a malformed rank
// expression surfaces a clear error rather than silently producing
// empty / mis-sorted results.
func TestSearchToolRankCompileError(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "x.txt"), "hi")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr: "is_text",
			Dir:  dir,
			Rank: "size +", // syntax error
		},
	})
	// CallTool returns the tool's error inside the result, not as
	// the transport error.
	if err != nil {
		t.Fatalf("transport CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected res.IsError=true for malformed rank, got false")
	}
}
