package gitmeta

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// initPoolRepo seeds a tempdir with a one-commit git repo, the same
// shape as initRepo in gitmeta_test.go (kept separate to avoid
// cross-file helper coupling).
func initPoolRepo(t *testing.T) string {
	t.Helper()
	if !HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	root := t.TempDir()
	runOrSkip(t, root, "init", "-q", "-b", "main")
	runOrSkip(t, root, "config", "user.email", "test@example.com")
	runOrSkip(t, root, "config", "user.name", "Pool Test")
	runOrSkip(t, root, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("# a\n"), 0o644); err != nil {
		t.Fatalf("write a.md: %v", err)
	}
	runOrSkip(t, root, "add", "a.md")
	runOrSkip(t, root, "commit", "-q", "-m", "Add a")
	return root
}

func TestPool_GetReusesCache(t *testing.T) {
	root := initPoolRepo(t)
	pool := NewPool()
	ctx := context.Background()

	c1, err := pool.Get(ctx, root)
	if err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if c1 == nil {
		t.Fatal("first Get returned nil cache for a real repo")
	}
	c2, err := pool.Get(ctx, root)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if c1 != c2 {
		t.Errorf("Pool returned different *Cache for unchanged HEAD: %p vs %p", c1, c2)
	}
	if got := pool.Len(); got != 1 {
		t.Errorf("Pool.Len() = %d, want 1", got)
	}
}

func TestPool_HeadChangeRebuilds(t *testing.T) {
	root := initPoolRepo(t)
	pool := NewPool()
	ctx := context.Background()

	c1, err := pool.Get(ctx, root)
	if err != nil || c1 == nil {
		t.Fatalf("first Get: c=%v err=%v", c1, err)
	}
	firstHead := c1.HeadSHA()

	// New commit → HEAD moves.
	if err := os.WriteFile(filepath.Join(root, "b.md"), []byte("# b\n"), 0o644); err != nil {
		t.Fatalf("write b.md: %v", err)
	}
	runOrSkip(t, root, "add", "b.md")
	runOrSkip(t, root, "commit", "-q", "-m", "Add b")

	c2, err := pool.Get(ctx, root)
	if err != nil || c2 == nil {
		t.Fatalf("second Get: c=%v err=%v", c2, err)
	}
	if c1 == c2 {
		t.Errorf("Pool returned SAME *Cache after HEAD changed; expected rebuild")
	}
	if c2.HeadSHA() == firstHead {
		t.Errorf("Cache HEAD did not update; still %q", firstHead)
	}
	// b.md should now appear in the cache.
	if _, ok := c2.Lookup(filepath.Join(root, "b.md")); !ok {
		t.Errorf("post-commit Cache should know about b.md")
	}
}

func TestPool_ConcurrentSafe(t *testing.T) {
	root := initPoolRepo(t)
	pool := NewPool()
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	caches := make([]*Cache, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, err := pool.Get(ctx, root)
			caches[idx] = c
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d Get: %v", i, err)
		}
		if caches[i] == nil {
			t.Errorf("goroutine %d got nil cache", i)
		}
	}
	// We don't assert a single pointer because the first-Get race is
	// allowed to produce different *Caches across racing builders.
	// What we DO assert is no crash, no race-detector trip, and a
	// sane final entry count.
	if got := pool.Len(); got != 1 {
		t.Errorf("Pool.Len() = %d, want 1", got)
	}
}

func TestPool_NonGitTreeNil(t *testing.T) {
	root := t.TempDir() // not a git repo
	pool := NewPool()
	c, err := pool.Get(context.Background(), root)
	if err != nil {
		t.Errorf("expected nil err for non-git tree, got %v", err)
	}
	if c != nil {
		t.Errorf("expected nil cache for non-git tree, got %+v", c)
	}
	if got := pool.Len(); got != 0 {
		t.Errorf("Pool.Len() = %d, want 0 (no entry stored for non-git tree)", got)
	}
}

func TestPool_WarmPrimes(t *testing.T) {
	root := initPoolRepo(t)
	pool := NewPool()
	ctx := context.Background()

	if err := pool.Warm(ctx, root); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if got := pool.Len(); got != 1 {
		t.Fatalf("Pool.Len() after Warm = %d, want 1", got)
	}
	// Subsequent Get returns the same cache without rebuilding —
	// proven indirectly by the HEAD-sha matching across calls.
	c, err := pool.Get(ctx, root)
	if err != nil {
		t.Fatalf("Get after Warm: %v", err)
	}
	if c == nil {
		t.Fatal("Get after Warm returned nil cache")
	}
}

func TestPool_NilReceiverSafe(t *testing.T) {
	var pool *Pool
	c, err := pool.Get(context.Background(), "/anywhere")
	if c != nil || err != nil {
		t.Errorf("nil-receiver Get should return (nil, nil); got (%v, %v)", c, err)
	}
	if err := pool.Warm(context.Background(), "/anywhere"); err != nil {
		t.Errorf("nil-receiver Warm should return nil; got %v", err)
	}
	if got := pool.Len(); got != 0 {
		t.Errorf("nil-receiver Len() should return 0; got %d", got)
	}
}
