package search_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalker_SortBySize_DescTopK seeds three markdown files of
// different sizes and asserts Walk returns the top-2 by size in
// descending order.
func TestWalker_SortBySize_DescTopK(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "small.md"), "# s\n")
	mustWriteFile(t, filepath.Join(dir, "medium.md"), "# m\n"+strings.Repeat("a", 1024))
	mustWriteFile(t, filepath.Join(dir, "huge.md"), "# h\n"+strings.Repeat("b", 8192))

	results, err := search.Walk(t.Context(), search.Options{
		Root:              dir,
		Expr:              "is_markdown",
		Sort:              "size",
		Order:             "desc",
		Limit:             2,
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (top-K)", len(results))
	}
	// huge > medium; small must be excluded.
	if !strings.HasSuffix(results[0].Path, "huge.md") {
		t.Errorf("first result: %s, want huge.md", results[0].Path)
	}
	if !strings.HasSuffix(results[1].Path, "medium.md") {
		t.Errorf("second result: %s, want medium.md", results[1].Path)
	}
}

// TestWalker_LimitWithoutSort returns the first N in walk order.
// The walk order isn't deterministic across runs but we know the
// total count must equal the limit.
func TestWalker_LimitWithoutSort(t *testing.T) {
	dir := t.TempDir()
	for i := range 10 {
		mustWriteFile(t, filepath.Join(dir, "doc-"+pad(i)+".md"), "# d\n")
	}
	results, err := search.Walk(t.Context(), search.Options{
		Root:  dir,
		Expr:  "is_markdown",
		Limit: 3,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

// TestWalker_ExcludeDirIsPruned asserts that a directory matched by
// the basename glob is not descended into. We seed a "skipme/" with
// a markdown file; without excludes it shows up, with excludes it
// does not (and importantly the walker never opens the file).
func TestWalker_ExcludeDirIsPruned(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "kept.md"), "# k\n")
	if err := os.MkdirAll(filepath.Join(dir, "skipme"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "skipme", "hidden.md"), "# h\n")

	results, err := search.Walk(t.Context(), search.Options{
		Root:     dir,
		Expr:     "is_markdown",
		Excludes: []string{"skipme"},
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (hidden.md should be pruned); paths: %v", len(results), paths(results))
	}
	if !strings.HasSuffix(results[0].Path, "kept.md") {
		t.Errorf("unexpected match: %v", results[0].Path)
	}
}

// TestWalker_ExcludeGlobMatchesFile asserts an *.bak glob matches
// individual files (not just directories) and they're skipped.
func TestWalker_ExcludeGlobMatchesFile(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "real.md"), "# r\n")
	mustWriteFile(t, filepath.Join(dir, "old.md.bak"), "# old\n")

	results, err := search.Walk(t.Context(), search.Options{
		Root:     dir,
		Expr:     "true",
		Excludes: []string{"*.bak"},
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, r := range results {
		if strings.HasSuffix(r.Path, ".bak") {
			t.Errorf("excluded *.bak match leaked through: %s", r.Path)
		}
	}
}

// TestWalker_RespectGitignore reads a .gitignore at the walk root
// and confirms the ignored path is skipped.
func TestWalker_RespectGitignore(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".gitignore"), "ignored/\n*.tmp\n")
	mustWriteFile(t, filepath.Join(dir, "kept.md"), "# k\n")
	if err := os.MkdirAll(filepath.Join(dir, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "ignored", "hidden.md"), "# h\n")
	mustWriteFile(t, filepath.Join(dir, "scratch.tmp"), "scratch\n")

	results, err := search.Walk(t.Context(), search.Options{
		Root:             dir,
		Expr:             "true",
		RespectGitignore: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, r := range results {
		base := filepath.Base(r.Path)
		if base == "hidden.md" || base == "scratch.tmp" {
			t.Errorf(".gitignore was not honoured; %s leaked through", r.Path)
		}
	}
}

// TestWalker_SnippetOnMarkdown asserts that with IncludeSnippet=true,
// markdown matches carry the first N lines on Result.Snippet.
func TestWalker_SnippetOnMarkdown(t *testing.T) {
	dir := t.TempDir()
	body := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	mustWriteFile(t, filepath.Join(dir, "doc.md"), body)

	results, err := search.Walk(t.Context(), search.Options{
		Root:              dir,
		Expr:              "is_markdown",
		IncludeAttributes: true,
		IncludeSnippet:    true,
		SnippetLines:      3,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	got := results[0].Snippet
	want := "line 1\nline 2\nline 3"
	if got != want {
		t.Errorf("snippet = %q\n want %q", got, want)
	}
}

// TestWalker_SnippetEmptyForBinary verifies that non-text content
// types leave Snippet empty even when IncludeSnippet is true.
func TestWalker_SnippetEmptyForBinary(t *testing.T) {
	dir := t.TempDir()
	// PDF magic header (won't actually parse correctly, but the
	// detector keys off the first four bytes; non-text content
	// type means snippet stays empty).
	mustWriteFile(t, filepath.Join(dir, "a.pdf"), "%PDF-1.4\n%fake\n")

	results, err := search.Walk(t.Context(), search.Options{
		Root:              dir,
		Expr:              "true",
		IncludeAttributes: true,
		IncludeSnippet:    true,
		SnippetLines:      5,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, r := range results {
		if r.Snippet != "" {
			t.Errorf("%s (%s) got snippet %q; want empty for non-text", r.Path, r.ContentType, r.Snippet)
		}
	}
}

// TestWalker_SortByModTime asserts time-typed attributes sort
// correctly. We touch files to control mtime ordering.
func TestWalker_SortByModTime(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	c := filepath.Join(dir, "c.md")
	mustWriteFile(t, a, "# a\n")
	mustWriteFile(t, b, "# b\n")
	mustWriteFile(t, c, "# c\n")
	// Spread the mtimes 1h apart so the test isn't flaky on
	// filesystems with second-resolution mtime.
	now := time.Now()
	if err := os.Chtimes(a, now.Add(-3*time.Hour), now.Add(-3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(b, now.Add(-2*time.Hour), now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(c, now.Add(-1*time.Hour), now.Add(-1*time.Hour)); err != nil {
		t.Fatal(err)
	}

	results, err := search.Walk(t.Context(), search.Options{
		Root:              dir,
		Expr:              "is_markdown",
		Sort:              "mod_time",
		Order:             "desc",
		IncludeAttributes: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d, want 3", len(results))
	}
	// Newest first: c, b, a
	wantOrder := []string{"c.md", "b.md", "a.md"}
	for i, r := range results {
		if filepath.Base(r.Path) != wantOrder[i] {
			t.Errorf("position %d: got %s, want %s", i, filepath.Base(r.Path), wantOrder[i])
		}
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func pad(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func paths(rs []search.Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Path
	}
	return out
}
