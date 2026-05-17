package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// TestBuildAttributesWith_BodyCacheHitMissStale is the body-cache
// counterpart of TestBuildAttributesWith_CacheHitMissStale. Confirms
// that IncludeBody=true populates the body cache on miss and hits on
// the subsequent call; touching the file invalidates the body entry.
func TestBuildAttributesWith_BodyCacheHitMissStale(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	mustWrite(t, path, "# heading\nbody content here for cache validation\n")

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()
	opts := celexpr.BuildOptions{Index: idx, IncludeBody: true}

	// Cold call: BodyMisses=1, BodyPuts=1.
	a1, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith #1: %v", err)
	}
	if body, _ := a1.Extra["body"].(string); body == "" {
		t.Errorf("expected body populated on cold call, got empty; extra=%+v", a1.Extra)
	}
	st := idx.Stats()
	if st.BodyMisses != 1 || st.BodyHits != 0 || st.BodyPuts != 1 {
		t.Errorf("after cold: %+v want body_misses=1 body_hits=0 body_puts=1", st)
	}

	// Warm call: BodyHits=1.
	a2, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith #2: %v", err)
	}
	if body, _ := a2.Extra["body"].(string); body == "" {
		t.Errorf("expected body populated on warm call, got empty")
	}
	st = idx.Stats()
	if st.BodyHits != 1 {
		t.Errorf("after warm: body_hits=%d want 1; stats=%+v", st.BodyHits, st)
	}

	// Bump mtime: body cache stale, re-extract + re-store.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	a3, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith #3: %v", err)
	}
	if body, _ := a3.Extra["body"].(string); body == "" {
		t.Errorf("expected body populated after stale, got empty")
	}
	st = idx.Stats()
	if st.BodyStales != 1 {
		t.Errorf("after stale: body_stales=%d want 1; stats=%+v", st.BodyStales, st)
	}
	if st.BodyPuts != 2 {
		t.Errorf("after stale: body_puts=%d want 2 (one cold + one stale-refresh); stats=%+v", st.BodyPuts, st)
	}
}

// TestBuildAttributesWith_BodyCache_NilIndex confirms a nil index
// disables body caching entirely — body still extracts and surfaces,
// no cache operations happen.
func TestBuildAttributesWith_BodyCache_NilIndex(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	mustWrite(t, path, "hello body\n")

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	// Nil Index, IncludeBody=true: body still populates from re-extract.
	a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), celexpr.BuildOptions{IncludeBody: true})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	if body, _ := a.Extra["body"].(string); body == "" {
		t.Errorf("expected body populated with nil index, got empty")
	}
}

// TestBuildAttributesWith_BodyCache_DisabledIndex confirms an index
// opened with BodyCacheCap{Disable: true} behaves like nil: bodies
// extract every call, never cache.
func TestBuildAttributesWith_BodyCache_DisabledIndex(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	mustWrite(t, path, "hello body\n")

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	idxPath := filepath.Join(dir, "idx.db")
	idx, err := index.OpenWith(idxPath, index.BodyCacheCap{Disable: true})
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	defer func() { _ = idx.Close() }()
	opts := celexpr.BuildOptions{Index: idx, IncludeBody: true}

	for i := range 2 {
		a, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
		if err != nil {
			t.Fatalf("BuildAttributesWith #%d: %v", i, err)
		}
		if body, _ := a.Extra["body"].(string); body == "" {
			t.Errorf("call %d: expected body populated, got empty", i)
		}
	}
	st := idx.Stats()
	if st.BodyHits != 0 {
		t.Errorf("expected BodyHits=0 with disabled cache, got %d; stats=%+v", st.BodyHits, st)
	}
	if st.BodyPuts != 0 {
		t.Errorf("expected BodyPuts=0 with disabled cache, got %d; stats=%+v", st.BodyPuts, st)
	}
}
