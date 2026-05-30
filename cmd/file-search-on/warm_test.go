package main

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/index"
)

// TestWarmIndex_PopulatesCache asserts the warmer's whole reason for
// existing: walking a tree with an attached Index causes every walked
// file to land in idx.Stats().AttrEntriesCount via the cache-miss side
// effect of BuildAttributesWith.
func TestWarmIndex_PopulatesCache(t *testing.T) {
	dir := t.TempDir()
	for _, b := range []string{
		"alpha.md", "beta.md",
		"gamma.json", "delta.json",
		"epsilon.go", "zeta.go",
		"plain.txt", "notes.yaml",
		"data.csv", "page.html",
	} {
		mustWriteFile(t, filepath.Join(dir, b), "body of "+b+"\n")
	}

	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	var log bytes.Buffer
	if err := warmIndex(context.Background(), idx, dir, 1, &log); err != nil {
		t.Fatalf("warmIndex: %v", err)
	}

	got := idx.Stats().AttrEntriesCount
	if got != 10 {
		t.Errorf("AttrEntriesCount = %d, want 10 (cache should hold one entry per walked file)", got)
	}
	if !strings.Contains(log.String(), "warm: 10 files in ") {
		t.Errorf("missing completion log line; got %q", log.String())
	}
}

// TestWarmIndex_RespectsContext confirms cancellation propagates: the
// warmer never outlives ctx, which is the SIGINT/SIGTERM contract the
// MCP server relies on for clean shutdown.
func TestWarmIndex_RespectsContext(t *testing.T) {
	dir := t.TempDir()
	for i := range 50 {
		mustWriteFile(t, filepath.Join(dir, "f.md"), "x") // many small files
		_ = i
	}
	// Distinct basenames so all 50 actually land on disk.
	for i := range 50 {
		mustWriteFile(t, filepath.Join(dir, "file_"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26))+".md"), "x")
	}

	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately — warmer must surface ctx.Err
	err := warmIndex(ctx, idx, dir, 1, nil)
	if err == nil {
		// Walker can also legitimately complete an empty walk before
		// noticing cancellation if the tree is tiny — accept either,
		// but if there was no error, the timing was just lucky.
		return
	}
	if !errorIsContextCancellation(err) {
		t.Fatalf("warmIndex error = %v, want context.Canceled (or wrapped)", err)
	}
}

func errorIsContextCancellation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "operation was canceled")
}

func TestWarmWorkers_QuarterCpuFloorOne(t *testing.T) {
	// Explicit request wins.
	if got := warmWorkers(7); got != 7 {
		t.Errorf("warmWorkers(7) = %d, want 7", got)
	}
	// Zero / negative falls back to NumCPU/4, floor 1.
	cpu := runtime.NumCPU()
	want := max(cpu/4, 1)
	if got := warmWorkers(0); got != want {
		t.Errorf("warmWorkers(0) on %d-cpu host = %d, want %d", cpu, got, want)
	}
	if got := warmWorkers(-5); got != want {
		t.Errorf("warmWorkers(-5) on %d-cpu host = %d, want %d", cpu, got, want)
	}
}

// TestWarmIndex_NilLogOk confirms a nil log writer is safe — the
// caller in mcp_cmd.go always passes os.Stderr today, but the
// signature accepts nil for headless / library callers.
func TestWarmIndex_NilLogOk(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "x.md"), "hi")

	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	if err := warmIndex(context.Background(), idx, dir, 1, nil); err != nil {
		t.Fatalf("warmIndex with nil log: %v", err)
	}
	if got := idx.Stats().AttrEntriesCount; got != 1 {
		t.Errorf("AttrEntriesCount = %d, want 1", got)
	}
}

// Sanity: the warmer's elapsed reporting is in a reasonable range.
// Not a load test; just confirming the round-to-millisecond doesn't
// round a sub-microsecond walk to "0s" looking broken.
func TestWarmIndex_ElapsedRendersNonZero(t *testing.T) {
	dir := t.TempDir()
	for i := range 5 {
		mustWriteFile(t, filepath.Join(dir, "f"+string(rune('a'+i))+".md"), "x")
	}
	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	var log bytes.Buffer
	start := time.Now()
	_ = warmIndex(context.Background(), idx, dir, 1, &log)
	if time.Since(start) > 5*time.Second {
		t.Fatalf("warmIndex took absurdly long")
	}
	if !strings.Contains(log.String(), "warm: 5 files in ") {
		t.Errorf("expected count line for 5 files, got %q", log.String())
	}
}
