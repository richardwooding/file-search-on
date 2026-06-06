package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
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
			Arguments: SearchSemanticInput{Query: "cyberpunk hacker", Dir: dir, Threshold: fptr(0.4), Limit: 10},
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

	if out1.AnnUsed {
		t.Errorf("call 1 should be the cold full walk (ann_used=false), got ann_used=true")
	}

	out2 := semantic("call 2")
	if out2.Matches[0].Title != "Neuromancer" {
		t.Errorf("call 2 (cached): Title=%q want %q (Finding #5: cached semantic match dropped title)", out2.Matches[0].Title, "Neuromancer")
	}
	// Call 2 must take the warm ANN fast path (issue #335 part 2): the
	// first walk warmed the in-memory vector index, so the repeat query
	// is answered without re-walking OR re-embedding the file — an even
	// stronger guarantee than the #306 cached-vector reuse it replaces.
	if !out2.AnnUsed {
		t.Errorf("call 2 should use the warm ANN index (ann_used=true), got false")
	}

	// The single file must have been embedded exactly once across both
	// calls — neither the warm ANN path nor the #306 cache reuse re-embeds.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "index_stats", Arguments: struct{}{}})
	if err != nil {
		t.Fatalf("index_stats: %v", err)
	}
	var stats IndexStatsOutput
	mustDecodeStructured(t, res, &stats)
	if stats.EmbedPuts != 1 {
		t.Errorf("EmbedPuts=%d want 1 — the file should be embedded exactly once; stats=%+v", stats.EmbedPuts, stats)
	}
}

// TestSearchSemanticSurfacesEmbedErrors is the MCP-level regression for
// issue #305: when a file's embedding fails (here the mock Ollama 400s
// on file embeds while the query embed succeeds), the tool must not
// silently return "0 results" — it surfaces embed_errors > 0 and a
// warning, without failing the call.
func TestSearchSemanticSurfacesEmbedErrors(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "# Doc\n\nsome body text to embed\n")

	// First /api/embed call (the query) succeeds; every later call (the
	// per-file embeds) returns 400, mimicking an over-long / rejected
	// input.
	var mu sync.Mutex
	calls := 0
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			_ = json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0, 0}}})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
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

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search_semantic",
		Arguments: SearchSemanticInput{Query: "anything", Dir: dir, Threshold: fptr(0.4), Limit: 10},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("embed failure must not fail the tool call: %v", res.GetError())
	}
	var out SearchSemanticOutput
	mustDecodeStructured(t, res, &out)
	if out.EmbedErrors == 0 {
		t.Errorf("EmbedErrors=0 want > 0 — a failed embed was swallowed silently (#305); out=%+v", out)
	}
	if out.Warning == "" {
		t.Errorf("expected a warning when embeds fail; out=%+v", out)
	}
}
