package search_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalker_IndexHitsOnSecondRun proves the index actually short-circuits
// the walker's per-file extraction: two passes over the same directory
// with the same in-memory index should produce equal match sets, and the
// second pass should report all-hits, no-misses for files seen the first
// time.
func TestWalker_IndexHitsOnSecondRun(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	for _, n := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("# h\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()
	opts := search.Options{
		Root:  dir,
		Expr:  "is_markdown",
		Index: idx,
	}

	first, err := search.Walk(ctx, opts, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("first walk: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("first walk: got %d matches want 3", len(first))
	}
	st := idx.Stats()
	if st.Misses != 3 || st.Puts != 3 {
		t.Errorf("after first walk: %+v want misses=3 puts=3", st)
	}
	hitsAfterFirst := st.Hits

	second, err := search.Walk(ctx, opts, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("second walk: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("second walk: got %d matches want 3", len(second))
	}
	st = idx.Stats()
	// All three files should hit on the second walk.
	if st.Hits-hitsAfterFirst != 3 {
		t.Errorf("after second walk: hits gained=%d want 3 (full stats=%+v)",
			st.Hits-hitsAfterFirst, st)
	}
	// No new puts on second walk (everything is a hit).
	if st.Puts != 3 {
		t.Errorf("after second walk: puts=%d want 3 (full stats=%+v)", st.Puts, st)
	}
}
