package search_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestComputeStats_Histogram seeds three markdown files, two JSON
// files, and one PDF, then verifies the histogram + totals.
func TestComputeStats_Histogram(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# a\n")
	mustWriteFile(t, filepath.Join(dir, "b.md"), "# b\n")
	mustWriteFile(t, filepath.Join(dir, "c.md"), "# c\n")
	mustWriteFile(t, filepath.Join(dir, "x.json"), `{"k":1}`)
	mustWriteFile(t, filepath.Join(dir, "y.json"), `{"k":2}`)
	mustWriteFile(t, filepath.Join(dir, "z.html"), "<!doctype html><html><head><title>Z</title></head><body>z</body></html>")

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root: dir,
		Expr: "true",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalCount != 6 {
		t.Errorf("TotalCount=%d want 6", stats.TotalCount)
	}
	// Verify a couple of expected buckets are present with the
	// right counts. Ordering: count desc, name asc — markdown
	// (3) > json (2) > html (1).
	bucketByName := map[string]search.ContentTypeBucket{}
	for _, b := range stats.ContentTypes {
		bucketByName[b.Name] = b
	}
	if b := bucketByName["markdown"]; b.Count != 3 {
		t.Errorf("markdown count=%d want 3", b.Count)
	}
	if b := bucketByName["json"]; b.Count != 2 {
		t.Errorf("json count=%d want 2", b.Count)
	}
	if b := bucketByName["html"]; b.Count != 1 {
		t.Errorf("html count=%d want 1", b.Count)
	}
	// First row is markdown (highest count).
	if stats.ContentTypes[0].Name != "markdown" {
		t.Errorf("first row=%q want markdown", stats.ContentTypes[0].Name)
	}
}

// TestComputeStats_ScopedByExpr verifies that passing an Expr scopes
// the histogram — only files matching the CEL filter are counted.
func TestComputeStats_ScopedByExpr(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# a\n")
	mustWriteFile(t, filepath.Join(dir, "x.json"), `{"k":1}`)
	mustWriteFile(t, filepath.Join(dir, "y.json"), `{"k":2}`)

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root: dir,
		Expr: "is_json",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalCount != 2 {
		t.Errorf("TotalCount=%d want 2 (json-only)", stats.TotalCount)
	}
	if len(stats.ContentTypes) != 1 || stats.ContentTypes[0].Name != "json" {
		t.Errorf("ContentTypes=%v want just [json]", stats.ContentTypes)
	}
}

// TestComputeStats_EmptyDir returns zero counts without crashing.
func TestComputeStats_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	stats, err := search.ComputeStats(t.Context(), search.Options{Root: dir, Expr: "true"}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalCount != 0 || len(stats.ContentTypes) != 0 {
		t.Errorf("empty dir produced non-zero stats: %+v", stats)
	}
}
