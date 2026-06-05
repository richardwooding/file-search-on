// Package vecindex is an in-memory vector index for semantic search
// (issue #335, part 2). It lets the MCP server answer "top-K most
// similar to this query vector" over a large, long-lived corpus WITHOUT
// re-walking the filesystem or re-embedding — the headline win of #335:
// a warm, server-resident semantic index.
//
// The backend is an EXACT brute-force top-K dot-product scan, not an
// approximate-nearest-neighbour graph. That choice is deliberate: the
// obvious pure-Go HNSW library (coder/hnsw v0.6.1) returned ~12% recall@5
// against a 100%-recall brute-force baseline on realistic 768-dim
// embeddings (and shipped a backwards CosineDistance), so an approximate
// index would have silently dropped relevant files. An exact scan over
// tens of thousands of vectors is sub-10ms — fast enough for the scale
// this project targets (the README already notes brute-force cosine is
// "fast enough for tens of thousands of files"). The Index interface is
// kept narrow so a vetted ANN backend can drop in later without changing
// callers.
//
// Keys are opaque strings (callers map them to files / chunks). Vectors
// must be L2-normalised — Search reports the cosine (== dot on unit
// vectors) and does NOT normalise.
package vecindex

import (
	"sort"
	"sync"

	"github.com/richardwooding/file-search-on/internal/embed"
)

// Index is a thread-safe, exact in-memory vector index keyed by string.
// All vectors must share a dimensionality (one embedding model); the
// server keeps a separate Index per model.
type Index struct {
	mu   sync.RWMutex
	vecs map[string][]float32
}

// New returns an empty index.
func New() *Index {
	return &Index{vecs: make(map[string][]float32)}
}

// Add inserts or replaces the vector for key. vec must be L2-normalised.
// A nil/empty vector is ignored. The slice is retained (not copied) — the
// caller must not mutate it after Add.
func (i *Index) Add(key string, vec []float32) {
	if len(vec) == 0 {
		return
	}
	i.mu.Lock()
	i.vecs[key] = vec
	i.mu.Unlock()
}

// Delete removes key. Reports whether it was present.
func (i *Index) Delete(key string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	_, ok := i.vecs[key]
	delete(i.vecs, key)
	return ok
}

// Has reports whether key is indexed.
func (i *Index) Has(key string) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	_, ok := i.vecs[key]
	return ok
}

// Len is the number of indexed vectors.
func (i *Index) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.vecs)
}

// Neighbour is one search result: the stored key and its cosine
// similarity to the query (higher = more similar).
type Neighbour struct {
	Key        string
	Similarity float64
}

// Search returns up to k nearest neighbours of query by cosine
// similarity, descending. query must be L2-normalised. Exact (100%
// recall). Returns nil when the index is empty or k <= 0.
func (i *Index) Search(query []float32, k int) []Neighbour {
	if k <= 0 || len(query) == 0 {
		return nil
	}
	i.mu.RLock()
	scored := make([]Neighbour, 0, len(i.vecs))
	for key, v := range i.vecs {
		scored = append(scored, Neighbour{Key: key, Similarity: embed.Dot(query, v)})
	}
	i.mu.RUnlock()

	if len(scored) == 0 {
		return nil
	}
	// Sort by similarity desc, key asc on ties for a deterministic order.
	sort.Slice(scored, func(a, b int) bool {
		if scored[a].Similarity != scored[b].Similarity {
			return scored[a].Similarity > scored[b].Similarity
		}
		return scored[a].Key < scored[b].Key
	})
	if len(scored) > k {
		scored = scored[:k]
	}
	return scored
}
