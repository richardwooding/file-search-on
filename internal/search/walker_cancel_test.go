package search_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalkStream_CancelledCtxSurfacesError is a regression test for
// the silent-cancellation bug fixed alongside this test. When the
// producer (fs.WalkDir) completes cleanly faster than the workers
// drain the job channel, the workers exit on ctx.Done() WITHOUT
// returning an error from their goroutine, and fs.WalkDir's callback
// also returns nil — so without the post-wg.Wait() ctx.Err() check
// in WalkStream, the function would return nil and callers would
// think the walk completed normally.
//
// We feed an already-cancelled ctx so the race is forced: workers
// see ctx.Done() immediately. The contract being pinned: WalkStream
// returns a non-nil error (specifically a context cancellation
// error) whenever ctx was cancelled, regardless of which goroutine
// noticed first.
func TestWalkStream_CancelledCtxSurfacesError(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# a\n")

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // already-done before walk starts

	out := make(chan search.Result, 16)
	err := search.WalkStream(ctx, search.Options{
		Root: dir,
		Expr: "is_markdown",
	}, content.DefaultRegistry(), out)

	// Drain the channel so we don't leave a goroutine blocked.
	for range out {
	}

	if err == nil {
		t.Fatal("WalkStream returned nil for already-cancelled ctx; want a cancellation error so callers can distinguish 'walk done' from 'walk cancelled'")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("WalkStream err=%v; want context.Canceled", err)
	}
}

// TestWalk_CancelledCtxReturnsPartialAndError mirrors the above for
// the buffered Walk wrapper — partial results land in the slice,
// the error reports cancellation.
func TestWalk_CancelledCtxReturnsPartialAndError(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# a\n")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := search.Walk(ctx, search.Options{
		Root: dir,
		Expr: "is_markdown",
	}, content.DefaultRegistry())

	if err == nil {
		t.Fatal("Walk returned nil err for already-cancelled ctx; want a cancellation error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Walk err=%v; want context.Canceled", err)
	}
}
