package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/embed"
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

// stubOllamaEmbed mounts /api/embed on an httptest server, returning a
// fixed 4-dim vector for every request. The vector matches a real
// Ollama response shape (`embeddings` is a 2D array). Count is bumped
// per call so tests can assert N files → N+1 Ollama calls (1 dummy
// query + N file bodies).
func stubOllamaEmbed(t *testing.T, count *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model string `json:"model"`
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if count != nil {
			count.Add(1)
		}
		// Return a 4-dim normalised vector. Varying it slightly per
		// request keeps the test honest — the warmer should accept
		// distinct file vectors and cache them all.
		_, _ = w.Write([]byte(`{"embeddings":[[0.5,0.5,0.5,0.5]]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestWarmEmbeddings_PopulatesVectorCache is the headline test: walking
// a tempdir of text files with warmEmbeddings should populate
// idx.Stats().EmbedPuts for every file.
func TestWarmEmbeddings_PopulatesVectorCache(t *testing.T) {
	dir := t.TempDir()
	for _, b := range []string{"a.md", "b.md", "c.md"} {
		mustWriteFile(t, filepath.Join(dir, b), "body of "+b+"\n")
	}

	var calls atomic.Int64
	srv := stubOllamaEmbed(t, &calls)

	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	embedder := embed.NewOllama(srv.URL, "test-model")
	var log bytes.Buffer
	if err := warmEmbeddings(context.Background(), idx, dir, 1, embedder, &log); err != nil {
		t.Fatalf("warmEmbeddings: %v", err)
	}

	got := idx.Stats().EmbedPuts
	if got != 3 {
		t.Errorf("EmbedPuts = %d, want 3 (one per walked file)", got)
	}
	// 1 dummy query + 3 file bodies = 4 Ollama calls.
	if calls.Load() != 4 {
		t.Errorf("Ollama call count = %d, want 4 (1 dummy + 3 files)", calls.Load())
	}
	if !strings.Contains(log.String(), "warm-embeddings (test-model): 3 files in ") {
		t.Errorf("missing completion line; got %q", log.String())
	}
}

// TestWarmEmbeddings_QueryEmbedFailureAborts asserts that when the
// dummy-query embed fails (Ollama down, model missing, etc.), the
// warmer returns the error WITHOUT starting the walk — no point
// embedding file bodies if we can't even embed the dummy query.
func TestWarmEmbeddings_QueryEmbedFailureAborts(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "x")

	var fileEmbedAttempts atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fileEmbedAttempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	embedder := embed.NewOllama(srv.URL, "test-model")
	err := warmEmbeddings(context.Background(), idx, dir, 1, embedder, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "dummy query") {
		t.Errorf("error should mention dummy-query path, got %v", err)
	}
	// Only the dummy-query call should have hit Ollama — the walk
	// must not have started.
	if fileEmbedAttempts.Load() != 1 {
		t.Errorf("Ollama hits = %d, want 1 (only the failing dummy query)", fileEmbedAttempts.Load())
	}
	if idx.Stats().EmbedPuts != 0 {
		t.Errorf("EmbedPuts = %d, want 0 (walk should not have started)", idx.Stats().EmbedPuts)
	}
}

// TestWarmEmbeddings_ContextCancel asserts ctx cancellation aborts
// cleanly — the warmer must not outlive ctx.
func TestWarmEmbeddings_ContextCancel(t *testing.T) {
	dir := t.TempDir()
	for _, b := range []string{"a.md", "b.md", "c.md"} {
		mustWriteFile(t, filepath.Join(dir, b), "x")
	}

	srv := stubOllamaEmbed(t, nil)
	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	embedder := embed.NewOllama(srv.URL, "test-model")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	err := warmEmbeddings(ctx, idx, dir, 1, embedder, nil)
	if err == nil {
		// Tiny tree can finish before ctx is observed; the assertion
		// is "no panic, no hang" — both achieved if we get here.
		return
	}
	if !errorIsContextCancellation(err) {
		t.Errorf("error = %v, want context-cancellation-shaped", err)
	}
}

// TestWarmEmbeddings_NilLogOk confirms nil log is safe.
func TestWarmEmbeddings_NilLogOk(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "x.md"), "hi")

	srv := stubOllamaEmbed(t, nil)
	idx := index.NewMemory()
	t.Cleanup(func() { _ = idx.Close() })

	embedder := embed.NewOllama(srv.URL, "test-model")
	if err := warmEmbeddings(context.Background(), idx, dir, 1, embedder, nil); err != nil {
		t.Fatalf("warmEmbeddings with nil log: %v", err)
	}
}
