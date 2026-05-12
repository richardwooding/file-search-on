package search_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestComputeStats_MTimeMonth groups files by year-month of mtime.
// We seed three files and use os.Chtimes to control mtime so the
// test isn't dependent on actual wall-clock time.
func TestComputeStats_MTimeMonth(t *testing.T) {
	dir := t.TempDir()
	files := []struct {
		name string
		mt   time.Time
	}{
		{"a.md", time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)},
		{"b.md", time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC)},
		{"c.md", time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC)},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, []byte("# h\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, f.mt, f.mt); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root:    dir,
		Expr:    "true",
		GroupBy: "mtime_month",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.GroupBy != "mtime_month" {
		t.Errorf("GroupBy=%q want mtime_month", stats.GroupBy)
	}
	byName := map[string]int64{}
	for _, b := range stats.Groups {
		byName[b.Name] = b.Count
	}
	if byName["2024-03"] != 2 {
		t.Errorf("2024-03 count=%d want 2", byName["2024-03"])
	}
	if byName["2025-01"] != 1 {
		t.Errorf("2025-01 count=%d want 1", byName["2025-01"])
	}
}

// TestComputeStats_MTimeYear is the coarser variant — single
// year bucket.
func TestComputeStats_MTimeYear(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	if err := os.WriteFile(a, []byte("# a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("# b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(a, t1, t1); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, t2, t2); err != nil {
		t.Fatal(err)
	}

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root:    dir,
		Expr:    "true",
		GroupBy: "mtime_year",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	byName := map[string]int64{}
	for _, b := range stats.Groups {
		byName[b.Name] = b.Count
	}
	if byName["2024"] != 1 {
		t.Errorf("2024 count=%d want 1", byName["2024"])
	}
	if byName["2025"] != 1 {
		t.Errorf("2025 count=%d want 1", byName["2025"])
	}
}

// TestComputeStats_DateMonthFromFrontmatter exercises the
// non-mtime path: bucket by markdown front-matter `date` field.
func TestComputeStats_DateMonthFromFrontmatter(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "---\ndate: 2024-03-15\n---\n# a\n")
	mustWriteFile(t, filepath.Join(dir, "b.md"), "---\ndate: 2024-03-20\n---\n# b\n")
	mustWriteFile(t, filepath.Join(dir, "c.md"), "---\ndate: 2024-07-01\n---\n# c\n")

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root:    dir,
		Expr:    "is_markdown",
		GroupBy: "date_month",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	byName := map[string]int64{}
	for _, b := range stats.Groups {
		byName[b.Name] = b.Count
	}
	if byName["2024-03"] != 2 {
		t.Errorf("2024-03 count=%d want 2", byName["2024-03"])
	}
	if byName["2024-07"] != 1 {
		t.Errorf("2024-07 count=%d want 1", byName["2024-07"])
	}
}
