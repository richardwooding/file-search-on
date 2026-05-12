package search_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalker_BodyContainsCELFilter is the headline test for body
// filtering: a CEL expression using body.contains() actually
// excludes files whose body doesn't match. This wires the whole
// pipeline — options → BuildAttributesWith → readBody → CEL
// activation → Evaluate.
func TestWalker_BodyContainsCELFilter(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "hit.md"), "# h\nbody mentioning transformer attention\n")
	mustWriteFile(t, filepath.Join(dir, "miss.md"), "# m\nbody mentioning cabbage\n")

	results, err := search.Walk(t.Context(), search.Options{
		Root:        dir,
		Expr:        `is_markdown && body.contains("transformer")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1; paths: %v", len(results), paths(results))
	}
	if !strings.HasSuffix(results[0].Path, "hit.md") {
		t.Errorf("got %s, want hit.md", results[0].Path)
	}
}

// TestWalker_BodyMatchesRegex uses CEL's built-in `matches` method
// (RE2 regex) — no custom function needed. This is the
// discoverability claim the docs make; the test pins it.
func TestWalker_BodyMatchesRegex(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "hit.md"), "// TODO: fix this\n")
	mustWriteFile(t, filepath.Join(dir, "miss.md"), "// nothing to do\n")

	results, err := search.Walk(t.Context(), search.Options{
		Root:        dir,
		Expr:        `is_markdown && body.matches("(?i)\\bTODO\\b")`,
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1; paths: %v", len(results), paths(results))
	}
	if !strings.HasSuffix(results[0].Path, "hit.md") {
		t.Errorf("got %s, want hit.md", results[0].Path)
	}
}

// TestWalker_BodyEmptyForBinary verifies binary content types
// leave body empty — `body.contains(...)` against e.g. a PDF
// is always false.
func TestWalker_BodyEmptyForBinary(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.pdf"), "%PDF-1.4\n%fake\n")

	results, err := search.Walk(t.Context(), search.Options{
		Root:        dir,
		Expr:        `body.contains("PDF")`, // body is empty, so this is never true
		IncludeBody: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (binary types have empty body); paths: %v", len(results), paths(results))
	}
}

// TestWalker_BodyMaxBytesTruncates verifies the cap is honoured —
// content past the cap is invisible to CEL.
func TestWalker_BodyMaxBytesTruncates(t *testing.T) {
	dir := t.TempDir()
	// "AAAAA...needle" — the needle is past the first 8 bytes.
	body := strings.Repeat("A", 64) + "needle\n"
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# h\n"+body)

	// Cap at 8 bytes — well before "needle" appears.
	results, err := search.Walk(t.Context(), search.Options{
		Root:         dir,
		Expr:         `body.contains("needle")`,
		IncludeBody:  true,
		BodyMaxBytes: 8,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (body cap should hide the needle); paths: %v", len(results), paths(results))
	}

	// With a generous cap the needle is visible.
	results, err = search.Walk(t.Context(), search.Options{
		Root:         dir,
		Expr:         `body.contains("needle")`,
		IncludeBody:  true,
		BodyMaxBytes: 0, // default 1 MiB — easily contains the needle
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk default cap: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("got %d results with default cap, want 1", len(results))
	}
}

