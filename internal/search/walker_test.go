package search_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

func TestWalkIncludeAttributes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Title\n\nSome body words.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		include bool
	}{
		{"include=false (default)", false},
		{"include=true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := search.Walk(context.Background(), search.Options{
				Root:              dir,
				Expr:              "is_markdown",
				Workers:           1,
				IncludeAttributes: tc.include,
			}, content.DefaultRegistry())
			if err != nil {
				t.Fatalf("Walk: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			r := results[0]
			if tc.include {
				if r.Attrs == nil {
					t.Fatalf("Attrs is nil but IncludeAttributes was true")
				}
				if r.Attrs.ContentType != "markdown" {
					t.Errorf("Attrs.ContentType = %q, want markdown", r.Attrs.ContentType)
				}
				// Spot-check that the markdown body title fallback fired (no front-matter, so H1).
				if got, _ := r.Attrs.Extra["title"].(string); got != "Title" {
					t.Errorf("Extra[title] = %q, want Title", got)
				}
			} else if r.Attrs != nil {
				t.Errorf("Attrs is non-nil when IncludeAttributes was false: %+v", r.Attrs)
			}
		})
	}
}
