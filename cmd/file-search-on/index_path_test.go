package main

import (
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestDefaultIndexPath_PerCWD locks in the per-cwd path-keying
// behaviour: distinct working directories produce distinct filenames,
// the same working directory produces the same filename
// (deterministic), and same-basename + different-parent directories
// don't collide because the hash differs.
func TestDefaultIndexPath_PerCWD(t *testing.T) {
	t.Parallel()

	a, err := defaultIndexPath("/Users/example/Code/foo")
	if err != nil {
		t.Fatalf("defaultIndexPath A: %v", err)
	}
	b, err := defaultIndexPath("/Users/example/Code/bar")
	if err != nil {
		t.Fatalf("defaultIndexPath B: %v", err)
	}
	if a == b {
		t.Errorf("distinct cwds produced the same path; got %s for both A and B", a)
	}

	// Deterministic: same input → same output.
	again, err := defaultIndexPath("/Users/example/Code/foo")
	if err != nil {
		t.Fatalf("defaultIndexPath A (2nd call): %v", err)
	}
	if again != a {
		t.Errorf("non-deterministic path for the same cwd: first %s, second %s", a, again)
	}

	// Same basename, different parent → different paths (hash should
	// differentiate). This is the "multiple ~/.../foo/ dirs" scenario.
	c, err := defaultIndexPath("/Users/other/work/foo")
	if err != nil {
		t.Fatalf("defaultIndexPath C: %v", err)
	}
	if c == a {
		t.Errorf("same-basename different-parent dirs collided; both → %s", a)
	}
	// Both should still mention the basename "foo".
	if !strings.Contains(filepath.Base(a), "foo") {
		t.Errorf("basename of %s does not contain 'foo'", a)
	}
	if !strings.Contains(filepath.Base(c), "foo") {
		t.Errorf("basename of %s does not contain 'foo'", c)
	}

	// File extension must be .db so `ls *.db` finds them.
	for _, p := range []string{a, b, c} {
		if !strings.HasSuffix(p, ".db") {
			t.Errorf("path %s missing .db suffix", p)
		}
	}
}

// TestSanitiseBasename covers the dir-name → filename component
// transformation: allowed characters survive, anything else becomes
// underscore, and length is capped.
func TestSanitiseBasename(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"file-search-on":  "file-search-on",
		"Hello World":     "Hello_World",
		"a/b/c":           "a_b_c", // / shouldn't reach this func, but defensive
		"weird:name?":     "weird_name_",
		"normal_dir.v2":   "normal_dir.v2",
		"":                "",
	}
	for in, want := range cases {
		got := sanitiseBasename(in)
		if got != want {
			t.Errorf("sanitiseBasename(%q) = %q, want %q", in, got, want)
		}
	}
	// Length cap.
	long := strings.Repeat("x", maxBasenameLen+20)
	got := sanitiseBasename(long)
	if len(got) != maxBasenameLen {
		t.Errorf("expected sanitiseBasename to cap at %d chars, got %d", maxBasenameLen, len(got))
	}
}

// TestResolveIndexBackend_NoIndex confirms that passing noIndex=true
// short-circuits to an in-memory index with the expected reason,
// regardless of the path argument.
func TestResolveIndexBackend_NoIndex(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "/tmp/should-be-ignored.db"} {
		idx, backend, err := resolveIndexBackend("", path, true, index.BodyCacheCap{})
		if err != nil {
			t.Fatalf("resolveIndexBackend noIndex with path=%q: %v", path, err)
		}
		if idx == nil {
			t.Fatalf("resolveIndexBackend returned nil index for noIndex=true (path=%q)", path)
		}
		if backend.Mode != BackendInMemory {
			t.Errorf("backend.Mode = %q, want %q", backend.Mode, BackendInMemory)
		}
		if backend.Reason != ReasonNoIndexFlag {
			t.Errorf("backend.Reason = %q, want %q", backend.Reason, ReasonNoIndexFlag)
		}
		if backend.Path != "" {
			t.Errorf("backend.Path = %q, want empty (noIndex skips disk)", backend.Path)
		}
		_ = idx.Close()
	}
}

// TestResolveIndexBackend_ExplicitPath_HappyPath verifies that
// passing a fresh path opens a persistent bbolt index and reports
// it as such.
func TestResolveIndexBackend_ExplicitPath_HappyPath(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "explicit.db")
	idx, backend, err := resolveIndexBackend("", path, false, index.BodyCacheCap{})
	if err != nil {
		t.Fatalf("resolveIndexBackend explicit path: %v", err)
	}
	defer func() { _ = idx.Close() }()
	if backend.Mode != BackendPersistent {
		t.Errorf("backend.Mode = %q, want %q", backend.Mode, BackendPersistent)
	}
	if backend.Path != path {
		t.Errorf("backend.Path = %q, want %q", backend.Path, path)
	}
	if backend.Reason != "" {
		t.Errorf("backend.Reason = %q, want empty for happy-path persistent open", backend.Reason)
	}
}

// TestResolveIndexBackend_StaleSchemaRebuilds pins the #418 fix: a cache
// file from an incompatible schema/version is transparently rebuilt rather
// than erroring out the process (the old behaviour forced the user to find
// and delete the file). Seeds a bbolt file with a stale meta payload, then
// asserts resolveIndexBackend returns a working persistent index.
func TestResolveIndexBackend_StaleSchemaRebuilds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.db")

	// Seed a file whose meta claims an old schema version — the same
	// shape an older binary would have left behind.
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		bk, err := tx.CreateBucket([]byte("meta"))
		if err != nil {
			return err
		}
		return bk.Put([]byte("schema"), []byte(`{"schema_version":1,"encoding":"gob"}`))
	}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	_ = db.Close()

	idx, backend, err := resolveIndexBackend("", path, false, index.BodyCacheCap{})
	if err != nil {
		t.Fatalf("resolveIndexBackend should rebuild a stale cache, got error: %v", err)
	}
	defer func() { _ = idx.Close() }()
	if backend.Mode != BackendPersistent {
		t.Errorf("backend.Mode = %q, want %q (rebuilt persistent index)", backend.Mode, BackendPersistent)
	}
	// The rebuilt index must be usable.
	if err := idx.Put("/x/a.go", &index.Entry{ContentType: "source/go", Size: 1, ModTimeUnixNano: 1}); err != nil {
		t.Errorf("rebuilt index Put failed: %v", err)
	}
}

// TestResolveIndexBackend_LockContentionFallback simulates two
// concurrent processes contending for the same bbolt file: the
// second open hits bbolt.ErrTimeout and openIndex returns an
// in-memory fallback with the expected reason.
//
// Skipped under -short because it waits for the 5-second bbolt
// timeout to fire.
func TestResolveIndexBackend_LockContentionFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 5-second lock-contention test in -short mode")
	}
	path := filepath.Join(t.TempDir(), "contended.db")

	// First holder.
	first, _, err := resolveIndexBackend("", path, false, index.BodyCacheCap{})
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer func() { _ = first.Close() }()

	// Second attempt — should fall back to in-memory once bbolt's
	// internal 5-second flock timeout elapses.
	second, backend, err := resolveIndexBackend("", path, false, index.BodyCacheCap{})
	if err != nil {
		t.Fatalf("second open should not error (fallback expected), got: %v", err)
	}
	defer func() { _ = second.Close() }()
	if backend.Mode != BackendInMemory {
		t.Errorf("backend.Mode = %q, want %q (lock-contention fallback)", backend.Mode, BackendInMemory)
	}
	if backend.Reason != ReasonLockContention {
		t.Errorf("backend.Reason = %q, want %q", backend.Reason, ReasonLockContention)
	}
}
