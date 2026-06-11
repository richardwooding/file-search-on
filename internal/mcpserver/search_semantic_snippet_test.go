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

// TestSearchSemanticMatchSnippet covers the opt-in matched-region snippet
// (#366 follow-up): a query that matches one function returns that function's
// source inline as match_snippet, the flag is required to populate it, and an
// over-tight snippet_lines truncates with a marker.
func TestSearchSemanticMatchSnippet(t *testing.T) {
	dir := t.TempDir()
	src := "package demo\n" + // 1
		"\n" + // 2
		"import \"fmt\"\n" + // 3
		"\n" + // 4
		"func add(a, b int) int {\n" + // 5
		"\treturn a + b\n" + // 6
		"}\n" + // 7
		"\n" + // 8
		"func greet() {\n" + // 9
		"\tfmt.Println(\"hello there\")\n" + // 10
		"}\n" // 11
	mustWrite(t, filepath.Join(dir, "demo.go"), src)

	// Mock Ollama: return [1,0,0] for any text mentioning "greet" (the query
	// and the greet function chunk), [0,1,0] otherwise — so the greet chunk
	// wins the max-sim and the matched region is that function.
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		vec := []float32{0, 1, 0}
		if strings.Contains(body.Input, "greet") {
			vec = []float32{1, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{vec}})
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

	call := func(in SearchSemanticInput) SearchSemanticOutput {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search_semantic", Arguments: in})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if res.GetError() != nil {
			t.Fatalf("tool error: %v", res.GetError())
		}
		var out SearchSemanticOutput
		mustDecodeStructured(t, res, &out)
		return out
	}

	// With the flag: match points at greet and the snippet carries its source.
	out := call(SearchSemanticInput{Query: "greet", Dir: dir, Threshold: new(0.5), Limit: 5, IncludeMatchSnippet: true})
	if len(out.Matches) != 1 {
		t.Fatalf("want 1 match, got %d: %+v", len(out.Matches), out.Matches)
	}
	m := out.Matches[0]
	if m.MatchSymbol != "greet" || m.MatchStartLine != 9 || m.MatchEndLine != 11 {
		t.Fatalf("match span = %q %d-%d, want greet 9-11", m.MatchSymbol, m.MatchStartLine, m.MatchEndLine)
	}
	if !strings.Contains(m.MatchSnippet, "func greet()") || !strings.Contains(m.MatchSnippet, "hello there") {
		t.Errorf("MatchSnippet missing function source:\n%s", m.MatchSnippet)
	}
	if strings.Contains(m.MatchSnippet, "func add") {
		t.Errorf("MatchSnippet leaked a different function:\n%s", m.MatchSnippet)
	}

	// Without the flag: no snippet (but the range still surfaces).
	noFlag := call(SearchSemanticInput{Query: "greet", Dir: dir, Threshold: new(0.5), Limit: 5})
	if noFlag.Matches[0].MatchSnippet != "" {
		t.Errorf("MatchSnippet should be empty without include_match_snippet, got %q", noFlag.Matches[0].MatchSnippet)
	}
	if noFlag.Matches[0].MatchSymbol != "greet" {
		t.Errorf("range/symbol should still surface without the flag, got symbol %q", noFlag.Matches[0].MatchSymbol)
	}

	// Tight cap: the 3-line function truncates with a marker.
	trunc := call(SearchSemanticInput{Query: "greet", Dir: dir, Threshold: new(0.5), Limit: 5, IncludeMatchSnippet: true, SnippetLines: 1})
	if !strings.Contains(trunc.Matches[0].MatchSnippet, "truncated") {
		t.Errorf("snippet_lines=1 should truncate with a marker, got:\n%s", trunc.Matches[0].MatchSnippet)
	}
}
