package search

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
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
}

// Walk walks the directory and returns matching files
func Walk(ctx context.Context, opts Options, registry *content.Registry) ([]Result, error) {
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	content.SetMaxLineBytes(opts.MaxLineBytes)

	evaluator, err := celexpr.New(opts.Expr)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	var results []Result
	paths := make(chan string, opts.Workers*2)
	var wg sync.WaitGroup

	for i := 0; i < opts.Workers; i++ {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case path, ok := <-paths:
					if !ok {
						return
					}
					attrs, err := celexpr.BuildAttributes(ctx, path, registry)
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
						Path:        path,
						ContentType: attrs.ContentType,
						Size:        attrs.Size,
					}
					if opts.IncludeAttributes {
						r.Attrs = attrs
					}
					mu.Lock()
					results = append(results, r)
					mu.Unlock()
				}
			}
		})
	}

	walkErr := filepath.WalkDir(opts.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case paths <- path:
		}
		return nil
	})
	close(paths)
	wg.Wait()

	return results, walkErr
}
