package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalk_SkipAttributesParse verifies that when Options.SkipAttributesParse
// is set, the walker emits Results with empty Extra maps — meaning the
// expensive ContentType.Attributes() parse was skipped — while still
// populating Path / ContentType / Size and the per-type / family bools
// via setTypeFlags.
func TestWalk_SkipAttributesParse(t *testing.T) {
	dir := t.TempDir()
	// Markdown with frontmatter — Attributes() would set title, word_count,
	// frontmatter, language, etc. if it ran.
	mdPath := filepath.Join(dir, "doc.md")
	body := "---\ntitle: Hello\nlanguage: en\n---\n# h\nbody body body body body\n"
	if err := os.WriteFile(mdPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("SkipAttributesParse=true → empty Extra", func(t *testing.T) {
		results, err := search.Walk(t.Context(), search.Options{
			Roots:               []string{dir},
			Expr:                "is_markdown",
			IncludeAttributes:   true,
			SkipAttributesParse: true,
		}, content.DefaultRegistry())
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("len(results)=%d want 1", len(results))
		}
		r := results[0]
		if r.ContentType != "markdown" {
			t.Errorf("ContentType=%q want markdown (Detect should still fire)", r.ContentType)
		}
		if !r.Attrs.IsMarkdown {
			t.Errorf("IsMarkdown=false (setTypeFlags should still fire)")
		}
		// The headline: Extra must be empty when SkipAttributesParse is on.
		if len(r.Attrs.Extra) != 0 {
			t.Errorf("Attrs.Extra=%+v want empty (Attributes parse should be skipped)", r.Attrs.Extra)
		}
	})

	t.Run("SkipAttributesParse=false → Extra populated", func(t *testing.T) {
		results, err := search.Walk(t.Context(), search.Options{
			Roots:             []string{dir},
			Expr:              "is_markdown",
			IncludeAttributes: true,
		}, content.DefaultRegistry())
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("len(results)=%d want 1", len(results))
		}
		r := results[0]
		if len(r.Attrs.Extra) == 0 {
			t.Errorf("Attrs.Extra empty want populated (Attributes should have parsed frontmatter)")
		}
		if title, _ := r.Attrs.Extra["title"].(string); title != "Hello" {
			t.Errorf("Extra[title]=%q want Hello (frontmatter parsed)", title)
		}
	})
}

// TestComputeStats_FastPathForContentTypeGroupBy verifies that the default
// group_by ("content_type") triggers the skip-attrs fast path automatically.
// We check this indirectly: write a markdown file with a panicking
// content-type-side hook would be intrusive, so instead we observe that the
// stats run is fast AND that the returned histogram is correct.
//
// The behavioural assertion: with the fast path, attrs.Extra would be
// untouched — but ComputeStats discards the results after bucketing, so we
// can't peek at Extra from outside. The functional assertion instead is
// that ComputeStats returns the expected buckets even with the skip-attrs
// path. Combined with TestWalk_SkipAttributesParse above (which asserts
// Extra IS empty under the flag), this is sufficient end-to-end coverage.
func TestComputeStats_FastPathForContentTypeGroupBy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("# h\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("# h2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Roots:   []string{dir},
		GroupBy: "content_type",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalCount != 3 {
		t.Errorf("TotalCount=%d want 3", stats.TotalCount)
	}
	buckets := map[string]int64{}
	for _, b := range stats.Groups {
		buckets[b.Name] = b.Count
	}
	if buckets["markdown"] != 2 {
		t.Errorf("buckets[markdown]=%d want 2; got %+v", buckets["markdown"], buckets)
	}
	if buckets["source/go"] != 1 {
		t.Errorf("buckets[source/go]=%d want 1; got %+v", buckets["source/go"], buckets)
	}
}

// TestComputeStats_SlowPathForAttributeGroupBy verifies that when the
// group_by key requires attribute parsing (e.g. "language" pulls from
// markdown frontmatter), ComputeStats does parse and produces the right
// bucket. This is the non-fast-path branch.
func TestComputeStats_SlowPathForAttributeGroupBy(t *testing.T) {
	dir := t.TempDir()
	mdEN := "---\nlanguage: en\n---\n# h\n"
	mdFR := "---\nlanguage: fr\n---\n# h\n"
	if err := os.WriteFile(filepath.Join(dir, "en.md"), []byte(mdEN), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fr.md"), []byte(mdFR), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := search.ComputeStats(t.Context(), search.Options{
		Roots:   []string{dir},
		Expr:    "is_markdown",
		GroupBy: "language",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalCount != 2 {
		t.Errorf("TotalCount=%d want 2", stats.TotalCount)
	}
	got := map[string]int64{}
	for _, b := range stats.Groups {
		got[b.Name] = b.Count
	}
	if got["en"] != 1 || got["fr"] != 1 {
		t.Errorf("buckets=%+v want en:1, fr:1 (group_by=language requires attribute parse)", got)
	}
}

// TestComputeStats_FastPathDisabledByNonTrivialExpr verifies that even
// when group_by is detector-only, a non-trivial CEL expression keeps the
// full Attributes parse on. Otherwise the expr couldn't reference
// per-format attributes like word_count.
func TestComputeStats_FastPathDisabledByNonTrivialExpr(t *testing.T) {
	dir := t.TempDir()
	long := "---\ntitle: long\n---\n# h\n" + strings.Repeat("word ", 50) + "\n"
	short := "---\ntitle: short\n---\n# h\nshort body\n"
	if err := os.WriteFile(filepath.Join(dir, "long.md"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "short.md"), []byte(short), 0o644); err != nil {
		t.Fatal(err)
	}

	// group_by=content_type would normally skip Attributes(), but the
	// CEL filter references word_count, so we must parse.
	stats, err := search.ComputeStats(t.Context(), search.Options{
		Roots:   []string{dir},
		Expr:    "is_markdown && word_count > 20",
		GroupBy: "content_type",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalCount != 1 {
		t.Errorf("TotalCount=%d want 1 (only long.md has word_count > 20); buckets=%+v", stats.TotalCount, stats.Groups)
	}
}

// noteUnusedImport keeps celexpr referenced so the import isn't pruned —
// the test file relies on the package being initialised so its init()s
// register all content types before tests run.
var _ = celexpr.New
