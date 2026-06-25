package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/projectdetect"
)

// IndexWatchStats counts the work a background WatchIndex maintainer
// performs over its lifetime. All counters are monotonic-since-start;
// read a consistent snapshot via Snapshot. The zero value is ready to
// use and safe for concurrent updates.
type IndexWatchStats struct {
	Refreshed atomic.Uint64 // cached entries re-parsed after a create/write
	Evicted   atomic.Uint64 // entries dropped after a remove/rename
	Errors    atomic.Uint64 // re-parse failures
}

// IndexWatchSnapshot is a plain-value snapshot of IndexWatchStats for
// reporting (MCP index_stats tool, dashboard).
type IndexWatchSnapshot struct {
	Refreshed uint64
	Evicted   uint64
	Errors    uint64
}

// Snapshot returns the current counters as plain values.
func (s *IndexWatchStats) Snapshot() IndexWatchSnapshot {
	if s == nil {
		return IndexWatchSnapshot{}
	}
	return IndexWatchSnapshot{
		Refreshed: s.Refreshed.Load(),
		Evicted:   s.Evicted.Load(),
		Errors:    s.Errors.Load(),
	}
}

// WatchIndex keeps the on-disk index cache fresh for the lifetime of a
// long-running server. It watches opts.Roots and, per debounced event:
//
//   - remove / rename → idx.Delete(path) (the one thing lazy
//     (size, mtime) validation never does — it leaves dead paths in the
//     bbolt file forever);
//   - create / write → re-parse the file via the standard
//     BuildAttributesWith path so the refreshed attributes land back in
//     the cache, BUT only when the path is ALREADY cached. The
//     conservative gate (idx.PeekAttrs) means we restore warmth to
//     entries an edit just staled rather than speculatively parsing
//     every transient new file (editor temp files, downloads-in-flight,
//     build output) under a churny tree.
//
// This is a latency / hygiene optimisation, NOT a correctness fix: lazy
// validation already re-parses a stale entry on the next lookup, so
// search results are always correct without the watcher. WatchIndex
// just keeps a warmed cache warm under churn and garbage-collects
// entries for deleted files.
//
// It blocks until ctx is cancelled, then returns cleanly. opts.Index
// must be non-nil; opts.Excludes / RespectGitignore / PruneBuildArtefacts
// are honoured so watched build-artefact dirs (node_modules, target, …)
// don't thrash the cache. stats may be nil.
func WatchIndex(ctx context.Context, opts Options, registry *content.Registry, idx index.Index, stats *IndexWatchStats) error {
	if idx == nil {
		return fmt.Errorf("WatchIndex: nil index")
	}
	if registry == nil {
		registry = content.DefaultRegistry()
	}

	// addDirsRecursive honours globs + .gitignore (and always skips .git)
	// but NOT PruneBuildArtefacts — unlike WalkStream, which unions the
	// build-excludes in. Resolve them once up front and merge so both the
	// directory registration and the per-event excluder skip build
	// artefacts. Mirrors walker.go's PruneBuildArtefacts handling.
	excludes := opts.Excludes
	if opts.PruneBuildArtefacts {
		for _, root := range opts.Roots {
			// Honour the user's excludes in the build-artefact pre-walk too
			// (projectdetect v0.4.0 also skips .git/.hg/.svn by default).
			if extra, err := projectdetect.CollectBuildExcludesWithOptions(ctx, root, projectdetect.FindOptions{Excludes: opts.Excludes}); err == nil {
				excludes = append(excludes, extra...)
			}
		}
	}

	buildOpts := opts.watchBuildOptions()

	// Per-event excluder keyed off the first root. fsnotify still fires
	// events for files inside watched dirs even if a specific basename
	// would be pruned, so we re-check each path's basename here.
	var excl *excluder
	if len(opts.Roots) > 0 {
		// includeGit=false: the background index maintainer never indexes
		// .git, regardless of any per-search include_git request.
		excl = newExcluder(os.DirFS(opts.Roots[0]), excludes, opts.RespectGitignore, false)
	}

	maintain := func(path string, op fsnotify.Op) {
		if ctx.Err() != nil {
			return
		}
		// Cache keys are absolute, cleaned paths (see celexpr's
		// BuildAttributesWith). Normalise the event path the same way so
		// PeekAttrs / Delete address the same entry BuildAttributesWith
		// would Put under.
		key := path
		if abs, err := filepath.Abs(path); err == nil {
			key = filepath.Clean(abs)
		}
		if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
			// The path is gone (rename source included — the destination
			// arrives as a separate create). Drop its cache entry.
			if err := idx.Delete(key); err == nil && stats != nil {
				stats.Evicted.Add(1)
			}
			return
		}
		// Create / Write. Skip excluded paths so a churny build dir
		// can't thrash the cache. Match against the root-relative path
		// (forward slashes) so gitignore path-patterns work, not just
		// basename globs.
		if excl != nil {
			rel := filepath.Base(path)
			if len(opts.Roots) > 0 {
				if r, err := filepath.Rel(opts.Roots[0], path); err == nil {
					rel = filepath.ToSlash(r)
				}
			}
			if excl.Match(rel, false) {
				return
			}
		}
		// Conservative gate: only refresh paths already in the cache.
		if _, ok := idx.PeekAttrs(key); !ok {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return // vanished between event and stat, or a directory
		}
		dir := filepath.Dir(path)
		base := filepath.Base(path)
		// The miss (the entry is now stale) makes BuildAttributesWith
		// re-parse and Put the fresh entry back itself — same side
		// effect warmIndex relies on. No explicit Put needed.
		if _, err := celexpr.BuildAttributesWith(ctx, os.DirFS(dir), base, path, registry, buildOpts); err != nil {
			if stats != nil {
				stats.Errors.Add(1)
			}
			return
		}
		if stats != nil {
			stats.Refreshed.Add(1)
		}
	}

	return watchLoop(ctx, opts.Roots, excludes, opts.RespectGitignore, maintain)
}
