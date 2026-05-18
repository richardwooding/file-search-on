package celexpr_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/index"
)

// TestBuildAttributesWith_Similarity_OffByDefault confirms that
// without SemanticQueryEmbedding + Embedder set, Similarity stays 0.
func TestBuildAttributesWith_Similarity_OffByDefault(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "# Heading\n\nbody body body\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.Similarity != 0 {
		t.Errorf("Similarity=%f want 0 (no embedding configured)", a.Similarity)
	}
}

// TestBuildAttributesWith_Similarity_PopulatesViaMock confirms the
// full path: Embedder is invoked, vector cached in the in-memory
// index, FileAttributes.Similarity populated. Uses an httptest
// Ollama mock so CI doesn't need a live model.
func TestBuildAttributesWith_Similarity_PopulatesViaMock(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "machine learning research paper\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	// Mock Ollama returns [1, 0, 0] for any input — every file
	// gets the same vector. Query is also [1, 0, 0] (normalised).
	// Expected cosine = 1.0.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{1, 0, 0}},
		})
	}))
	defer srv.Close()

	embedder := embed.NewOllama(srv.URL, "mock")
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	queryVec := []float32{1, 0, 0}
	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: queryVec,
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.Similarity < 0.99 {
		t.Errorf("Similarity=%f want ~1.0 (identical mock vectors)", a.Similarity)
	}
	st := idx.Stats()
	if st.EmbedPuts < 1 {
		t.Errorf("expected EmbedPuts >= 1; stats=%+v", st)
	}
}

// TestBuildAttributesWith_Similarity_CacheHit confirms a second call
// against the same (path, size, mtime) reuses the cached vector and
// skips the Ollama call.
func TestBuildAttributesWith_Similarity_CacheHit(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "cached content\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.6, 0.8}},
		})
	}))
	defer srv.Close()

	embedder := embed.NewOllama(srv.URL, "mock")
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	queryVec := []float32{0.6, 0.8}
	opts := celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: queryVec,
	}

	// First call: cache miss → Ollama call → put.
	_, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if calls != 1 {
		t.Fatalf("first call made %d Ollama calls; want 1", calls)
	}
	if idx.Stats().EmbedPuts == 0 {
		t.Errorf("first call: expected EmbedPuts > 0")
	}

	// Second call: same (size, mtime) → cache hit → no Ollama call.
	_, err = celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if calls != 1 {
		t.Errorf("second call made an Ollama call; total=%d want 1", calls)
	}
	if idx.Stats().EmbedHits < 1 {
		t.Errorf("second call: expected EmbedHits >= 1; stats=%+v", idx.Stats())
	}
}

// TestEvaluate_SimilarityFilter exercises the CEL predicate from a
// synthesised FileAttributes.
func TestEvaluate_SimilarityFilter(t *testing.T) {
	high := &celexpr.FileAttributes{Similarity: 0.85}
	mid := &celexpr.FileAttributes{Similarity: 0.55}
	none := &celexpr.FileAttributes{Similarity: 0}

	ev, err := celexpr.New(`similarity > 0.7`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if m, _ := ev.Evaluate(high); !m {
		t.Errorf("similarity 0.85 should match > 0.7")
	}
	if m, _ := ev.Evaluate(mid); m {
		t.Errorf("similarity 0.55 should NOT match > 0.7")
	}
	if m, _ := ev.Evaluate(none); m {
		t.Errorf("similarity 0 should NOT match > 0.7")
	}
}

// TestBuildAttributesWith_Similarity_EmbedError surfaces as embed
// error counter but doesn't fail the walk.
func TestBuildAttributesWith_Similarity_EmbedError(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "x\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	embedder := embed.NewOllama(srv.URL, "missing-model")
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{1, 0},
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith should not fail on embed error: %v", err)
	}
	if a.Similarity != 0 {
		t.Errorf("Similarity=%f want 0 on embed error", a.Similarity)
	}
	if idx.Stats().EmbedErrors < 1 {
		t.Errorf("expected EmbedErrors > 0 after 404; stats=%+v", idx.Stats())
	}
}

// TestBuildAttributesWith_Similarity_BinarySkipped: image / video /
// archive content shouldn't even attempt embedding.
func TestBuildAttributesWith_Similarity_BinarySkipped(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	// PNG magic header — detects as image/png, not text.
	path := filepath.Join(dir, "f.png")
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\nIHDRsmall"), 0o644); err != nil {
		t.Fatal(err)
	}

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1}}})
	}))
	defer srv.Close()

	embedder := embed.NewOllama(srv.URL, "mock")

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{1},
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.Similarity != 0 {
		t.Errorf("Similarity=%f want 0 (binary content type)", a.Similarity)
	}
	if calls != 0 {
		t.Errorf("Ollama called %d times for a binary file; want 0", calls)
	}
}

// TestEmbedder_NoModel_Surfaces from a celexpr-level perspective:
// when the Embedder returns ErrNoModel, the walk still completes
// (similarity stays 0; the call doesn't fail).
func TestEmbedder_NoModel_Surfaces(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "x\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	// No model set → ErrNoModel on first Embed call.
	embedder := embed.NewOllama("http://localhost:11434", "")

	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{1},
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith should not fail when Embedder returns ErrNoModel: %v", err)
	}
	if a.Similarity != 0 {
		t.Errorf("Similarity=%f want 0 with no model", a.Similarity)
	}
}
