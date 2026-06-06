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

// TestSearchSemantic_ThresholdZeroIsNoFloor is the #349 regression: an
// OMITTED threshold defaults to 0.5 (drops weak matches), while an
// EXPLICIT threshold:0 means "no floor" and returns every ranked file —
// previously 0 was coerced to 0.5 so callers couldn't express it.
func TestSearchSemantic_ThresholdZeroIsNoFloor(t *testing.T) {
	dir := t.TempDir()
	const marker = "transformer"
	mustWrite(t, filepath.Join(dir, "match.md"), "# p\n\n"+strings.Repeat(marker+" attention ", 20))
	mustWrite(t, filepath.Join(dir, "other.md"), "# m\n\n"+strings.Repeat("gardening compost ", 20))

	// match.md → query-aligned [1,0,0] (sim 1.0); other.md → orthogonal
	// [0,1,0] (sim 0.0). With the 0.5 default, only match.md survives; with
	// no floor, both do.
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
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil)
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
		if res.IsError {
			t.Fatalf("tool error: %v", res.GetError())
		}
		var out SearchSemanticOutput
		mustDecodeStructured(t, res, &out)
		return out
	}

	// Omitted threshold → default 0.5 → only match.md (sim 1.0) survives.
	def := call(SearchSemanticInput{Query: marker, Dir: dir, Limit: 10})
	if def.SimilarityThreshold != 0.5 {
		t.Errorf("omitted threshold: similarity_threshold=%v want 0.5", def.SimilarityThreshold)
	}
	if def.Count != 1 {
		t.Errorf("omitted threshold (0.5): count=%d want 1 (orthogonal file dropped); %+v", def.Count, def.Matches)
	}

	// Explicit threshold:0 → no floor → both files returned.
	zero := call(SearchSemanticInput{Query: marker, Dir: dir, Threshold: fptr(0), Limit: 10})
	if zero.SimilarityThreshold != 0 {
		t.Errorf("threshold:0: similarity_threshold=%v want 0 (not coerced to 0.5) — #349", zero.SimilarityThreshold)
	}
	if zero.Count != 2 {
		t.Errorf("threshold:0 (no floor): count=%d want 2 (both files) — #349", zero.Count)
	}
}
