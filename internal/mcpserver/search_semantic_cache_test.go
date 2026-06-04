package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestSearchSemanticReusesCachedEmbeddings is the MCP-level regression
// for issue #306 (the server re-embedded the whole tree on every
// search_semantic call) and Finding #5 (semantic matches dropped
// title/author). A mock Ollama answers /api/embed with a fixed vector;
// the same query is run twice against the same shared in-memory index.
//
// The second call must reuse the cached per-file embedding
// (index_stats.embed_hits > 0, and the file is embedded exactly once)
// and both calls must surface the file's title.
func TestSearchSemanticReusesCachedEmbeddings(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "# Neuromancer\n\ncyberspace console cowboy ICE\n")

	// Mock Ollama: every embed (query + each file) returns [1,0,0], so
	// cosine similarity is 1.0 and the file clears the threshold.
	fileEmbeds := 0
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			fileEmbeds++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0, 0}}})
	}))
	t.Cleanup(ollama.Close)

	ctx := t.Context()
	idx := index.NewMemory()
	server := New("test", idx, 0, EmbedDefaults{Server: ollama.URL, Model: "mock"})

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

	semantic := func(label string) SearchSemanticOutput {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_semantic",
			Arguments: SearchSemanticInput{Query: "cyberpunk hacker", Dir: dir, Threshold: 0.4, Limit: 10},
		})
		if err != nil {
			t.Fatalf("%s: CallTool: %v", label, err)
		}
		if res.GetError() != nil {
			t.Fatalf("%s: tool error: %v", label, res.GetError())
		}
		var out SearchSemanticOutput
		mustDecodeStructured(t, res, &out)
		if out.Count != 1 || len(out.Matches) != 1 {
			t.Fatalf("%s: Count=%d len(Matches)=%d want 1/1; out=%+v", label, out.Count, len(out.Matches), out)
		}
		return out
	}

	out1 := semantic("call 1")
	if out1.Matches[0].Title != "Neuromancer" {
		t.Errorf("call 1: Title=%q want %q", out1.Matches[0].Title, "Neuromancer")
	}

	out2 := semantic("call 2")
	if out2.Matches[0].Title != "Neuromancer" {
		t.Errorf("call 2 (cached): Title=%q want %q (Finding #5: cached semantic match dropped title)", out2.Matches[0].Title, "Neuromancer")
	}

	// The single file must have been embedded exactly once across both
	// calls (the query is embedded each call, hence > 1 total /api/embed
	// hits, but the file's vector must come from cache on call 2).
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "index_stats", Arguments: struct{}{}})
	if err != nil {
		t.Fatalf("index_stats: %v", err)
	}
	var stats IndexStatsOutput
	mustDecodeStructured(t, res, &stats)
	if stats.EmbedHits < 1 {
		t.Errorf("EmbedHits=%d want >= 1 — the repeat semantic query re-embedded the tree (#306); stats=%+v", stats.EmbedHits, stats)
	}
	if stats.EmbedPuts != 1 {
		t.Errorf("EmbedPuts=%d want 1 — the file should be embedded exactly once; stats=%+v", stats.EmbedPuts, stats)
	}
}
