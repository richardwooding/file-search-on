package mcpserver

import (
	"context"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
	"github.com/richardwooding/file-search-on/internal/vecindex"
)

// annSearchCap bounds how many ranked files the ANN path materialises
// before filtering + paginating. Generous so deep cursor pages still
// work; the exact scan is O(n) regardless, so this only caps the
// post-scan slice.
const annSearchCap = 100_000

// semanticIndex is the MCP server's warm, in-memory vector index for
// semantic search (issue #335, part 2). It lets search_semantic answer a
// query without re-walking the filesystem or re-embedding, once a tree
// has been embedded by an earlier full walk.
//
// One vecindex.Index is kept per embedding model (vectors from different
// models live in incompatible coordinate systems). Each chunk vector is
// stored under the key "<absPath>\x00<chunkIndex>" so a document's best-
// matching chunk drives its score (the #332 chunked max-sim semantics),
// and the file path is recovered by splitting on the NUL.
//
// Coverage: the index only answers for a (dir, model) pair that a full
// walk has previously covered AND whose directory structure is unchanged
// since. "Unchanged" is a cheap recursive directory-mtime fingerprint —
// adding or removing a file bumps its parent directory's mtime, so the
// fingerprint detects set changes in O(dirs) without an O(files) walk.
// Per-file CONTENT edits don't change directory mtimes; those are caught
// at query time by re-stat'ing each returned candidate and dropping
// entries whose (size, mtime) no longer matches the cached vector.
type semanticIndex struct {
	idx index.Index

	mu       sync.Mutex
	byModel  map[string]*vecindex.Index // model → chunk-vector index
	coverage map[string]string          // "<model>\x00<absDir>" → dir fingerprint
}

func newSemanticIndex(idx index.Index) *semanticIndex {
	return &semanticIndex{
		idx:      idx,
		byModel:  make(map[string]*vecindex.Index),
		coverage: make(map[string]string),
	}
}

func chunkKey(absPath string, i int) string {
	return absPath + "\x00" + strconv.Itoa(i)
}

func fileFromChunkKey(key string) string {
	if before, _, ok := strings.Cut(key, "\x00"); ok {
		return before
	}
	return key
}

// chunkIndexFromKey recovers the chunk index encoded by chunkKey, or -1 when
// the key carries none (issue #366).
func chunkIndexFromKey(key string) int {
	if _, after, ok := strings.Cut(key, "\x00"); ok {
		if i, err := strconv.Atoi(after); err == nil {
			return i
		}
	}
	return -1
}

func coverageKey(model, absDir string) string { return model + "\x00" + absDir }

// Covered reports whether the index can answer a semantic query for
// (absDir, model) — i.e. a prior full walk warmed it AND the directory
// structure is unchanged since. Cheap: one recursive directory-mtime
// fingerprint, no per-file work.
func (s *semanticIndex) Covered(absDir, model string) bool {
	if model == "" {
		return false
	}
	s.mu.Lock()
	want, ok := s.coverage[coverageKey(model, absDir)]
	s.mu.Unlock()
	if !ok {
		return false
	}
	return want == dirFingerprint(absDir)
}

// Warm loads every cached chunk vector under absDir for the given model
// into the in-memory index and records the directory fingerprint as
// covered. Called after a full semantic walk completes (the walk has
// populated the attribute cache with vectors for all text files under
// absDir, regardless of the query's CEL filter — similarity is computed
// before the filter runs). Idempotent: re-warming refreshes vectors and
// the fingerprint.
func (s *semanticIndex) Warm(absDir, model string) {
	if s.idx == nil || model == "" {
		return
	}
	// ListAttrs is a substring match; narrow to a true prefix afterwards.
	summaries, _, err := s.idx.ListAttrs(absDir, 1<<30, 0)
	if err != nil {
		return
	}
	vi := s.indexFor(model)
	prefix := absDir + string(filepath.Separator)
	for _, sum := range summaries {
		if sum.Path != absDir && !strings.HasPrefix(sum.Path, prefix) {
			continue
		}
		e, ok := s.idx.PeekAttrs(sum.Path)
		if !ok || e.EmbedModel != model {
			continue
		}
		for i, v := range e.ChunkVectors {
			vi.Add(chunkKey(sum.Path, i), v)
		}
	}
	s.mu.Lock()
	s.coverage[coverageKey(model, absDir)] = dirFingerprint(absDir)
	s.mu.Unlock()
}

func (s *semanticIndex) indexFor(model string) *vecindex.Index {
	s.mu.Lock()
	defer s.mu.Unlock()
	vi, ok := s.byModel[model]
	if !ok {
		vi = vecindex.New()
		s.byModel[model] = vi
	}
	return vi
}

// fileHit is one file from a semantic query: the absolute path and the
// max cosine similarity across its chunks.
type fileHit struct {
	Path       string
	Similarity float64
	// ChunkIdx is the index (into the file's ChunkVectors / ChunkSpans) of
	// the chunk that produced Similarity — so the result can report which
	// function / region matched (issue #366). -1 if unknown.
	ChunkIdx int
}

// Search returns up to k files most similar to query under the model,
// scored by their best-matching chunk (max-sim, mirroring #332). It
// over-fetches chunk neighbours so multiple chunks of the same file
// collapse to one fileHit without starving the result. Returns nil when
// the model has no index.
func (s *semanticIndex) Search(model string, query []float32, k int) []fileHit {
	s.mu.Lock()
	vi := s.byModel[model]
	s.mu.Unlock()
	if vi == nil || k <= 0 {
		return nil
	}
	// Over-fetch chunks: a file can contribute many chunks, so fetch
	// generously to ensure k DISTINCT files survive the collapse.
	fetch := k*8 + 64
	neighbours := vi.Search(query, fetch)
	best := make(map[string]float64, len(neighbours))
	bestIdx := make(map[string]int, len(neighbours))
	order := make([]string, 0, len(neighbours))
	for _, n := range neighbours {
		f := fileFromChunkKey(n.Key)
		if cur, ok := best[f]; !ok || n.Similarity > cur {
			if !ok {
				order = append(order, f)
			}
			best[f] = n.Similarity
			bestIdx[f] = chunkIndexFromKey(n.Key)
		}
	}
	hits := make([]fileHit, 0, len(order))
	for _, f := range order {
		hits = append(hits, fileHit{Path: f, Similarity: best[f], ChunkIdx: bestIdx[f]})
	}
	sort.Slice(hits, func(a, b int) bool {
		if hits[a].Similarity != hits[b].Similarity {
			return hits[a].Similarity > hits[b].Similarity
		}
		return hits[a].Path < hits[b].Path
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

// Query runs the warm ANN fast path for a covered (absDir, model): it
// finds the most-similar files via the in-memory index, then for each
// candidate VERIFIES freshness (re-stat + cache (size,mtime) validation,
// dropping content-edited / vanished files whose cached vector is stale)
// and applies the folded CEL filter (expr + similarity threshold) over
// the cached attributes. No filesystem walk, no re-embedding. Returns
// the surviving results ranked by similarity desc, plus the number of
// stale candidates skipped (surfaced for observability). foldedExpr is
// the same "(expr) && similarity >= threshold" the walk path compiles.
func (s *semanticIndex) Query(ctx context.Context, absDir, model string, queryVec []float32, foldedExpr string, registry *content.Registry) ([]search.Result, int, error) {
	ev, err := celexpr.New(foldedExpr)
	if err != nil {
		return nil, 0, err
	}
	hits := s.Search(model, queryVec, annSearchCap)
	out := make([]search.Result, 0, len(hits))
	stale := 0
	for _, hit := range hits {
		if ctx.Err() != nil {
			return out, stale, ctx.Err()
		}
		fi, statErr := os.Stat(hit.Path)
		if statErr != nil {
			stale++ // file vanished since indexing
			continue
		}
		// Freshness gate: the cached vector is only valid if the file's
		// (size, mtime) still matches. A content edit invalidates it —
		// skip rather than rank on a stale vector.
		entry, ok := s.idx.Lookup(hit.Path, fi.Size(), fi.ModTime())
		if !ok {
			stale++
			continue
		}
		parent := filepath.Dir(hit.Path)
		base := filepath.Base(hit.Path)
		attrs, aerr := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, hit.Path, registry, celexpr.BuildOptions{Index: s.idx})
		if aerr != nil || attrs == nil {
			continue
		}
		attrs.Similarity = hit.Similarity
		// Report which chunk matched — the function span / line range of the
		// winning chunk (issue #366). Guarded: pre-#366 entries have vectors
		// but no spans.
		if entry != nil && hit.ChunkIdx >= 0 && hit.ChunkIdx < len(entry.ChunkSpans) {
			sp := entry.ChunkSpans[hit.ChunkIdx]
			attrs.MatchStartLine = sp.StartLine
			attrs.MatchEndLine = sp.EndLine
			attrs.MatchSymbol = sp.Symbol
		}
		if ok, eerr := ev.Evaluate(attrs); eerr != nil || !ok {
			continue
		}
		out = append(out, search.Result{Path: hit.Path, ContentType: attrs.ContentType, Size: attrs.Size, Attrs: attrs})
	}
	return out, stale, nil
}

// dirFingerprint hashes the recursive directory structure of root —
// every directory's relative path and modification time. Adding or
// removing an entry bumps the parent directory's mtime, so the
// fingerprint changes whenever the file SET changes, while staying
// O(dirs) (it never stats individual files). Unreadable trees / missing
// roots hash to a stable sentinel so a vanished dir reads as "changed".
func dirFingerprint(root string) string {
	h := fnv.New64a()
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Record the error path so a now-unreadable subtree changes
			// the fingerprint, but keep walking siblings.
			_, _ = h.Write([]byte("!" + path))
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// Skip .git — it isn't part of the searchable corpus, and its
		// contents mutate on every commit/fetch, which would otherwise
		// churn the fingerprint and spuriously invalidate the semantic
		// cache. The root itself is never a .git skip (path != root).
		if path != root && d.Name() == ".git" {
			return fs.SkipDir
		}
		info, ierr := d.Info()
		if ierr != nil {
			_, _ = h.Write([]byte("?" + path))
			return nil
		}
		_, _ = h.Write([]byte(path))
		_, _ = h.Write([]byte{0})
		var buf [8]byte
		n := info.ModTime().UnixNano()
		for i := range 8 {
			buf[i] = byte(n >> (8 * i))
		}
		_, _ = h.Write(buf[:])
		return nil
	})
	return strconv.FormatUint(h.Sum64(), 16)
}
