package search_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestOrchestrators_PreCancelledCtxReturnPromptly is the cancellation
// invariant for issue #337: every long-running orchestrator must observe
// a cancelled context and return PROMPTLY — never run to completion past
// cancellation (the class of bug #331 fixed by hand). Each case runs with
// an already-cancelled ctx and must (a) return within a generous deadline
// and (b) signal cancellation, either by returning a context error or by
// setting Cancelled=true on its partial-result struct.
//
// This is the mechanical guard behind the "check ctx.Err() at entry and
// every N iterations of any unbounded loop" convention (see the package
// doc in doc.go). A new orchestrator that forgets it will hang here.
func TestOrchestrators_PreCancelledCtxReturnPromptly(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.md", "b.md", "c.txt", "d.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("# "+n+"\n\nbody body body\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirB, "a.md"), []byte("# a\n\nother\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "bundle.zip")
	writeZIP(t, zipPath, map[string]string{"inner1.md": "# x\n\nbody\n", "inner2.txt": "plain\n"})

	reg := content.DefaultRegistry()
	opts := func() search.Options { return search.Options{Roots: []string{dir}, Expr: "true"} }

	cases := []struct {
		name string
		// run returns true when cancellation was signalled (ctx error OR
		// Cancelled flag). It is invoked with an already-cancelled ctx.
		run func(ctx context.Context) bool
	}{
		{"WalkStream", func(ctx context.Context) bool {
			out := make(chan search.Result, 64)
			done := make(chan error, 1)
			go func() { done <- search.WalkStream(ctx, opts(), reg, out) }()
			for range out { //nolint:revive // drain
			}
			return isCancel(<-done)
		}},
		{"Walk", func(ctx context.Context) bool {
			_, err := search.Walk(ctx, opts(), reg)
			return isCancel(err)
		}},
		{"ComputeStats", func(ctx context.Context) bool {
			s, err := search.ComputeStats(ctx, opts(), reg)
			return isCancel(err) || (s != nil && s.Cancelled)
		}},
		{"FindDuplicates", func(ctx context.Context) bool {
			d, err := search.FindDuplicates(ctx, opts(), reg)
			return isCancel(err) || (d != nil && d.Cancelled)
		}},
		{"FindNearDuplicates", func(ctx context.Context) bool {
			d, err := search.FindNearDuplicates(ctx, opts(), reg)
			return isCancel(err) || (d != nil && d.Cancelled)
		}},
		{"FindMatches", func(ctx context.Context) bool {
			o := opts()
			o.Pattern = "body"
			r, err := search.FindMatches(ctx, o, reg)
			return isCancel(err) || (r != nil && r.Cancelled)
		}},
		{"DiffTrees", func(ctx context.Context) bool {
			r, err := search.DiffTrees(ctx, dir, dirB, search.OpAMinusB, search.Options{Expr: "true"}, reg)
			return isCancel(err) || (r != nil && r.Cancelled)
		}},
		{"WalkArchiveEntries", func(ctx context.Context) bool {
			r, err := search.WalkArchiveEntries(ctx, zipPath, search.ArchiveWalkOptions{Expr: "true"}, reg)
			return isCancel(err) || (r != nil && r.Cancelled)
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // pre-cancel

			signalled := make(chan bool, 1)
			go func() { signalled <- tc.run(ctx) }()

			select {
			case got := <-signalled:
				if !got {
					t.Errorf("%s did not signal cancellation (no ctx error, Cancelled=false)", tc.name)
				}
			case <-time.After(15 * time.Second):
				t.Fatalf("%s did not return within 15s on a pre-cancelled ctx — it ignored cancellation (#337)", tc.name)
			}
		})
	}
}

func isCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
