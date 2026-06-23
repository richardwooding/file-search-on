package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// startWatchIndex runs WatchIndex in a goroutine and returns its stats
// plus a cancel func that blocks until the watcher has fully drained.
func startWatchIndex(t *testing.T, opts Options, idx index.Index) (stats *IndexWatchStats, cancel func()) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	stats = &IndexWatchStats{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = WatchIndex(ctx, opts, content.DefaultRegistry(), idx, stats)
	}()
	// Let the watcher register the initial directories before the test
	// starts mutating files.
	time.Sleep(150 * time.Millisecond)
	return stats, func() {
		cancelFn()
		<-done
	}
}

// seedIndex parses path through the standard attribute path so it lands
// in idx (the same side-effect warmIndex relies on), and asserts the
// entry is now cached. Returns the cache key (abs, cleaned).
func seedIndex(t *testing.T, idx index.Index, path string) string {
	t.Helper()
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if _, err := celexpr.BuildAttributesWith(
		context.Background(), os.DirFS(dir), base, path,
		content.DefaultRegistry(), celexpr.BuildOptions{Index: idx},
	); err != nil {
		t.Fatalf("seed BuildAttributesWith: %v", err)
	}
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = filepath.Clean(abs)
	}
	if _, ok := idx.PeekAttrs(key); !ok {
		t.Fatalf("seed failed: %s not cached under key %s", path, key)
	}
	return key
}

func TestWatchIndexRefreshesStaleEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := index.NewMemory()
	key := seedIndex(t, idx, path)
	before, _ := idx.PeekAttrs(key)

	stats, cancel := startWatchIndex(t, Options{Roots: []string{dir}, Index: idx}, idx)
	defer cancel()

	// Grow the file. Sleep first so the mtime is observably newer (some
	// filesystems have coarse mtime granularity).
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(path, []byte("# hello\n\nmuch more content here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !waitFor(func() bool { return stats.Refreshed.Load() >= 1 }, 3*time.Second) {
		t.Fatalf("WatchIndex did not refresh; stats=%+v", stats.Snapshot())
	}
	after, ok := idx.PeekAttrs(key)
	if !ok {
		t.Fatal("entry vanished after refresh")
	}
	if after.Size <= before.Size {
		t.Errorf("expected refreshed size > %d, got %d", before.Size, after.Size)
	}
}

func TestWatchIndexEvictsDeletedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doomed.md")
	if err := os.WriteFile(path, []byte("# bye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := index.NewMemory()
	key := seedIndex(t, idx, path)

	stats, cancel := startWatchIndex(t, Options{Roots: []string{dir}, Index: idx}, idx)
	defer cancel()

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if !waitFor(func() bool {
		_, ok := idx.PeekAttrs(key)
		return !ok
	}, 3*time.Second) {
		t.Fatalf("WatchIndex did not evict deleted file; stats=%+v", stats.Snapshot())
	}
	if stats.Evicted.Load() < 1 {
		t.Errorf("expected Evicted >= 1, got %d", stats.Evicted.Load())
	}
}

func TestWatchIndexSkipsUncachedCreate(t *testing.T) {
	dir := t.TempDir()
	idx := index.NewMemory()

	stats, cancel := startWatchIndex(t, Options{Roots: []string{dir}, Index: idx}, idx)
	defer cancel()

	// Brand-new file that was never cached: the conservative gate must
	// skip it (no speculative warming of transient files).
	path := filepath.Join(dir, "fresh.md")
	if err := os.WriteFile(path, []byte("# fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait past the debounce window plus margin, then assert no refresh.
	time.Sleep(900 * time.Millisecond)
	if got := stats.Refreshed.Load(); got != 0 {
		t.Errorf("expected no refresh for uncached new file, got Refreshed=%d", got)
	}
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = filepath.Clean(abs)
	}
	if _, ok := idx.PeekAttrs(key); ok {
		t.Errorf("uncached new file should not have been added to the index")
	}
}

func TestWatchIndexRenameEvictsOldPath(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "before.md")
	if err := os.WriteFile(oldPath, []byte("# before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := index.NewMemory()
	oldKey := seedIndex(t, idx, oldPath)

	stats, cancel := startWatchIndex(t, Options{Roots: []string{dir}, Index: idx}, idx)
	defer cancel()

	newPath := filepath.Join(dir, "after.md")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}

	if !waitFor(func() bool {
		_, ok := idx.PeekAttrs(oldKey)
		return !ok
	}, 3*time.Second) {
		t.Fatalf("WatchIndex did not evict renamed-away path; stats=%+v", stats.Snapshot())
	}
}

func TestWatchIndexRespectsExcludes(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "node_modules")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "pkg.md")
	if err := os.WriteFile(path, []byte("# vendored\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := index.NewMemory()
	// Even if somehow cached, an excluded path must not be refreshed.
	key := seedIndex(t, idx, path)

	stats, cancel := startWatchIndex(t, Options{
		Roots:    []string{dir},
		Index:    idx,
		Excludes: []string{"node_modules"},
	}, idx)
	defer cancel()

	if err := os.WriteFile(path, []byte("# vendored\n\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(900 * time.Millisecond)
	if got := stats.Refreshed.Load(); got != 0 {
		t.Errorf("expected no refresh for excluded path, got Refreshed=%d", got)
	}
	_ = key
}

// TestWatchSkipsGitDir verifies the recursive watcher never registers the
// .git metadata directory: a cached file under .git must not be refreshed
// when it changes, because .git is never watched. (.git is huge and churny
// on real repos — watching it can cost thousands of descriptors and freeze
// a long-lived server. Issue #464.)
func TestWatchSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(gitDir, "HEAD")
	if err := os.WriteFile(path, []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := index.NewMemory()
	// Even if somehow cached, a path under .git must not be refreshed.
	key := seedIndex(t, idx, path)

	stats, cancel := startWatchIndex(t, Options{Roots: []string{dir}, Index: idx}, idx)
	defer cancel()

	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(path, []byte("ref: refs/heads/feature\nmore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(900 * time.Millisecond)
	if got := stats.Refreshed.Load(); got != 0 {
		t.Errorf("expected no refresh for path under .git, got Refreshed=%d", got)
	}
	_ = key
}

// TestAddDirsRecursiveRespectsBudget verifies that registration stops once
// the descriptor budget is exhausted, so a large tree can't drive a
// long-lived watcher into EMFILE (issue #464). With a budget of 10 and a
// root holding 50 subdirectories, exactly 10 directories (root + 9) get
// watched and the budget is flagged truncated.
func TestAddDirsRecursiveRespectsBudget(t *testing.T) {
	dir := t.TempDir()
	for i := range 50 {
		if err := os.Mkdir(filepath.Join(dir, fmt.Sprintf("d%02d", i)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()

	budget := &watchBudget{remaining: 10}
	if err := addDirsRecursive(w, dir, nil, false, budget); err != nil {
		t.Fatalf("addDirsRecursive: %v", err)
	}
	if !budget.truncated {
		t.Errorf("expected budget.truncated=true after exceeding watch budget")
	}
	if got := len(w.WatchList()); got != 10 {
		t.Errorf("expected exactly 10 watched dirs under a budget of 10, got %d", got)
	}
}

// TestWatchLoopDebounceCoalesces verifies the shared event loop collapses
// a rapid write burst into a single onEvent and that a create-then-remove
// within the window resolves to the final (remove) op.
func TestWatchLoopDebounceCoalesces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "burst.txt")

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var events []string // "op:base"
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = watchLoop(ctx, []string{dir}, nil, false, func(p string, op fsnotify.Op) {
			mu.Lock()
			defer mu.Unlock()
			tag := "write"
			if op.Has(fsnotify.Remove) || op.Has(fsnotify.Rename) {
				tag = "gone"
			}
			events = append(events, tag+":"+filepath.Base(p))
		})
	}()
	defer func() { cancel(); <-done }()
	time.Sleep(150 * time.Millisecond)

	// Rapid create-then-remove inside the debounce window.
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("xy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if !waitFor(func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(events) >= 1
	}, 3*time.Second) {
		t.Fatal("watchLoop emitted no event")
	}
	// Allow any stragglers to land.
	time.Sleep(400 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 coalesced event, got %d: %v", len(events), events)
	}
	if events[0] != "gone:burst.txt" {
		t.Errorf("expected final op to be the remove (gone:burst.txt), got %q", events[0])
	}
}
