package celexpr_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// TestBuildAttributesWith_CacheHitMissStale exercises the integrated
// path: real disk file + content registry + index. First call is a
// miss-then-store; second call hits; touching the file invalidates.
func TestBuildAttributesWith_CacheHitMissStale(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	mustWrite(t, path, "---\ntitle: Hi\n---\n# h\nbody body body\n")

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(abs)
	parent := filepath.Dir(abs)

	idx := index.NewMemory()
	defer func() { _ = idx.Close() }()
	opts := celexpr.BuildOptions{Index: idx}

	// Cold call: miss, then store.
	a1, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith #1: %v", err)
	}
	if a1.ContentType != "markdown" {
		t.Errorf("ContentType=%q want markdown", a1.ContentType)
	}
	st := idx.Stats()
	if st.Misses != 1 || st.Hits != 0 || st.Puts != 1 {
		t.Errorf("after cold: %+v want misses=1 hits=0 puts=1", st)
	}

	// Warm call: hit. ContentType + Extra come from cache.
	a2, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith #2: %v", err)
	}
	if a2.ContentType != "markdown" {
		t.Errorf("warm ContentType=%q want markdown", a2.ContentType)
	}
	if v, ok := a2.Extra["title"].(string); !ok || v != "Hi" {
		t.Errorf("warm title=%#v want \"Hi\"", a2.Extra["title"])
	}
	st = idx.Stats()
	if st.Hits != 1 {
		t.Errorf("after warm: hits=%d want 1; stats=%+v", st.Hits, st)
	}

	// Bump the mtime: stale → re-extract → re-store.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	a3, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, abs, content.DefaultRegistry(), opts)
	if err != nil {
		t.Fatalf("BuildAttributesWith #3: %v", err)
	}
	if a3.ContentType != "markdown" {
		t.Errorf("stale-refresh ContentType=%q want markdown", a3.ContentType)
	}
	st = idx.Stats()
	if st.Stales != 1 {
		t.Errorf("after stale: stales=%d want 1; stats=%+v", st.Stales, st)
	}
	if st.Puts != 2 {
		t.Errorf("after stale: puts=%d want 2; stats=%+v", st.Puts, st)
	}
}

// TestBuildAttributesWith_NilIndexNoCaching makes sure a nil index
// does not crash and produces the same result as BuildAttributes.
func TestBuildAttributesWith_NilIndexNoCaching(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	mustWrite(t, path, "# h\nbody\n")

	parent := filepath.Dir(path)
	base := filepath.Base(path)

	a1, err := celexpr.BuildAttributesWith(ctx, os.DirFS(parent), base, path, content.DefaultRegistry(), celexpr.BuildOptions{})
	if err != nil {
		t.Fatalf("BuildAttributesWith: %v", err)
	}
	a2, err := celexpr.BuildAttributes(ctx, os.DirFS(parent), base, path, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("BuildAttributes: %v", err)
	}
	if a1.ContentType != a2.ContentType {
		t.Errorf("ContentType drift: with-nil-opts=%q wrapper=%q", a1.ContentType, a2.ContentType)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
