package search_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestComputeStats_TimeoutSurfacesCancelled is the regression test
// for the wrapped-error bug found via CLI smoke against ~/Library
// with --timeout 100ms. Symptom: stats.Cancelled stayed false even
// though the walk stopped early, so the CLI lost its "timed out"
// message, suggestions, and exit-code-124 path. Root cause:
// walker.go:600 wraps the producer's ctx error via errors.Join, so
// stats.go's direct equality `switch walkErr { case context.X }` no
// longer matched. Fix: switch to `errors.Is(walkErr, context.X)`.
//
// Test strategy: pre-cancel the ctx, then call ComputeStats. The
// producer's first WalkDir callback sees ctx.Done() immediately,
// returns ctx.Err() (Canceled), which errors.Join wraps. Pre-fix
// this returned (stats, walkErr) with Cancelled=false. Post-fix it
// sets Cancelled=true + reason=client_cancel.
func TestComputeStats_TimeoutSurfacesCancelled(t *testing.T) {
	dir := t.TempDir()
	// Populate the dir with enough files that the walker has work
	// to do — though we cancel pre-emptively below.
	for i := range 50 {
		mustWriteFile(t, filepath.Join(dir, fmt.Sprintf("f%d.md", i)), "# x\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // fire before ComputeStats sees the ctx

	stats, err := search.ComputeStats(ctx, search.Options{
		Root: dir,
		Expr: "true",
	}, content.DefaultRegistry())

	if err != nil {
		t.Fatalf("ComputeStats returned err on cancelled ctx (should swallow): %v", err)
	}
	if stats == nil {
		t.Fatal("stats is nil on cancellation; expected partial-result struct")
		return // unreachable; quiets staticcheck SA5011
	}
	if !stats.Cancelled {
		t.Errorf("stats.Cancelled = false; expected true (bug: errors.Join wraps the ctx error, direct equality fails)")
	}
	if stats.CancellationReason != "client_cancel" {
		t.Errorf("stats.CancellationReason = %q, want %q", stats.CancellationReason, "client_cancel")
	}
}

// TestComputeStats_DeadlineExceededSurfacesTimeout mirrors the above
// for the timeout path. Sets a 1ms deadline; the producer trips it
// almost immediately. Pre-fix bug: reason stayed empty.
func TestComputeStats_DeadlineExceededSurfacesTimeout(t *testing.T) {
	dir := t.TempDir()
	for i := range 200 {
		mustWriteFile(t, filepath.Join(dir, fmt.Sprintf("f%d.md", i)), "# x\n")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	// Tiny sleep to ensure the deadline has actually fired before
	// ComputeStats kicks off — defends against fast machines where
	// 1ms might not have ticked by the time we enter ComputeStats.
	time.Sleep(2 * time.Millisecond)

	stats, err := search.ComputeStats(ctx, search.Options{
		Root: dir,
		Expr: "true",
	}, content.DefaultRegistry())

	if err != nil {
		t.Fatalf("ComputeStats returned err on timeout (should swallow): %v", err)
	}
	if stats == nil {
		t.Fatal("stats is nil on timeout; expected partial-result struct")
	}
	if !stats.Cancelled {
		t.Errorf("stats.Cancelled = false; expected true on deadline-exceeded")
	}
	if stats.CancellationReason != "timeout" {
		t.Errorf("stats.CancellationReason = %q, want %q", stats.CancellationReason, "timeout")
	}
}
