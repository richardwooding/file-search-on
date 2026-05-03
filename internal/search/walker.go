package search

import (
	"context"
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
}

// Options configures the search
type Options struct {
	Root    string
	Expr    string
	Workers int
}

// Walk walks the directory and returns matching files
func Walk(ctx context.Context, opts Options, registry *content.Registry) ([]Result, error) {
	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}

	evaluator, err := celexpr.New(opts.Expr)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	var results []Result
	paths := make(chan string, opts.Workers*2)
	var wg sync.WaitGroup

	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range paths {
				attrs, err := celexpr.BuildAttributes(path, registry)
				if err != nil {
					continue
				}
				match, err := evaluator.Evaluate(attrs)
				if err != nil || !match {
					continue
				}
				mu.Lock()
				results = append(results, Result{
					Path:        path,
					ContentType: attrs.ContentType,
					Size:        attrs.Size,
				})
				mu.Unlock()
			}
		}()
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
