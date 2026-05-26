package search

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
)

// collectMatches runs Watch in a goroutine and returns a thread-safe
// accessor for the matches seen so far, plus a cancel func.
func startWatch(t *testing.T, opts Options) (paths func() []string, cancel func()) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	var mu sync.Mutex
	var seen []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Watch(ctx, opts, content.DefaultRegistry(), func(r Result) {
			mu.Lock()
			seen = append(seen, r.Path)
			mu.Unlock()
		})
	}()
	// Give the watcher a beat to register the initial directories
	// before the test starts creating files.
	time.Sleep(150 * time.Millisecond)
	return func() []string {
			mu.Lock()
			defer mu.Unlock()
			out := make([]string, len(seen))
			copy(out, seen)
			return out
		}, func() {
			cancelFn()
			<-done
		}
}

// waitFor polls cond until it's true or the deadline elapses.
func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestWatchFiresOnNewMatch(t *testing.T) {
	dir := t.TempDir()
	paths, cancel := startWatch(t, Options{Roots: []string{dir}, Expr: "is_markdown"})
	defer cancel()

	mdPath := filepath.Join(dir, "note.md")
	if err := os.WriteFile(mdPath, []byte("# hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !waitFor(func() bool {
		return slices.Contains(paths(), mdPath)
	}, 3*time.Second) {
		t.Errorf("watch did not fire for %s within 3s; saw %v", mdPath, paths())
	}
}

func TestWatchFiltersNonMatch(t *testing.T) {
	dir := t.TempDir()
	paths, cancel := startWatch(t, Options{Roots: []string{dir}, Expr: "is_markdown"})
	defer cancel()

	// A JSON file should NOT match is_markdown.
	if err := os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Give it time to (not) fire.
	time.Sleep(800 * time.Millisecond)
	if got := paths(); len(got) != 0 {
		t.Errorf("expected no matches for a JSON file under is_markdown, got %v", got)
	}
}

func TestWatchPicksUpNewSubdir(t *testing.T) {
	dir := t.TempDir()
	paths, cancel := startWatch(t, Options{Roots: []string{dir}, Expr: "is_markdown"})
	defer cancel()

	// Create a subdirectory AFTER the watch started, then a file in it.
	sub := filepath.Join(dir, "nested")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Let the watcher register the new subdir.
	time.Sleep(200 * time.Millisecond)
	nestedMd := filepath.Join(sub, "deep.md")
	if err := os.WriteFile(nestedMd, []byte("# deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !waitFor(func() bool {
		return slices.Contains(paths(), nestedMd)
	}, 3*time.Second) {
		t.Errorf("watch did not fire for file in new subdir %s; saw %v", nestedMd, paths())
	}
}

func TestWatchInvalidExpr(t *testing.T) {
	dir := t.TempDir()
	err := Watch(context.Background(), Options{Roots: []string{dir}, Expr: "this is not valid CEL %%%"}, content.DefaultRegistry(), func(Result) {})
	if err == nil {
		t.Error("expected an error compiling an invalid CEL expression")
	}
}

func TestWatchCancelReturns(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Watch(ctx, Options{Roots: []string{dir}, Expr: "true"}, content.DefaultRegistry(), func(Result) {})
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Watch returned error on cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Watch did not return within 2s of ctx cancel")
	}
}
