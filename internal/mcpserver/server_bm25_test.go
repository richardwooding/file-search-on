package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestSearchTool_KeywordQueryBM25 confirms the search tool's keyword_query
// populates bm25, ranks the term-dense file first, and that bm25 survives
// a fields projection (issue #335).
func TestSearchTool_KeywordQueryBM25(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "dense.md"), "# kubernetes\n\n"+strings.Repeat("kubernetes scheduler kubernetes ", 20))
	mustWrite(t, filepath.Join(dir, "mention.md"), "# notes\n\na single kubernetes reference buried in unrelated prose here\n")
	mustWrite(t, filepath.Join(dir, "unrelated.md"), "# baking\n\n"+strings.Repeat("flour sugar butter oven ", 20))

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir, KeywordQuery: "kubernetes scheduler"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if len(out.Matches) != 3 {
		t.Fatalf("want 3 matches, got %d", len(out.Matches))
	}
	if filepath.Base(out.Matches[0].Path) != "dense.md" {
		t.Errorf("want dense.md ranked first, got %s", filepath.Base(out.Matches[0].Path))
	}
	if out.Matches[0].BM25 <= 0 {
		t.Errorf("top match should carry bm25 > 0, got %f", out.Matches[0].BM25)
	}
	if got := filepath.Base(out.Matches[len(out.Matches)-1].Path); got != "unrelated.md" {
		t.Errorf("want unrelated.md last, got %s", got)
	}

	// bm25 must be a valid projection field.
	res2, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir, KeywordQuery: "kubernetes", Fields: []string{"bm25"}, Limit: 1},
	})
	if err != nil {
		t.Fatalf("CallTool (fields): %v", err)
	}
	if res2.IsError {
		t.Fatalf("fields:[bm25] rejected: %v", res2.GetError())
	}
	var out2 SearchOutput
	mustDecodeStructured(t, res2, &out2)
	if len(out2.Matches) != 1 || out2.Matches[0].BM25 <= 0 {
		t.Errorf("projected response should include bm25 > 0; got %+v", out2.Matches)
	}
}

// TestSearchSemanticTool_Hybrid exercises the hybrid RRF path end-to-end
// with a mock Ollama that returns a query-aligned vector only for the
// file whose body contains the marker, so semantic and keyword signals
// point at the same file and it must rank first.
func TestSearchSemanticTool_Hybrid(t *testing.T) {
	dir := t.TempDir()
	const marker = "transformer"
	mustWrite(t, filepath.Join(dir, "match.md"), "# paper\n\n"+strings.Repeat(marker+" attention ", 30))
	mustWrite(t, filepath.Join(dir, "other.md"), "# misc\n\n"+strings.Repeat("gardening compost ", 30))

	// Mock Ollama: align the embedding with the query ([1,0,0]) when the
	// input mentions the marker, else orthogonal ([0,1,0]).
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		vec := []float32{0, 1, 0}
		if strings.Contains(req.Input, marker) {
			vec = []float32{1, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{vec}})
	}))
	t.Cleanup(ollama.Close)

	ctx := t.Context()
	server := New("test", index.NewMemory(), 0, EmbedDefaults{Server: ollama.URL, Model: "mock"})
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

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search_semantic",
		Arguments: SearchSemanticInput{
			Query:     marker + " attention mechanism",
			Dir:       dir,
			Hybrid:    true,
			Threshold: 0.0, // keep both files so RRF has two to fuse
			Limit:     10,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("hybrid search_semantic errored: %v", res.GetError())
	}
	var out SearchSemanticOutput
	mustDecodeStructured(t, res, &out)
	if len(out.Matches) == 0 {
		t.Fatal("hybrid returned no matches")
	}
	if filepath.Base(out.Matches[0].Path) != "match.md" {
		t.Errorf("hybrid should rank match.md first (strong in both signals), got %s", filepath.Base(out.Matches[0].Path))
	}
	if out.Matches[0].BM25 <= 0 {
		t.Errorf("hybrid top match should carry bm25 > 0, got %f", out.Matches[0].BM25)
	}
}
