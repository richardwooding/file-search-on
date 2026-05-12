package search_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestComputeStats_GroupByExt buckets by file extension.
func TestComputeStats_GroupByExt(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# a\n")
	mustWriteFile(t, filepath.Join(dir, "b.md"), "# b\n")
	mustWriteFile(t, filepath.Join(dir, "x.json"), `{}`)

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root:    dir,
		Expr:    "true",
		GroupBy: "ext",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.GroupBy != "ext" {
		t.Errorf("GroupBy=%q want ext", stats.GroupBy)
	}
	byName := map[string]int64{}
	for _, b := range stats.Groups {
		byName[b.Name] = b.Count
	}
	if byName[".md"] != 2 {
		t.Errorf(".md count=%d want 2", byName[".md"])
	}
	if byName[".json"] != 1 {
		t.Errorf(".json count=%d want 1", byName[".json"])
	}
	if len(stats.ContentTypes) != 0 {
		t.Errorf("ContentTypes should be empty when GroupBy!=content_type; got %v", stats.ContentTypes)
	}
}

// TestComputeStats_GroupByLanguage buckets by source language.
func TestComputeStats_GroupByLanguage(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package main\n")
	mustWriteFile(t, filepath.Join(dir, "b.go"), "package main\n")
	mustWriteFile(t, filepath.Join(dir, "c.py"), "print(1)\n")

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root:    dir,
		Expr:    "is_source",
		GroupBy: "language",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	byName := map[string]int64{}
	for _, b := range stats.Groups {
		byName[b.Name] = b.Count
	}
	if byName["go"] != 2 {
		t.Errorf("go count=%d want 2", byName["go"])
	}
	if byName["python"] != 1 {
		t.Errorf("python count=%d want 1", byName["python"])
	}
}

// TestComputeStats_GroupByUnknownFallback verifies that an unknown
// group_by value falls back to content_type (rather than erroring
// or crashing), matching the documented degrade-don't-error pattern.
func TestComputeStats_GroupByUnknownFallback(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# a\n")
	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root:    dir,
		Expr:    "true",
		GroupBy: "made_up_key",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.GroupBy != "content_type" {
		t.Errorf("GroupBy=%q want content_type (fallback)", stats.GroupBy)
	}
}

// TestComputeStats_DefaultGroupByPopulatesBoth: when GroupBy is
// "content_type" (or unset), both Groups and ContentTypes are
// populated for back-compat.
func TestComputeStats_DefaultGroupByPopulatesBoth(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# a\n")
	stats, err := search.ComputeStats(t.Context(), search.Options{
		Root: dir,
		Expr: "true",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if len(stats.Groups) == 0 {
		t.Fatal("Groups empty")
	}
	if len(stats.ContentTypes) == 0 {
		t.Fatal("ContentTypes empty; should be populated alongside Groups for default group_by")
	}
	if stats.Groups[0].Name != stats.ContentTypes[0].Name {
		t.Errorf("Groups[0]=%q ContentTypes[0]=%q should match for default group_by", stats.Groups[0].Name, stats.ContentTypes[0].Name)
	}
}
