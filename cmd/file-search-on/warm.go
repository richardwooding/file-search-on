package main

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// warmWorkers returns the worker count to use for the warmer when the
// user didn't pin one explicitly. A quarter of NumCPU (floor 1) is the
// CPU-budget mechanism: enough parallelism to make headway on cold
// trees, but a small enough share of cores that the MCP server, the
// agent driving it, and whatever else is running on the box stay
// responsive. No rate-limiter / sleep is layered on top — capping
// workers is sufficient because walk-and-extract is I/O-bound for most
// content types.
func warmWorkers(requested int) int {
	if requested > 0 {
		return requested
	}
	if n := runtime.NumCPU() / 4; n > 0 {
		return n
	}
	return 1
}

// warmIndex walks root in the background and lets each cache-miss
// land in idx as a side-effect of the standard WalkStream attribute-
// extraction path. No expensive flags (hashes, OCR, body, snippet,
// phash, xattrs) are enabled — only the cheap detector + per-type
// Attributes() parse, the same path a normal MCP search call exercises
// on a cold tree. Result of the walk is discarded; the drainer goroutine
// counts files for the completion log line.
//
// Errors from WalkStream (root open failure, CEL compile failure) are
// returned to the caller, which logs them and continues — the MCP
// server's lifecycle is independent of the warmer.
func warmIndex(ctx context.Context, idx index.Index, root string, workers int, log io.Writer) error {
	workers = warmWorkers(workers)
	opts := search.Options{
		Root: root,
		// Match every file — what we want is the side-effect of each
		// file's ContentType.Attributes() being parsed and Put into
		// the cache. The CEL evaluator's empty-expr default ("true")
		// would also work, but being explicit reads clearer.
		Expr:                "true",
		Workers:             workers,
		Index:               idx,
		IncludeAttributes:   false,
		RespectGitignore:    true,
		PruneBuildArtefacts: true,
	}
	out := make(chan search.Result, workers*2)
	var scanned atomic.Int64
	done := make(chan struct{})
	go func() {
		for range out {
			scanned.Add(1)
		}
		close(done)
	}()
	start := time.Now()
	err := search.WalkStream(ctx, opts, contentpkg.DefaultRegistry(), out)
	// WalkStream closes out before returning; just wait for the
	// drainer to finish counting.
	<-done
	elapsed := time.Since(start).Round(time.Millisecond)
	if log != nil {
		if err != nil {
			_, _ = fmt.Fprintf(log, "warm: %d files in %s (err: %v)\n", scanned.Load(), elapsed, err)
		} else {
			_, _ = fmt.Fprintf(log, "warm: %d files in %s\n", scanned.Load(), elapsed)
		}
	}
	return err
}

// warmEmbeddings walks root and populates index.Entry.Vector for every
// walked file, using the supplied embedder. Like warmIndex it leans on
// the existing search.WalkStream machinery — but with an Embedder and
// a SemanticQueryEmbedding plumbed through so the walker's existing
// populateSimilarity path fires per file. The side-effect we care about
// is the Vector cache fill; the per-file similarity scores against the
// dummy query are computed and discarded.
//
// populateSimilarity short-circuits on len(SemanticQueryEmbedding) == 0,
// so we must pass a non-empty query vector. We satisfy that by embedding
// a single-character string up front — one Ollama round-trip,
// sub-second on a healthy server. Errors from that initial query embed
// (Ollama unreachable, model missing) are returned without starting
// the walk.
//
// IncludeBody is set to true because populateSimilarity reads each
// file's body to feed Embedder.Embed; that makes the embeddings warm
// substantially more expensive per file than warmIndex's attribute-only
// pass.
func warmEmbeddings(ctx context.Context, idx index.Index, root string, workers int, embedder embed.Embedder, log io.Writer) error {
	workers = warmWorkers(workers)
	queryVec, err := embedder.Embed(ctx, " ")
	if err != nil {
		return fmt.Errorf("warm: embed dummy query: %w", err)
	}
	embed.Normalize(queryVec)

	opts := search.Options{
		Root:                   root,
		Expr:                   "true",
		Workers:                workers,
		Index:                  idx,
		IncludeAttributes:      false,
		IncludeBody:            true,
		RespectGitignore:       true,
		PruneBuildArtefacts:    true,
		Embedder:               embedder,
		SemanticQueryEmbedding: queryVec,
	}
	out := make(chan search.Result, workers*2)
	var scanned atomic.Int64
	done := make(chan struct{})
	go func() {
		for range out {
			scanned.Add(1)
		}
		close(done)
	}()
	start := time.Now()
	err = search.WalkStream(ctx, opts, contentpkg.DefaultRegistry(), out)
	<-done
	elapsed := time.Since(start).Round(time.Millisecond)
	if log != nil {
		if err != nil {
			_, _ = fmt.Fprintf(log, "warm-embeddings (%s): %d files in %s (err: %v)\n", embedder.Model(), scanned.Load(), elapsed, err)
		} else {
			_, _ = fmt.Fprintf(log, "warm-embeddings (%s): %d files in %s\n", embedder.Model(), scanned.Load(), elapsed)
		}
	}
	return err
}
