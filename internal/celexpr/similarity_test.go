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
	"github.com/richardwooding/ollamaembed"
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

	embedder := ollamaembed.NewOllama(srv.URL, "mock")
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

	embedder := ollamaembed.NewOllama(srv.URL, "mock")
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

// TestBuildAttributesWith_Similarity_ModelMismatch confirms that
// switching the embedding model between two walks invalidates the
// cached vector — the second walk bumps EmbedModelMismatches and
// re-embeds, rather than silently returning a Dot product against a
// vector from the wrong coordinate system.
func TestBuildAttributesWith_Similarity_ModelMismatch(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "content for model A then model B\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		// Same vector either way — the test cares about the model
		// identity check, not vector content.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{0.6, 0.8}},
		})
	}))
	defer srv.Close()

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()
	queryVec := []float32{0.6, 0.8}

	// Walk 1: populate cache with model A.
	embedderA := ollamaembed.NewOllama(srv.URL, "model-a")
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedderA,
		SemanticQueryEmbedding: queryVec,
	}); err != nil {
		t.Fatalf("walk A: %v", err)
	}
	if idx.Stats().EmbedPuts != 1 {
		t.Fatalf("after walk A: EmbedPuts=%d want 1; stats=%+v", idx.Stats().EmbedPuts, idx.Stats())
	}

	// Walk 2: same tree, different model. Must NOT report a hit;
	// must bump EmbedModelMismatches and re-ollamaembed.
	embedderB := ollamaembed.NewOllama(srv.URL, "model-b")
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedderB,
		SemanticQueryEmbedding: queryVec,
	}); err != nil {
		t.Fatalf("walk B: %v", err)
	}
	st := idx.Stats()
	if st.EmbedModelMismatches != 1 {
		t.Errorf("EmbedModelMismatches=%d want 1; stats=%+v", st.EmbedModelMismatches, st)
	}
	if st.EmbedHits != 0 {
		t.Errorf("EmbedHits=%d want 0 (model changed, must not hit); stats=%+v", st.EmbedHits, st)
	}
	if st.EmbedPuts != 2 {
		t.Errorf("EmbedPuts=%d want 2 (re-embed under model B); stats=%+v", st.EmbedPuts, st)
	}
	if calls != 2 {
		t.Errorf("Ollama calls=%d want 2 (one per walk); model mismatch should force re-embed", calls)
	}

	// Walk 3: back to model A. Cache now holds model B's vector, so
	// this is ANOTHER mismatch. Demonstrates the cache holds only
	// one model at a time per file — switching back and forth is
	// not free, but at least it's correct.
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedderA,
		SemanticQueryEmbedding: queryVec,
	}); err != nil {
		t.Fatalf("walk A again: %v", err)
	}
	st = idx.Stats()
	if st.EmbedModelMismatches != 2 {
		t.Errorf("EmbedModelMismatches=%d want 2; stats=%+v", st.EmbedModelMismatches, st)
	}
}

// TestBuildAttributesWith_Similarity_PreV154EntryRejected exercises
// the upgrade path: an Entry that pre-dates #154 has Vector populated
// but EmbedModel="". We must NOT trust such a vector. Post-#332 the live
// pipeline reads ChunkVectors, so a legacy single-Vector entry (no
// ChunkVectors) is simply a miss → re-embed into chunk vectors.
func TestBuildAttributesWith_Similarity_PreV154EntryRejected(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "legacy cache entry simulation\n")

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

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	// Manually seed the cache with a pre-#154-shaped entry: Vector
	// populated, EmbedModel empty (simulating a gob decode from a
	// cache file written before this PR).
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	cacheKey, _ := filepath.Abs(path)
	_ = idx.Put(cacheKey, &index.Entry{
		Size:            info.Size(),
		ModTimeUnixNano: info.ModTime().UnixNano(),
		ContentType:     "markdown",
		Vector:          []float32{0.6, 0.8},
		EmbedModel:      "", // pre-#154 entries have empty EmbedModel
	})

	embedder := ollamaembed.NewOllama(srv.URL, "mock")
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{0.6, 0.8},
	}); err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	st := idx.Stats()
	if st.EmbedHits != 0 {
		t.Errorf("EmbedHits=%d want 0 (must not trust a legacy single Vector); stats=%+v", st.EmbedHits, st)
	}
	if st.EmbedMisses != 1 {
		t.Errorf("EmbedMisses=%d want 1 (legacy Vector has no ChunkVectors → re-embed); stats=%+v", st.EmbedMisses, st)
	}
	if calls != 1 {
		t.Errorf("Ollama calls=%d want 1 (re-embed under the new model)", calls)
	}
	// The entry must now carry chunk vectors stamped with the model.
	if e, ok := idx.PeekAttrs(cacheKey); !ok || len(e.ChunkVectors) == 0 || e.EmbedModel != "mock" {
		t.Errorf("legacy entry not re-embedded into ChunkVectors; ok=%v entry=%+v", ok, e)
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

	embedder := ollamaembed.NewOllama(srv.URL, "missing-model")
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

	embedder := ollamaembed.NewOllama(srv.URL, "mock")

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
	embedder := ollamaembed.NewOllama("http://localhost:11434", "")

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

// mockEmbedServer returns an httptest Ollama mock that always answers
// with the given vector, plus a counter of how many embed calls it
// served. Shared by the cache-clobber regression tests below.
func mockEmbedServer(t *testing.T, vec []float32, calls *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{vec}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestBuildAttributesWith_Similarity_TruncatesEmbedInput is the
// regression for issue #305: the body handed to the embedder must be
// capped (default 8 KiB, or BuildOptions.EmbedInputMaxBytes) so
// book-length input doesn't trip the over-long-input errors some
// Ollama model/version combinations return.
func TestBuildAttributesWith_Similarity_TruncatesEmbedInput(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	// ~50 KiB of body text — well past any embed cap under test.
	mustWrite(t, path, "# Big\n\n"+strings.Repeat("lorem ipsum dolor ", 3000)+"\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	var mu sync.Mutex
	maxInput := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if len(body.Input) > maxInput {
			maxInput = len(body.Input)
		}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0, 0}}})
	}))
	defer srv.Close()

	run := func(cap, want int) {
		idx := index.NewMemory()
		defer func() { _ = idx.Close() }()
		mu.Lock()
		maxInput = 0
		mu.Unlock()
		_, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
			Index:                  idx,
			Embedder:               ollamaembed.NewOllama(srv.URL, "mock"),
			SemanticQueryEmbedding: []float32{1, 0, 0},
			EmbedInputMaxBytes:     cap,
		})
		if err != nil {
			t.Fatalf("cap=%d: %v", cap, err)
		}
		mu.Lock()
		got := maxInput
		mu.Unlock()
		if got > want {
			t.Errorf("cap=%d: embedder received %d bytes, want <= %d", cap, got, want)
		}
		if got == 0 {
			t.Errorf("cap=%d: embedder never called", cap)
		}
	}

	run(0, 8<<10)  // default 8 KiB
	run(2048, 2048) // explicit smaller cap
}

// TestBuildAttributesWith_Similarity_RetriesSmallerOnError verifies the
// #305 fallback: when the embedder rejects the (byte-capped) input
// because its TOKEN count is still over the model's hard limit — as
// dense / CJK text does — the walk retries once at a smaller size
// instead of dropping the file with a silent embed error.
func TestBuildAttributesWith_Similarity_RetriesSmallerOnError(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "dense.md")
	mustWrite(t, path, "# Dense\n\n"+strings.Repeat("token ", 20000)+"\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	// Mock rejects any input larger than 2 KiB (mimics a token-limit
	// rejection at the default 8 KiB cap), succeeds at/below it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if len(body.Input) > 2<<10 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0, 0}}})
	}))
	defer srv.Close()

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()
	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               ollamaembed.NewOllama(srv.URL, "mock"),
		SemanticQueryEmbedding: []float32{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if a.Similarity < 0.99 {
		t.Errorf("Similarity=%f want ~1.0 — retry should have embedded the file", a.Similarity)
	}
	st := idx.Stats()
	if st.EmbedErrors != 0 {
		t.Errorf("EmbedErrors=%d want 0 — the smaller-input retry should have succeeded; stats=%+v", st.EmbedErrors, st)
	}
	if st.EmbedPuts != 1 {
		t.Errorf("EmbedPuts=%d want 1 (vector cached after retry); stats=%+v", st.EmbedPuts, st)
	}
}

// TestBuildAttributesWith_Similarity_MissPathRetainsExtra is the
// regression for the cache-entry clobber behind issue #306 and
// Finding #5. On the cache-MISS path BuildAttributesWith used to Put
// an attribute entry (with parsed Extra) and then populateSimilarity
// Put a SEPARATE vector-only entry to the same key — last write wins,
// so the cached entry lost its Extra. A second semantic walk then
// served a vector-only cache hit with no title/word_count.
//
// Asserts that after one semantic walk the cached entry carries BOTH
// the embedding Vector AND the parsed Extra, and that a second walk
// reuses the vector (EmbedHits) while still surfacing word_count.
func TestBuildAttributesWith_Similarity_MissPathRetainsExtra(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "# My Title\n\nalpha beta gamma delta epsilon\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	calls := 0
	srv := mockEmbedServer(t, []float32{1, 0, 0}, &calls)
	embedder := ollamaembed.NewOllama(srv.URL, "mock")
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	opts := celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{1, 0, 0},
	}

	// Walk 1: cache miss → parse attrs + embed + cache both.
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts); err != nil {
		t.Fatalf("walk 1: %v", err)
	}

	// The single cached entry must carry both the vector and the Extra.
	e, ok := idx.PeekAttrs(abs)
	if !ok {
		t.Fatalf("no cached entry for %s after walk 1", abs)
	}
	if len(e.ChunkVectors) == 0 {
		t.Errorf("cached entry missing ChunkVectors after walk 1; entry=%+v", e)
	}
	if e.Extra == nil || e.Extra["word_count"] == nil {
		t.Errorf("cached entry lost Extra (clobber bug): Extra=%v", e.Extra)
	}

	// Walk 2: cache hit → reuse the vector (no Ollama call) AND still
	// surface the parsed attributes.
	a2, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("walk 2: %v", err)
	}
	if calls != 1 {
		t.Errorf("walk 2 made %d total Ollama calls; want 1 (vector should be cached)", calls)
	}
	if idx.Stats().EmbedHits < 1 {
		t.Errorf("walk 2: EmbedHits=%d want >= 1; stats=%+v", idx.Stats().EmbedHits, idx.Stats())
	}
	if a2.Extra == nil || a2.Extra["word_count"] == nil {
		t.Errorf("walk 2: cache hit dropped word_count (Finding #5): Extra=%v", a2.Extra)
	}
}

// TestBuildAttributesWith_Similarity_MixedTrafficCacheHit mirrors the
// MCP server flow that issue #306 was filed against: the shared index
// has already served a non-semantic query (so attributes are cached)
// before any semantic query runs. A subsequent repeated semantic query
// must reuse the cached embedding rather than re-embedding the file.
func TestBuildAttributesWith_Similarity_MixedTrafficCacheHit(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "# Doc\n\nsome searchable body text here\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	calls := 0
	srv := mockEmbedServer(t, []float32{1, 0, 0}, &calls)
	embedder := ollamaembed.NewOllama(srv.URL, "mock")
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	// Non-semantic walk first (no Embedder) — like a plain `search`.
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{Index: idx}); err != nil {
		t.Fatalf("non-semantic walk: %v", err)
	}

	semOpts := celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{1, 0, 0},
	}
	// First semantic walk embeds + caches the vector.
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), semOpts); err != nil {
		t.Fatalf("semantic walk 1: %v", err)
	}
	// Second semantic walk must hit the cache.
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), semOpts); err != nil {
		t.Fatalf("semantic walk 2: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 Ollama embed call across two semantic walks, got %d", calls)
	}
	if idx.Stats().EmbedHits < 1 {
		t.Errorf("EmbedHits=%d want >= 1; stats=%+v", idx.Stats().EmbedHits, idx.Stats())
	}
}

// TestBuildAttributesWith_Similarity_NonSemanticPreservesVector guards
// the cross-tool race on the shared MCP index: once a file's embedding
// is cached, a later non-semantic walk (plain search / stats) that
// re-stat'd the file must NOT overwrite the cached entry with a
// vector-less one. Re-running a non-semantic walk after a semantic
// walk and then a third semantic walk must still hit the vector.
func TestBuildAttributesWith_Similarity_NonSemanticPreservesVector(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.md")
	mustWrite(t, path, "# Title\n\nbody text body text\n")

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	calls := 0
	srv := mockEmbedServer(t, []float32{1, 0, 0}, &calls)
	embedder := ollamaembed.NewOllama(srv.URL, "mock")
	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	semOpts := celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{1, 0, 0},
	}

	// Semantic walk caches the vector.
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), semOpts); err != nil {
		t.Fatalf("semantic walk 1: %v", err)
	}
	// Non-semantic walk over the same file (no Embedder).
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{Index: idx}); err != nil {
		t.Fatalf("non-semantic walk: %v", err)
	}
	// Vector must survive the non-semantic walk.
	if e, ok := idx.PeekAttrs(abs); !ok || len(e.ChunkVectors) == 0 {
		t.Fatalf("non-semantic walk wiped the cached vector; ok=%v entry=%+v", ok, e)
	}
	// Third walk: semantic again, must hit (no new Ollama call).
	if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), semOpts); err != nil {
		t.Fatalf("semantic walk 2: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 Ollama call; vector should have survived the non-semantic walk, got %d", calls)
	}
}

// TestBuildAttributesWith_Similarity_DeepPassageViaChunks is the core
// #332 regression: a document whose relevant passage is buried far past
// the single-chunk embed cap must still score high, because the body is
// split into chunks and scored by MAX cosine. Before #332 only the
// opening ~8 KiB was embedded, so a match deep in a long document
// returned ~0 and the file was missed.
func TestBuildAttributesWith_Similarity_DeepPassageViaChunks(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "long.md")

	// Build a body far larger than the (small) chunk cap below, with the
	// target phrase buried near the end so it lands in a late chunk.
	const marker = "quantum entanglement teleportation protocol"
	filler := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 400)
	body := filler + "\n\n" + marker + "\n\n" + filler
	mustWrite(t, path, body)

	abs, _ := filepath.Abs(path)
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	// Mock Ollama: a chunk containing the marker embeds as [1,0,0]
	// (aligned with the query); every other chunk embeds as [0,1,0]
	// (orthogonal). The request carries {"input": "<chunk text>"}.
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		vec := []float32{0, 1, 0}
		if strings.Contains(req.Input, marker) {
			vec = []float32{1, 0, 0}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{vec},
		})
	}))
	defer srv.Close()

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()

	embedder := ollamaembed.NewOllama(srv.URL, "mock")
	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{
		Index:                  idx,
		Embedder:               embedder,
		SemanticQueryEmbedding: []float32{1, 0, 0},
		EmbedInputMaxBytes:     512, // force many chunks
	})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}

	// Max-sim must surface the deep marker chunk.
	if a.Similarity < 0.99 {
		t.Errorf("Similarity=%f want ~1.0 (deep marker chunk should dominate via max-sim)", a.Similarity)
	}
	// The body must have been split into more than one chunk (else this
	// test would pass even without chunking).
	if calls < 2 {
		t.Errorf("Ollama calls=%d want >1 (body should be chunked)", calls)
	}
	if e, ok := idx.PeekAttrs(abs); !ok || len(e.ChunkVectors) < 2 {
		t.Errorf("cached entry should hold multiple chunk vectors; ok=%v nChunks=%d", ok, len(e.ChunkVectors))
	}
}
