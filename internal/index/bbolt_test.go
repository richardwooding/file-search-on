package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.etcd.io/bbolt"
)

func TestBoltOpenCreatesSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open: meta should already exist.
	idx2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if err := idx2.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
}

func TestBoltRoundTripPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	mtime := time.Unix(1700000000, 0)
	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	abs := filepath.Join(dir, "file.md")
	e := &Entry{
		Size:            42,
		ModTimeUnixNano: mtime.UnixNano(),
		ContentType:     "markdown",
		Extra: map[string]any{
			"title":      "Hi",
			"word_count": int64(7),
			"tags":       []string{"a", "b"},
		},
	}
	if err := idx.Put(abs, e); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Closing flushes pending writes.
	if err := idx.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen and confirm the entry survived.
	idx2, err := Open(path)
	if err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	defer func() { _ = idx2.Close() }()

	got, ok := idx2.Lookup(abs, 42, mtime)
	if !ok {
		t.Fatalf("expected hit after reopen, got miss; stats=%+v", idx2.Stats())
	}
	if got.ContentType != "markdown" {
		t.Errorf("ContentType=%q want markdown", got.ContentType)
	}
	if v, ok := got.Extra["word_count"].(int64); !ok || v != 7 {
		t.Errorf("word_count=%#v want int64(7)", got.Extra["word_count"])
	}
	if v, ok := got.Extra["tags"].([]string); !ok || len(v) != 2 {
		t.Errorf("tags=%#v want [a b]", got.Extra["tags"])
	}
}

func TestBoltSchemaMismatchRefusesOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	// Create a bbolt file with a meta bucket that claims an unknown
	// schema version. Open() should return ErrSchemaMismatch and NOT
	// modify the file (we don't auto-delete user data).
	db, err := bbolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("bbolt.Open: %v", err)
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		bk, err := tx.CreateBucket([]byte(bucketMeta))
		if err != nil {
			return err
		}
		// Bogus payload that isn't even valid JSON — exercises the
		// "unmarshal failed" branch.
		return bk.Put([]byte(metaKey), []byte("not json"))
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = db.Close()

	if _, err := Open(path); err == nil {
		t.Fatal("expected ErrSchemaMismatch, got nil")
	} else if err != ErrSchemaMismatch && !errIsSchemaMismatch(err) {
		t.Fatalf("expected ErrSchemaMismatch, got %v", err)
	}

	// File must still be on disk untouched.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should still exist: %v", err)
	}
}

func errIsSchemaMismatch(err error) bool {
	return err == ErrSchemaMismatch
}

// TestBoltAttrSchemaIDInvalidatesAcrossVersions pins the #418 fix: a cache
// written by one binary version is rejected when opened by another, so a
// newer binary never serves attributes the cache predates. (Before the
// fix, validity was (size, mtime) only and a stale entry survived forever.)
func TestBoltAttrSchemaIDInvalidatesAcrossVersions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	// Binary "v1" writes an entry.
	idx, err := openBoltIndex(path, BodyCacheCap{}, "v1")
	if err != nil {
		t.Fatalf("open v1: %v", err)
	}
	mtime := time.Unix(1700000000, 0)
	if err := idx.Put("/x/a.go", &Entry{ContentType: "source/go", Size: 10, ModTimeUnixNano: mtime.UnixNano(),
		Extra: map[string]any{"functions": []string{"A"}}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := idx.Close(); err != nil {
		t.Fatalf("close v1: %v", err)
	}

	// A different binary version must reject the cache rather than serve
	// its (now potentially incomplete) entries.
	if _, err := openBoltIndex(path, BodyCacheCap{}, "v2"); !errIsSchemaMismatch(err) {
		t.Fatalf("v2 opening a v1 cache: got %v, want ErrSchemaMismatch", err)
	}

	// Re-opening with the SAME id round-trips normally.
	idx2, err := openBoltIndex(path, BodyCacheCap{}, "v1")
	if err != nil {
		t.Fatalf("reopen v1: %v", err)
	}
	defer func() { _ = idx2.Close() }()
	if _, ok := idx2.Lookup("/x/a.go", 10, mtime); !ok {
		t.Error("same-id reopen should find the entry")
	}
}

func TestBoltPutThenLookupSameSession(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = idx.Close() }()

	mtime := time.Unix(1700000000, 0)
	abs := filepath.Join(dir, "file.md")
	if err := idx.Put(abs, &Entry{
		Size:            5,
		ModTimeUnixNano: mtime.UnixNano(),
		ContentType:     "markdown",
		Extra:           map[string]any{"word_count": int64(1)},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The writer goroutine is async; wait at most ~3× flush interval
	// for the entry to land. flushInterval is 100 ms.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, ok := idx.Lookup(abs, 5, mtime); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("entry never became readable; stats=%+v", idx.Stats())
}

func TestBoltRejectsRelativePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx.db")

	idx, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = idx.Close() }()

	if err := idx.Put("relative/path.md", &Entry{Size: 1, ModTimeUnixNano: 1}); err != nil {
		t.Fatalf("Put returned err (expected silent drop): %v", err)
	}
	st := idx.Stats()
	if st.Errors == 0 {
		t.Errorf("expected Errors>0 for relative path; got %+v", st)
	}
}

// TestBoltEntryOversizeCounter is the #348 regression: an entry whose
// encoded form exceeds the cap is dropped on a DEDICATED EntryOversize
// counter, not the generic Errors bucket — so a too-small cap (the #346
// class of bug) is diagnosable via index_stats instead of silent.
func TestBoltEntryOversizeCounter(t *testing.T) {
	dir := t.TempDir()
	idx, err := Open(filepath.Join(dir, "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = idx.Close() }()

	// An Extra value guaranteed to blow past maxEntryBytes.
	huge := make([]byte, 0, maxEntryBytes+1024)
	for len(huge) < maxEntryBytes+1024 {
		huge = append(huge, 'x')
	}
	e := &Entry{
		Size:            1,
		ModTimeUnixNano: 1,
		ContentType:     "text",
		Extra:           map[string]any{"title": string(huge)},
	}
	if err := idx.Put("/abs/oversize.txt", e); err != nil {
		t.Fatalf("Put: %v", err)
	}

	st := idx.Stats()
	if st.EntryOversize != 1 {
		t.Errorf("EntryOversize = %d, want 1 (oversize Put must hit the dedicated counter, #348)", st.EntryOversize)
	}
	if st.Errors != 0 {
		t.Errorf("Errors = %d, want 0 (an oversize drop is NOT a generic error)", st.Errors)
	}
}
