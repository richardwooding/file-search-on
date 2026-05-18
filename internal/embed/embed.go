// Package embed provides text-to-vector embedding via Ollama's local
// HTTP API. Vector embeddings power the `similarity` CEL variable
// and the `search_semantic` MCP tool (PR for issue #151).
//
// Ollama is the only embedding source supported in v1 — it's the
// canonical local-model runner, exposes a stable HTTP API on
// localhost:11434, and lets the user pick the model via one-time
// `ollama pull <name>`. No SDK, no CGo, no bundled model.
//
// The Embedder interface is small enough that other embedding
// sources (OpenAI API, llama.cpp HTTP, etc.) can plug in later
// without rewiring callers — same Embed(ctx, text) shape.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder turns text into a fixed-size embedding vector. Implementations
// are expected to be safe for concurrent Embed calls (the OllamaEmbedder
// uses an *http.Client which is concurrency-safe).
type Embedder interface {
	// Embed returns the embedding vector for the given text. Honours
	// ctx; HTTP-backed implementations route ctx through
	// http.NewRequestWithContext so cancellation propagates.
	Embed(ctx context.Context, text string) ([]float32, error)
	// Model returns the model identifier the embedder is configured
	// to use. Used in error messages and telemetry.
	Model() string
}

// OllamaEmbedder calls Ollama's local /api/embed endpoint. Lazy
// connect — the first Embed call performs the only HTTP round-trip
// needed to detect Ollama-not-running / model-not-pulled. Server
// construction does no network I/O.
//
// Wire shape (Ollama 0.1.34+):
//
//	POST /api/embed
//	{"model": "<name>", "input": "..."}
//
//	200
//	{"embeddings": [[float32, ...]], ...}
type OllamaEmbedder struct {
	model  string
	server string
	client *http.Client
}

// NewOllama returns an embedder pointed at the given Ollama server.
// server is the base URL (e.g. "http://localhost:11434"); model is
// the embedding model name (e.g. "nomic-embed-text"). Empty strings
// surface as errors on the first Embed call rather than at
// construction time — callers can pass partial config from MCP
// startup flags + per-call overrides without preflight checks.
func NewOllama(server, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		model:  model,
		server: strings.TrimRight(server, "/"),
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// ErrNoModel is returned by Embed when no model has been configured
// (neither at server startup nor per-call). Surfaces cleanly in MCP
// responses as "no embedding model configured".
var ErrNoModel = errors.New("no embedding model configured (pass --embedding-model or per-call 'model')")

// ErrNoServer is returned by Embed when no server URL has been
// configured. Practically rare since OllamaEmbedder defaults to
// localhost:11434, but defensive against an empty override.
var ErrNoServer = errors.New("no embedding server configured")

func (o *OllamaEmbedder) Model() string { return o.model }

// Embed POSTs the text to Ollama's /api/embed endpoint and returns
// the embedding vector. The returned vector is NOT pre-normalised —
// callers should call Normalize before storing or comparing if they
// want cosine-similarity semantics.
//
// Errors surface from three places: missing configuration (ErrNoModel
// / ErrNoServer), transport (the HTTP call itself — server
// unreachable, timeout, etc.), and protocol (non-200 response, empty
// embedding array). Each carries a wrapped underlying error so
// callers can errors.Is past the wrapper.
func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	if o.model == "" {
		return nil, ErrNoModel
	}
	if o.server == "" {
		return nil, ErrNoServer
	}
	reqBody, err := json.Marshal(map[string]any{
		"model": o.model,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.server+"/api/embed", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call ollama at %s: %w", o.server, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status %d (model %q)", resp.StatusCode, o.model)
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
		// Older Ollama versions returned a singular "embedding"
		// at the top level. Accept it as a fallback.
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode ollama response: %w", err)
	}
	if len(out.Embeddings) > 0 && len(out.Embeddings[0]) > 0 {
		return out.Embeddings[0], nil
	}
	if len(out.Embedding) > 0 {
		return out.Embedding, nil
	}
	return nil, fmt.Errorf("ollama returned no embedding for model %q", o.model)
}

// Cosine returns the cosine similarity of two vectors, in [-1, 1].
// Returns 0 when either vector is empty OR their lengths disagree —
// defensive against partially-populated cache entries. Vectors don't
// need to be pre-normalised; the function divides by the L2 norms
// itself. For pre-normalised vectors, prefer Dot which skips the
// norm computation.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		da := float64(a[i])
		db := float64(b[i])
		dot += da * db
		normA += da * da
		normB += db * db
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// Dot returns the dot product of two vectors. Used as a fast path
// when both vectors are pre-normalised (then dot == cosine). Returns
// 0 on length mismatch.
func Dot(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

// Normalize L2-normalises v in place. After this call, Dot(v, w)
// for any normalised w equals the cosine similarity. No-op when v
// has zero norm.
func Normalize(v []float32) {
	if len(v) == 0 {
		return
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
}
