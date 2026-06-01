package search_test

import (
	"context"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalk_SkipsFIFOs is the regression test for the CI flake that
// caused the mcpserver package to hit the 3-minute test timeout. An
// unconnected FIFO blocks indefinitely on open(O_RDONLY) — if the
// walker doesn't filter it out at the DirEntry stage, the worker that
// reaches it via Registry.Detect hangs forever.
//
// We create a FIFO inside a tempdir alongside regular files, run
// search.Walk with a generous timeout, and assert the call returns
// quickly without hanging.
func TestWalk_SkipsFIFOs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs are POSIX-only")
	}

	root := t.TempDir()
	// Regular files the walker should still find.
	mustWriteFile(t, filepath.Join(root, "a.md"), "# hello\n")
	mustWriteFile(t, filepath.Join(root, "b.go"), "package main\n")

	// FIFO that would otherwise block opener until a writer opens
	// the other end.
	fifo := filepath.Join(root, "stuck.fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	// Bound the test with a hard deadline. Without the walker's
	// special-file filter, this test would hang for the full
	// per-test timeout (3 minutes in CI) — the assertion is "must
	// return within 5 seconds".
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	var results []search.Result
	var walkErr error
	go func() {
		defer close(done)
		results, walkErr = search.Walk(ctx, search.Options{
			Root:    root,
			Expr:    "true",
			Workers: 2,
		}, content.DefaultRegistry())
	}()

	select {
	case <-done:
		if walkErr != nil {
			t.Fatalf("walk error: %v", walkErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("walk hung past 10s — special-file filter likely not running")
	}

	// Sanity: the two regular files came through, the FIFO did not.
	var sawFIFO bool
	var sawRegular int
	for _, r := range results {
		base := filepath.Base(r.Path)
		switch base {
		case "stuck.fifo":
			sawFIFO = true
		case "a.md", "b.go":
			sawRegular++
		}
	}
	if sawFIFO {
		t.Errorf("FIFO unexpectedly surfaced as a walk result")
	}
	if sawRegular != 2 {
		t.Errorf("regular files surfaced = %d, want 2", sawRegular)
	}
}
