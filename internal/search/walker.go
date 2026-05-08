package search

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// Result represents a matching file
type Result struct {
	Path        string
	ContentType string
	Size        int64
	// Attrs is set when Options.IncludeAttributes is true. Nil otherwise.
	// Carries the full FileAttributes that the CEL evaluator already built
	// for this file, so callers can render verbose / JSON / template output
	// without re-statting or re-parsing.
	Attrs *celexpr.FileAttributes
}

// Options configures the search
type Options struct {
	Root    string
	Expr    string
	Workers int
	// MaxLineBytes overrides the per-line scanner buffer cap honoured by the
	// text, csv, and html content types. Zero means use the package default
	// (see content.DefaultMaxLineBytes). Process-global; concurrent Walk
	// calls with different caps will race.
	MaxLineBytes int
	// IncludeAttributes, when true, populates Result.Attrs with the full
	// FileAttributes the CEL evaluator built. Off by default so the cheap
	// path-and-size case does not pay the pointer-keeping cost.
	IncludeAttributes bool
	// FS overrides the filesystem used for walking and IO. Defaults to
	// `os.DirFS(Root)` when nil. Tests inject embed.FS or fstest.MapFS for
	// hermetic execution; production almost never sets this.
	FS fs.FS
	// Index, when non-nil, is consulted by each worker to skip the
	// expensive ContentType.Attributes parse for files whose
	// (size, mtime) match a previous walk. The index handles its own
	// concurrency; workers never block on it.
	Index index.Index
}

// Walk walks the directory and returns every matching file. It is a
// thin wrapper over WalkStream that drains the channel into a slice.
// Use WalkStream directly when callers want to process matches as they
// arrive (incremental output, MCP progress notifications, bounded
// memory on huge result sets).
func Walk(ctx context.Context, opts Options, registry *content.Registry) ([]Result, error) {
	out := make(chan Result, 64)
	var results []Result
	var walkErr error
	done := make(chan struct{})
	go func() {
		walkErr = WalkStream(ctx, opts, registry, out)
		close(done)
	}()
	for r := range out {
		results = append(results, r)
	}
	<-done
	return results, walkErr
}

// WalkStream walks the directory and sends each matching file on out.
// out is closed before WalkStream returns; consumers should range over
// it. The error return reports walker setup failures (CEL compile,
// root open) and any error fs.WalkDir surfaces (cancellation,
// permission). Per-file scan failures are silently skipped — same
// semantics as Walk.
//
// out should be buffered. An unbuffered channel works but couples
// worker throughput to consumer speed; a buffer of opts.Workers or
// larger keeps producer and consumer loosely coupled.
//
// Cancellation propagates to three sites: the producer (fs.WalkDir
// callback), each worker's receive on the jobs channel, and the
// per-file ContentType.Attributes calls inside BuildAttributes.
func WalkStream(ctx context.Context, opts Options, registry *content.Registry, out chan<- Result) error {
	defer close(out)

	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	content.SetMaxLineBytes(opts.MaxLineBytes)

	evaluator, err := celexpr.New(opts.Expr)
	if err != nil {
		return err
	}

	fsys := opts.FS
	if fsys == nil {
		root := opts.Root
		if root == "" {
			root = "."
		}
		fsys = os.DirFS(root)
	}

	type job struct{ fsPath, displayPath string }
	jobs := make(chan job, opts.Workers*2)
	var wg sync.WaitGroup

	for range opts.Workers {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					attrs, err := celexpr.BuildAttributesWith(ctx, fsys, j.fsPath, j.displayPath, registry, celexpr.BuildOptions{Index: opts.Index})
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					if err != nil {
						continue
					}
					match, err := evaluator.Evaluate(attrs)
					if err != nil || !match {
						continue
					}
					r := Result{
						Path:        j.displayPath,
						ContentType: attrs.ContentType,
						Size:        attrs.Size,
					}
					if opts.IncludeAttributes {
						r.Attrs = attrs
					}
					select {
					case <-ctx.Done():
						return
					case out <- r:
					}
				}
			}
		})
	}

	walkErr := fs.WalkDir(fsys, ".", func(fsPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// User-facing path: OS-native join with Root when set, else the
		// fs-style path. Tests that pass an in-memory FS without a Root
		// see fs-style paths in Result.Path.
		displayPath := fsPath
		if opts.Root != "" {
			displayPath = filepath.Join(opts.Root, filepath.FromSlash(fsPath))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case jobs <- job{fsPath: fsPath, displayPath: displayPath}:
		}
		return nil
	})
	close(jobs)
	wg.Wait()

	return walkErr
}
