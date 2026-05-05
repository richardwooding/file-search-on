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

func TestWalkStream_BasicMatchesWalk(t *testing.T) {
	dir := t.TempDir()
	for i := range 5 {
		path := filepath.Join(dir, fmt.Sprintf("doc-%02d.md", i))
		if err := os.WriteFile(path, []byte("# h\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	out := make(chan search.Result, 8)
	var streamed []search.Result
	done := make(chan struct{})
	go func() {
		for r := range out {
			streamed = append(streamed, r)
		}
		close(done)
	}()
	err := search.WalkStream(context.Background(), search.Options{
		Root:    dir,
		Expr:    "is_markdown",
		Workers: 2,
	}, content.DefaultRegistry(), out)
	if err != nil {
		t.Fatalf("WalkStream: %v", err)
	}
	<-done

	if len(streamed) != 5 {
		t.Errorf("streamed %d results; want 5", len(streamed))
	}
	// Every streamed result is a markdown match. Order is unspecified.
	for _, r := range streamed {
		if r.ContentType != "markdown" {
			t.Errorf("ContentType = %q; want markdown", r.ContentType)
		}
	}
}

func TestWalkStream_RespectsCancellation(t *testing.T) {
	dir := t.TempDir()
	for i := range 200 {
		path := filepath.Join(dir, fmt.Sprintf("doc-%03d.md", i))
		if err := os.WriteFile(path, []byte("# h\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out := make(chan search.Result, 8)
	// Drain the channel concurrently so out <- r doesn't deadlock when
	// the consumer is slow under cancellation.
	go func() {
		for range out {
		}
	}()

	start := time.Now()
	err := search.WalkStream(ctx, search.Options{
		Root:    dir,
		Expr:    "is_markdown",
		Workers: 1,
	}, content.DefaultRegistry(), out)
	elapsed := time.Since(start)

	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("WalkStream returned unexpected error: %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("WalkStream did not return promptly under cancellation: took %v", elapsed)
	}
}

func TestWalkStream_ClosesChannel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# h\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := make(chan search.Result, 4)
	go func() {
		_ = search.WalkStream(context.Background(), search.Options{
			Root: dir, Expr: "is_markdown", Workers: 1,
		}, content.DefaultRegistry(), out)
	}()

	// Range exits when the channel is closed; if WalkStream forgets to close
	// it, this loop deadlocks and the test times out.
	count := 0
	for range out {
		count++
	}
	if count == 0 {
		t.Errorf("expected at least 1 result before channel close; got 0")
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
