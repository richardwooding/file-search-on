package search

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// MaxWatchFDs bounds the number of file descriptors the recursive
// watcher will consume registering a tree. It is a var, not a const, so
// tests can lower it; operationally it should not need tuning.
//
// The cap exists because fsnotify's macOS / *BSD kqueue backend opens
// one descriptor per watched directory AND one per file inside it (it
// has to, to report which entry changed). A modest tree — a few
// thousand files — can therefore exhaust the per-process descriptor
// limit (kern.maxfilesperproc, commonly 10240) and freeze a long-lived
// server with EMFILE on every subsequent open. Once the estimated
// descriptor cost of the watched set reaches this budget, registration
// stops; the un-watched remainder falls back to lazy (size, mtime)
// revalidation on the next query — correctness is unaffected, only
// cache-warmth latency under churn. Issue #464.
var MaxWatchFDs = 4096

// fileWatchCostsFD reports whether the platform's fsnotify backend holds
// a descriptor per watched FILE (kqueue: macOS + the BSDs), as opposed
// to inotify (Linux) where only directories cost a watch. Drives whether
// files count against MaxWatchFDs.
func fileWatchCostsFD() bool {
	switch runtime.GOOS {
	case "darwin", "freebsd", "netbsd", "openbsd", "dragonfly":
		return true
	default:
		return false
	}
}

// watchBudget tracks the descriptor budget shared across a watcher's
// initial registration and any directories registered later (when a
// CREATE event arrives). truncated latches once the budget is hit so the
// caller can warn exactly once.
type watchBudget struct {
	remaining int
	truncated bool
}

// Watch sets up recursive filesystem watching across opts.Roots and
// calls onMatch once per newly-created / modified file that matches
// opts.Expr. It blocks until ctx is cancelled, then stops cleanly.
//
// The per-file evaluation mirrors the Walk path exactly — same
// BuildAttributesWith + Evaluate, so OCR / hashes / phash / body /
// xattrs / index caching all compose identically. Only create + write
// events are considered (deletes have no match to emit, so they're
// skipped here — see WatchIndex for the delete-aware consumer).
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

	buildOpts := opts.watchBuildOptions()

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

	return watchLoop(ctx, opts.Roots, opts.Excludes, opts.RespectGitignore, func(path string, op fsnotify.Op) {
		// The match tool only surfaces new/changed files; a removed or
		// renamed-away path has no match to emit.
		if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
			return
		}
		evalPath(path)
	})
}

// watchLoop is the shared fsnotify event engine behind Watch and
// WatchIndex. It registers every existing directory under each root
// (honouring globs + .gitignore), auto-registers directories created
// during the watch, and invokes onEvent once per debounced file event.
// It blocks until ctx is cancelled, then stops cleanly — all pending
// debounce timers are stopped so onEvent can't fire after watchLoop
// returns.
//
// onEvent receives the file path and the latest fsnotify.Op observed
// for it within the debounce window. Create/Write/Remove/Rename are all
// forwarded; directory creation is handled internally (recursive
// registration) and never forwarded. Consumers inspect op to decide
// what to do (Watch ignores Remove/Rename; WatchIndex evicts on them).
//
// Op coalescing is latest-wins: a quick create-then-remove resolves to
// Remove (don't act on a vanished file), and a write following a remove
// (a rename destination reusing a name) re-arms with the newer op.
func watchLoop(ctx context.Context, roots, globs []string, respectGitignore bool, onEvent func(path string, op fsnotify.Op)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }()

	// One descriptor budget shared by the initial registration and any
	// directories registered later (on CREATE). Warn at most once when it
	// truncates so a huge tree doesn't silently watch only a prefix.
	budget := &watchBudget{remaining: MaxWatchFDs}
	for _, root := range roots {
		if err := addDirsRecursive(watcher, root, globs, respectGitignore, budget); err != nil {
			return err
		}
	}
	warnedTruncated := false
	warnTruncated := func() {
		if budget.truncated && !warnedTruncated {
			warnedTruncated = true
			fmt.Fprintf(os.Stderr, "watch: tree exceeds the %d-descriptor watch budget; "+
				"watching a subset and relying on lazy revalidation for the rest "+
				"(results stay correct; narrow the root or exclude large dirs to silence this)\n", MaxWatchFDs)
		}
	}
	warnTruncated()

	// Per-path debounce state. Each entry holds the latest op and its
	// timer. A WaitGroup tracks scheduled-but-not-yet-finished timer
	// callbacks so the exit defer can DRAIN any in-flight onEvent before
	// watchLoop returns — otherwise a timer that fired just before
	// cancellation could run onEvent (touching a since-closed index)
	// after the caller believes the watcher has stopped.
	type pending struct {
		timer *time.Timer
		op    fsnotify.Op
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	timers := make(map[string]*pending)
	defer func() {
		mu.Lock()
		for _, p := range timers {
			if p.timer.Stop() {
				// Stopped before firing: its callback won't run, so
				// balance the Add we made when scheduling it.
				wg.Done()
			}
			// Stop()==false means the callback already ran or is running
			// (blocked on mu) — it will call wg.Done itself.
		}
		mu.Unlock()
		wg.Wait() // block until in-flight onEvent callbacks finish
	}()

	schedule := func(name string, op fsnotify.Op) {
		mu.Lock()
		defer mu.Unlock()
		if old, ok := timers[name]; ok {
			if old.timer.Stop() { // latest op wins; supersede prior timer
				wg.Done()
			}
		}
		wg.Add(1)
		p := &pending{op: op}
		p.timer = time.AfterFunc(watchDebounce, func() {
			defer wg.Done()
			mu.Lock()
			cur, ok := timers[name]
			// Only the most-recently-scheduled timer acts. A timer that
			// already fired but was superseded finds cur != p and bails,
			// so onEvent fires exactly once per debounce window.
			act := ok && cur == p
			if act {
				delete(timers, name)
			}
			mu.Unlock()
			if act {
				onEvent(name, p.op)
			}
		})
		timers[name] = p
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
			// isn't recursive). Register it and don't forward the event.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = addDirsRecursive(watcher, event.Name, globs, respectGitignore, budget)
					warnTruncated()
					continue
				}
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) ||
				event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				schedule(event.Name, event.Op)
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
// the watcher, skipping basenames matched by the excluder and the VCS
// metadata directory (.git — large, churny, and never something a search
// cares to watch; watching it alone can cost thousands of descriptors).
// Missing / unreadable directories are skipped, not fatal — the tree can
// change under us.
//
// Registration is bounded by budget: each watched directory and (on
// kqueue platforms) each file inside it draws down budget.remaining. When
// it reaches zero the walk stops and budget.truncated is set, so a
// long-lived watcher can't exhaust the process descriptor limit on a
// large tree (see MaxWatchFDs).
func addDirsRecursive(watcher *fsnotify.Watcher, root string, globs []string, respectGitignore bool, budget *watchBudget) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("watch root %s: %w", root, err)
	}
	if !info.IsDir() {
		// Watching a single file: register its parent so writes to it
		// surface (fsnotify watches directories, not files, reliably).
		return watcher.Add(filepath.Dir(root))
	}
	fileCosts := fileWatchCostsFD()
	// includeGit=false: the watcher already hard-skips .git below, and
	// never needs to watch VCS internals.
	excl := newExcluder(os.DirFS(root), globs, respectGitignore, false)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry; skip
		}
		if !d.IsDir() {
			// A file inside an already-Added directory still costs a
			// descriptor on kqueue platforms. Account for it so the budget
			// reflects real FD pressure, not just directory count.
			if fileCosts {
				budget.remaining--
				if budget.remaining <= 0 {
					budget.truncated = true
					return filepath.SkipAll
				}
			}
			return nil
		}
		if path != root {
			// Never watch the VCS metadata dir, and honour caller excludes
			// (build artefacts, node_modules, gitignored trees).
			if filepath.Base(path) == ".git" {
				return filepath.SkipDir
			}
			if rel, rerr := filepath.Rel(root, path); rerr == nil && excl.Match(filepath.ToSlash(rel), true) {
				return filepath.SkipDir
			}
		}
		if budget.remaining <= 0 {
			budget.truncated = true
			return filepath.SkipAll
		}
		_ = watcher.Add(path) // best-effort; a vanished dir is fine
		budget.remaining--
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
		VerifyC2PA:             opts.VerifyC2PA,
		WithPHash:              opts.WithPHash,
	}
}
