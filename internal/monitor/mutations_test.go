package monitor

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/index"
)

// withTestServer constructs a Server with the given Config overrides on
// top of the testserver defaults. Index is always a fresh in-memory.
func withTestServer(cfg Config) *Server {
	if cfg.Version == "" {
		cfg.Version = "test-1.2.3"
	}
	if cfg.Mode == "" {
		cfg.Mode = "mcp-stdio"
	}
	if cfg.Index == nil {
		cfg.Index = index.NewMemory()
	}
	return NewServer(cfg)
}

// postForm issues a POST to handler with the given form body and
// returns the recorder. handler is the bound method (e.g. s.handleCacheEvict).
func postForm(t *testing.T, handler http.HandlerFunc, path, form string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form))
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

// --- evict ---

func TestCacheEvict_RemovesEntry(t *testing.T) {
	s := withTestServer(Config{})
	// Seed an entry.
	abs := absPath(t, "victim.md")
	if err := s.cfg.Index.Put(abs, &index.Entry{Size: 1, ModTimeUnixNano: time.Now().UnixNano(), ContentType: "markdown"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := s.cfg.Index.PeekAttrs(abs); !ok {
		t.Fatal("setup: PeekAttrs should have seen entry pre-evict")
	}
	rec := postForm(t, s.handleCacheEvict, "/api/cache/evict", "path="+abs)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := s.cfg.Index.PeekAttrs(abs); ok {
		t.Errorf("PeekAttrs still has entry after evict")
	}
}

func TestCacheEvict_RejectsIllFormedPaths(t *testing.T) {
	s := withTestServer(Config{})
	cases := map[string]int{
		"":                                http.StatusBadRequest, // missing path
		"path=":                           http.StatusBadRequest, // empty path
		"path=relative/file.md":           http.StatusBadRequest, // not absolute
		"path=/etc/../etc/passwd":         http.StatusBadRequest, // not clean
		"path=/Users/me/Code/proj/a.md": http.StatusNoContent,  // valid (entry doesn't exist; Delete is idempotent)
	}
	for body, want := range cases {
		rec := postForm(t, s.handleCacheEvict, "/api/cache/evict", body)
		if rec.Code != want {
			t.Errorf("body=%q status = %d, want %d (body=%s)", body, rec.Code, want, rec.Body.String())
		}
	}
}

// --- clear ---

func TestCacheClear_WipesEntries(t *testing.T) {
	s := withTestServer(Config{})
	abs := absPath(t, "a.md")
	if err := s.cfg.Index.Put(abs, &index.Entry{Size: 1, ModTimeUnixNano: time.Now().UnixNano(), ContentType: "markdown"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rec := postForm(t, s.handleCacheClear, "/api/cache/clear", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := s.cfg.Index.Stats().AttrEntriesCount; got != 0 {
		t.Errorf("AttrEntriesCount after Clear = %d, want 0", got)
	}
}

// --- warm-attrs ---

func TestWarmAttrs_FiresGoroutine(t *testing.T) {
	var calls atomic.Int64
	done := make(chan struct{})
	fn := func(_ context.Context, root string) error {
		calls.Add(1)
		if !filepath.IsAbs(root) {
			t.Errorf("warm fn received non-absolute root %q", root)
		}
		close(done)
		return nil
	}
	cwd := absPath(t, "")
	s := withTestServer(Config{Cwd: cwd, WarmAttrsFn: fn})
	rec := postForm(t, s.handleWarmAttrs, "/api/cache/warm-attrs", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("warm goroutine never fired")
	}
	if calls.Load() != 1 {
		t.Errorf("warm fn invocations = %d, want 1", calls.Load())
	}
	// Wait for state to settle.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state := s.warming.Load(); state != nil && state.LastDuration != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := s.warming.Load()
	if state == nil || state.LastKind != "attrs" {
		t.Errorf("warming state = %+v, want LastKind=attrs", state)
	}
}

func TestWarmAttrs_412WhenUnwired(t *testing.T) {
	s := withTestServer(Config{}) // no WarmAttrsFn
	rec := postForm(t, s.handleWarmAttrs, "/api/cache/warm-attrs", "")
	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", rec.Code)
	}
}

func TestWarmAttrs_RejectsRelativeDir(t *testing.T) {
	s := withTestServer(Config{
		Cwd:         "/tmp",
		WarmAttrsFn: func(context.Context, string) error { return nil },
	})
	rec := postForm(t, s.handleWarmAttrs, "/api/cache/warm-attrs", "dir=relative/path")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestWarmAttrs_ErrorSurfacesInLastError(t *testing.T) {
	done := make(chan struct{})
	fn := func(context.Context, string) error {
		defer close(done)
		return errors.New("boom")
	}
	s := withTestServer(Config{Cwd: "/tmp", WarmAttrsFn: fn})
	postForm(t, s.handleWarmAttrs, "/api/cache/warm-attrs", "")
	<-done
	// Spin until state settles.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if state := s.warming.Load(); state != nil && state.LastError != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := s.warming.Load()
	if state == nil || state.LastError != "boom" {
		t.Errorf("LastError = %+v, want \"boom\"", state)
	}
}

// --- warm-bodies ---

func TestWarmBodies_FiresGoroutine(t *testing.T) {
	done := make(chan struct{})
	s := withTestServer(Config{
		Cwd:        "/tmp",
		WarmBodyFn: func(context.Context, string) error { close(done); return nil },
	})
	rec := postForm(t, s.handleWarmBodies, "/api/cache/warm-bodies", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("warm-bodies fn never fired")
	}
}

func TestWarmBodies_412WhenUnwired(t *testing.T) {
	s := withTestServer(Config{Cwd: "/tmp"}) // no WarmBodyFn
	rec := postForm(t, s.handleWarmBodies, "/api/cache/warm-bodies", "")
	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", rec.Code)
	}
}

// --- warm-embeddings ---

func TestWarmEmbeddings_412WhenNoModel(t *testing.T) {
	s := withTestServer(Config{Cwd: "/tmp"}) // no WarmEmbeddingsFn
	rec := postForm(t, s.handleWarmEmbeddings, "/api/cache/warm-embeddings", "")
	if rec.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", rec.Code)
	}
}

func TestWarmEmbeddings_FiresWhenWired(t *testing.T) {
	done := make(chan struct{})
	s := withTestServer(Config{
		Cwd:              "/tmp",
		WarmEmbeddingsFn: func(context.Context, string) error { close(done); return nil },
	})
	rec := postForm(t, s.handleWarmEmbeddings, "/api/cache/warm-embeddings", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("warm-embeddings fn never fired")
	}
}

// --- concurrent-warm gate ---

func TestWarm_409WhenAnotherInFlight(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	s := withTestServer(Config{
		Cwd: "/tmp",
		WarmAttrsFn: func(ctx context.Context, _ string) error {
			<-block // hold the goroutine open
			return nil
		},
	})
	rec1 := postForm(t, s.handleWarmAttrs, "/api/cache/warm-attrs", "")
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("first POST status = %d", rec1.Code)
	}
	// Second POST should 409 while the first is still in flight.
	rec2 := postForm(t, s.handleWarmAttrs, "/api/cache/warm-attrs", "")
	if rec2.Code != http.StatusConflict {
		t.Errorf("second POST status = %d, want 409", rec2.Code)
	}
}

// --- overview reflects mutation surface ---

func TestOverview_AdvertisesMutationFlags(t *testing.T) {
	s := withTestServer(Config{
		Cwd:              "/tmp",
		WarmAttrsFn:      func(context.Context, string) error { return nil },
		WarmBodyFn:       func(context.Context, string) error { return nil },
		WarmEmbeddingsFn: nil, // intentionally unwired
	})
	o := decode(t, s.handleOverview, "/api/overview")
	if o["warm_attrs_available"] != true {
		t.Errorf("warm_attrs_available = %v, want true", o["warm_attrs_available"])
	}
	if o["warm_bodies_available"] != true {
		t.Errorf("warm_bodies_available = %v, want true", o["warm_bodies_available"])
	}
	if o["warm_embeddings_available"] != false {
		t.Errorf("warm_embeddings_available = %v, want false", o["warm_embeddings_available"])
	}
	if o["cwd"] != "/tmp" {
		t.Errorf("cwd = %v, want /tmp", o["cwd"])
	}
}

// absPath is a tiny helper that returns a clean absolute path under
// /Users/test/proj/<name>. We don't actually create any file here —
// the index Put/Peek/Delete operations don't stat.
func absPath(t *testing.T, name string) string {
	t.Helper()
	if name == "" {
		return "/Users/test/proj"
	}
	p := filepath.Join("/Users/test/proj", name)
	if filepath.Clean(p) != p {
		t.Fatalf("test path %q not clean", p)
	}
	return p
}
