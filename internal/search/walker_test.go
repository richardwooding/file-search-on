package search_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

func TestWalkRespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	// Plenty of files so the walk has real work to do — enough that cancelling
	// before completion is meaningful.
	for i := range 200 {
		path := filepath.Join(dir, fmt.Sprintf("doc-%03d.md", i))
		if err := os.WriteFile(path, []byte("# h\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel; the walker should bail out essentially immediately

	start := time.Now()
	results, err := search.Walk(ctx, search.Options{
		Root:    dir,
		Expr:    "is_markdown",
		Workers: 1,
	}, content.DefaultRegistry())
	elapsed := time.Since(start)

	// Walk returns the WalkDir error from the producer when the callback
	// returns ctx.Err(). It may instead return nil if cancellation arrived
	// after the walk completed; either is acceptable. What matters is that
	// the function returns promptly, doesn't process all 200 files, and
	// either reports a partial result or none at all.
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Walk returned unexpected error: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("Walk did not return promptly under cancellation: took %v", elapsed)
	}
	if len(results) >= 200 {
		t.Errorf("Walk processed all %d files despite cancellation", len(results))
	}
}

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
