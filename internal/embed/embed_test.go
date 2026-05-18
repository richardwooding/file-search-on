package embed_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/embed"
)

// TestOllamaEmbedder_HappyPath confirms a successful round-trip
// against a mock Ollama server. The mock returns a known vector so
// we can assert on the exact response.
func TestOllamaEmbedder_HappyPath(t *testing.T) {
	want := []float32{0.1, 0.2, 0.3}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		var body struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Model == "" || body.Input == "" {
			http.Error(w, "empty model/input", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{want},
		})
	}))
	defer srv.Close()

	e := embed.NewOllama(srv.URL, "nomic-embed-text")
	got, err := e.Embed(t.Context(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d-dim, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%f want %f", i, got[i], want[i])
		}
	}
	if e.Model() != "nomic-embed-text" {
		t.Errorf("Model()=%q", e.Model())
	}
}

// TestOllamaEmbedder_LegacyShape covers the older Ollama response
// format that returns a singular `embedding` at the top level
// instead of `embeddings[0]`.
func TestOllamaEmbedder_LegacyShape(t *testing.T) {
	want := []float32{0.5, 0.5}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embedding": want,
		})
	}))
	defer srv.Close()

	e := embed.NewOllama(srv.URL, "legacy-model")
	got, err := e.Embed(t.Context(), "x")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d-dim, want 2", len(got))
	}
}

// TestOllamaEmbedder_NoModel returns ErrNoModel without any HTTP
// call when model is empty.
func TestOllamaEmbedder_NoModel(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		calls++
	}))
	defer srv.Close()

	e := embed.NewOllama(srv.URL, "")
	_, err := e.Embed(t.Context(), "x")
	if !errors.Is(err, embed.ErrNoModel) {
		t.Errorf("err=%v want ErrNoModel", err)
	}
	if calls != 0 {
		t.Errorf("no-model path made %d HTTP calls; want 0", calls)
	}
}

// TestOllamaEmbedder_HTTPError surfaces a wrapped error when the
// server returns non-200.
func TestOllamaEmbedder_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model not found", http.StatusNotFound)
	}))
	defer srv.Close()

	e := embed.NewOllama(srv.URL, "missing-model")
	_, err := e.Embed(t.Context(), "x")
	if err == nil {
		t.Fatalf("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error doesn't mention 404: %v", err)
	}
}

// TestOllamaEmbedder_Unreachable confirms a transport-level error
// when the server isn't reachable.
func TestOllamaEmbedder_Unreachable(t *testing.T) {
	e := embed.NewOllama("http://127.0.0.1:1", "model")
	_, err := e.Embed(t.Context(), "x")
	if err == nil {
		t.Fatalf("expected transport error against unreachable port")
	}
}

// TestCosine_Identity: cosine(v, v) == 1.0 for any non-zero vector.
func TestCosine_Identity(t *testing.T) {
	v := []float32{1, 2, 3, 4}
	got := embed.Cosine(v, v)
	if math.Abs(got-1.0) > 1e-9 {
		t.Errorf("cosine(v,v)=%f want 1.0", got)
	}
}

// TestCosine_Orthogonal: cosine of two perpendicular vectors == 0.
func TestCosine_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	got := embed.Cosine(a, b)
	if math.Abs(got) > 1e-9 {
		t.Errorf("cosine(perpendicular)=%f want 0", got)
	}
}

// TestCosine_Antipodal: cosine of antipodal vectors == -1.
func TestCosine_Antipodal(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{-1, -2}
	got := embed.Cosine(a, b)
	if math.Abs(got+1.0) > 1e-9 {
		t.Errorf("cosine(antipodal)=%f want -1.0", got)
	}
}

// TestCosine_LengthMismatch returns 0 defensively.
func TestCosine_LengthMismatch(t *testing.T) {
	if embed.Cosine([]float32{1, 2, 3}, []float32{1, 2}) != 0 {
		t.Errorf("length mismatch should yield 0")
	}
	if embed.Cosine(nil, []float32{1}) != 0 {
		t.Errorf("nil vector should yield 0")
	}
}

// TestNormalize: after Normalize, the L2 norm is 1.0.
func TestNormalize(t *testing.T) {
	v := []float32{3, 4} // norm 5
	embed.Normalize(v)
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(norm)-1.0) > 1e-6 {
		t.Errorf("post-Normalize L2=%f want 1.0", math.Sqrt(norm))
	}
	if math.Abs(float64(v[0])-0.6) > 1e-6 || math.Abs(float64(v[1])-0.8) > 1e-6 {
		t.Errorf("Normalize([3,4])=%v want [0.6, 0.8]", v)
	}
}

// TestNormalize_ZeroVector leaves a zero vector unchanged (avoiding
// division by zero).
func TestNormalize_ZeroVector(t *testing.T) {
	v := []float32{0, 0, 0}
	embed.Normalize(v)
	for i, x := range v {
		if x != 0 {
			t.Errorf("v[%d]=%f want 0 after Normalize of zero vector", i, x)
		}
	}
}

// TestDot is the fast path for pre-normalised vectors.
func TestDot(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	if embed.Dot(a, b) != 0 {
		t.Errorf("dot of perpendicular should be 0")
	}
	if embed.Dot([]float32{1, 2}, []float32{1, 2}) != 5 {
		t.Errorf("dot([1,2],[1,2]) should be 5")
	}
}

// TestEmbedThenNormalize confirms the end-to-end pipeline: the
// pre-normalised dot product equals the un-normalised cosine
// similarity, justifying the fast-path optimisation in the cache.
func TestEmbedThenNormalize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{3, 4}}, // norm 5
		})
	}))
	defer srv.Close()

	e := embed.NewOllama(srv.URL, "model")
	v, err := e.Embed(t.Context(), "x")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	cosBefore := embed.Cosine(v, []float32{3, 4})
	embed.Normalize(v)
	dotAfter := embed.Dot(v, []float32{0.6, 0.8})
	if math.Abs(cosBefore-dotAfter) > 1e-6 {
		t.Errorf("Cosine pre-Normalize (%f) != Dot post-Normalize (%f)", cosBefore, dotAfter)
	}
}

// TestOllamaEmbedder_ContextCancel honours ctx cancellation.
func TestOllamaEmbedder_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hold the response so the client-side context cancellation
		// fires first. Wait until the ctx is done — that's what
		// http.NewRequestWithContext + the client's transport will
		// observe.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		cancel()
	}()

	e := embed.NewOllama(srv.URL, "model")
	_, err := e.Embed(ctx, "x")
	if err == nil {
		t.Fatalf("expected error from canceled ctx")
	}
}
