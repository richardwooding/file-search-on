package celexpr_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/ollamaembed"
)

// matchVec returns [1,0,0] for any chunk whose text mentions needle, else
// [0,1,0]. With a query of [1,0,0], the chunk containing needle wins the
// max-sim, so we can assert which function the hit points at.
func needleEmbedServer(t *testing.T, needle string, calls *int) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		*calls++
		mu.Unlock()
		vec := []float32{0, 1, 0}
		if strings.Contains(body.Input, needle) {
			vec = []float32{1, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{vec}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBuildAttributesWith_FunctionLevelMatch: a Go source file chunked by
// function span surfaces the matching function's name + line range (issue
// #366), and a second walk reuses the cache (no re-embed) — proving the
// "symbol" scheme is stamped and accepted.
func TestBuildAttributesWith_FunctionLevelMatch(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.go")
	src := "package demo\n" + // 1
		"\n" + // 2
		"import \"fmt\"\n" + // 3
		"\n" + // 4
		"func add(a, b int) int {\n" + // 5
		"\treturn a + b\n" + // 6
		"}\n" + // 7
		"\n" + // 8
		"func retryWithBackoff() {\n" + // 9
		"\tfmt.Println(\"retry\")\n" + // 10
		"}\n" // 11
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	calls := 0
	srv := needleEmbedServer(t, "retryWithBackoff", &calls)

	build := func(idx index.Index) *celexpr.FileAttributes {
		a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
			Index:                  idx,
			Embedder:               ollamaembed.NewOllama(srv.URL, "mock"),
			SemanticQueryEmbedding: []float32{1, 0, 0},
		})
		if err != nil {
			t.Fatalf("BuildAttributesWith: %v", err)
		}
		return a
	}

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	a := build(idx)
	if a.MatchSymbol != "retryWithBackoff" {
		t.Errorf("MatchSymbol = %q, want retryWithBackoff", a.MatchSymbol)
	}
	if a.MatchStartLine != 9 || a.MatchEndLine != 11 {
		t.Errorf("match range = %d-%d, want 9-11", a.MatchStartLine, a.MatchEndLine)
	}
	if a.Similarity < 0.99 {
		t.Errorf("Similarity = %f, want ~1.0", a.Similarity)
	}

	// Second walk: same model + (size, mtime) → cache hit, no new embed calls,
	// and the match span still surfaces from the cached ChunkSpans.
	callsAfterFirst := calls
	a2 := build(idx)
	if calls != callsAfterFirst {
		t.Errorf("second walk made %d new embed calls, want 0 (cache hit)", calls-callsAfterFirst)
	}
	if a2.MatchSymbol != "retryWithBackoff" || a2.MatchStartLine != 9 {
		t.Errorf("cached match = %q %d, want retryWithBackoff 9", a2.MatchSymbol, a2.MatchStartLine)
	}
}

// TestBuildAttributesWith_NonSourceMatchHasNoSymbol: a non-source file still
// reports a line range for the matched byte window, but with no symbol.
func TestBuildAttributesWith_NonSourceMatchHasNoSymbol(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# Notes\n\nsome retry guidance here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(path)

	calls := 0
	srv := needleEmbedServer(t, "retry", &calls)
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(filepath.Dir(abs)), filepath.Base(abs), abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               ollamaembed.NewOllama(srv.URL, "mock"),
		SemanticQueryEmbedding: []float32{1, 0, 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.MatchSymbol != "" {
		t.Errorf("non-source MatchSymbol = %q, want empty", a.MatchSymbol)
	}
	if a.MatchStartLine < 1 {
		t.Errorf("non-source should still report a line range, got StartLine %d", a.MatchStartLine)
	}
}
