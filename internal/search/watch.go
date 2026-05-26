package search

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
)

// watchDebounce coalesces rapid write bursts into a single evaluation
// per path. Editors and downloaders emit several WRITE events for one
// logical save / download; without debouncing the same file would
// match (and emit) multiple times. 300ms is below human-perceptible
// latency yet long enough to swallow a multi-write save.
const watchDebounce = 300 * time.Millisecond

// Watch sets up recursive filesystem watching across opts.Roots and
// calls onMatch once per newly-created / modified file that matches
// opts.Expr. It blocks until ctx is cancelled, then stops cleanly.
//
// The per-file evaluation mirrors the Walk path exactly — same
// BuildAttributesWith + Evaluate, so OCR / hashes / phash / body /
// xattrs / index caching all compose identically. Only create + write
// events are considered (deletes are out of scope per issue #211).
//
// fsnotify is not recursive: every existing directory under each root
// is registered up front, and any directory created during the watch
// is registered when its CREATE event arrives. Files created inside a
// brand-new directory in the window before its watch is added can be
// missed — an inherent fsnotify race, acceptable for a "tell me about
// new matches" tool.
//
// Issue #211.
func Watch(ctx context.Context, opts Options, registry *content.Registry, onMatch func(Result)) error {
	if registry == nil {
		registry = content.DefaultRegistry()
	}
	expr := opts.Expr
	if expr == "" {
		expr = "true"
	}
	evaluator, err := celexpr.New(expr)
	if err != nil {
		return fmt.Errorf("compiling CEL expression: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	for _, root := range opts.Roots {
		if err := addDirsRecursive(watcher, root, opts.Excludes, opts.RespectGitignore); err != nil {
			return err
		}
	}

	buildOpts := opts.watchBuildOptions()

	// Per-path debounce timers. Stopped on exit so a pending timer
	// can't call onMatch after Watch returns.
	var mu sync.Mutex
	timers := make(map[string]*time.Timer)
	defer func() {
		mu.Lock()
		for _, t := range timers {
			t.Stop()
		}
		mu.Unlock()
	}()

	evalPath := func(path string) {
		if ctx.Err() != nil {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return // vanished, or a directory (handled separately)
		}
		dir := filepath.Dir(path)
		base := filepath.Base(path)
		attrs, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), base, path, registry, buildOpts)
		if err != nil {
			return
		}
		match, err := evaluator.Evaluate(attrs)
		if err != nil || !match {
			return
		}
		r := Result{Path: path, ContentType: attrs.ContentType, Size: attrs.Size}
		if opts.IncludeAttributes {
			r.Attrs = attrs
		}
		onMatch(r)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// A newly-created directory must be watched too (fsnotify
			// isn't recursive). Register it and skip evaluation.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = addDirsRecursive(watcher, event.Name, opts.Excludes, opts.RespectGitignore)
					continue
				}
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				name := event.Name
				mu.Lock()
				if t, ok := timers[name]; ok {
					t.Stop()
				}
				timers[name] = time.AfterFunc(watchDebounce, func() {
					mu.Lock()
					delete(timers, name)
					mu.Unlock()
					evalPath(name)
				})
				mu.Unlock()
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			// Best-effort: a transient watcher error (e.g. a removed
			// directory's watch going stale) shouldn't tear down the
			// whole watch. Keep going.
		}
	}
}

// addDirsRecursive registers root and every subdirectory under it with
// the watcher, skipping basenames matched by the excluder. Missing /
// unreadable directories are skipped, not fatal — the tree can change
// under us.
func addDirsRecursive(watcher *fsnotify.Watcher, root string, globs []string, respectGitignore bool) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("watch root %s: %w", root, err)
	}
	if !info.IsDir() {
		// Watching a single file: register its parent so writes to it
		// surface (fsnotify watches directories, not files, reliably).
		return watcher.Add(filepath.Dir(root))
	}
	excl := newExcluder(os.DirFS(root), globs, respectGitignore)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry; skip
		}
		if !d.IsDir() {
			return nil
		}
		if path != root {
			if rel, rerr := filepath.Rel(root, path); rerr == nil && excl.Match(filepath.ToSlash(rel), true) {
				return filepath.SkipDir
			}
		}
		_ = watcher.Add(path) // best-effort; a vanished dir is fine
		return nil
	})
}

// watchBuildOptions maps the subset of search.Options that make sense
// for per-event evaluation onto celexpr.BuildOptions. Ranking,
// semantic embedding, and project resolution are omitted — they're
// walk-collection concerns, not single-file-event concerns.
func (opts Options) watchBuildOptions() celexpr.BuildOptions {
	return celexpr.BuildOptions{
		Index:                  opts.Index,
		IncludeBody:            opts.IncludeBody,
		BodyMaxBytes:           opts.BodyMaxBytes,
		ComputeHashes:          opts.ComputeHashes,
		CheckDisguised:         opts.CheckDisguised,
		ReadExtendedAttributes: opts.ReadExtendedAttributes,
		Allowlist:              opts.Allowlist,
		Denylist:               opts.Denylist,
		OCRImages:              opts.OCRImages,
		OCRTimeout:             opts.OCRTimeout,
		WithPHash:              opts.WithPHash,
	}
}
