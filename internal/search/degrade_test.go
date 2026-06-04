package search_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalk_MalformedFilesNotDropped is the search-level regression for
// issue #321: a malformed PDF / docx sitting among good files must still
// appear in `search 'true'` results, not silently vanish.
func TestWalk_MalformedFilesNotDropped(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("good.md", "# Title\n\nbody\n")
	write("broken.pdf", "this is not a pdf\n") // detects pdf by ext; Attributes errors
	write("empty.pdf", "")
	write("notreally.docx", "not a zip\n")

	results, err := search.Walk(t.Context(), search.Options{
		Roots: []string{dir},
		Expr:  "true",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := map[string]bool{}
	for _, r := range results {
		got[filepath.Base(r.Path)] = true
	}
	for _, name := range []string{"good.md", "broken.pdf", "empty.pdf", "notreally.docx"} {
		if !got[name] {
			t.Errorf("%s missing from results — malformed files must not be dropped (#321); got %v", name, got)
		}
	}
}
