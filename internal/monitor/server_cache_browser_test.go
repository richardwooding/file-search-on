package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/index"
)

// seedFixtureIndex builds an in-memory index with 5 attrs entries and
// 2 body entries, all backed by real files in t.TempDir() so the
// staleness probe (os.Stat in the handler) returns false on fresh
// reads.
func seedFixtureIndex(t *testing.T) (idx index.Index, tmp string, paths []string) {
	t.Helper()
	idx = index.NewMemory()
	tmp = t.TempDir()
	for _, b := range []string{"alpha.md", "beta.md", "gamma.go", "delta.txt", "epsilon.md"} {
		p := filepath.Join(tmp, b)
		if err := os.WriteFile(p, []byte("body of "+b), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if err := idx.Put(p, &index.Entry{
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			ContentType:     "text",
			Extra:           map[string]any{"basename": b, "word_count": 3},
		}); err != nil {
			t.Fatalf("Put: %v", err)
		}
		paths = append(paths, p)
	}
	// Two of the same files also have body cache entries.
	for _, p := range paths[:2] {
		info, _ := os.Stat(p)
		if err := idx.PutBody(p, &index.BodyEntry{
			Size:            info.Size(),
			ModTimeUnixNano: info.ModTime().UnixNano(),
			Body:            "cached body for " + filepath.Base(p),
		}); err != nil {
			t.Fatalf("PutBody: %v", err)
		}
	}
	return idx, tmp, paths
}

func newCacheBrowserTestServer(t *testing.T) (*Server, []string) {
	t.Helper()
	idx, _, paths := seedFixtureIndex(t)
	s := NewServer(Config{
		Version:     "test-1.2.3",
		Mode:        "mcp-stdio",
		Index:       idx,
		EmbedServer: "http://localhost:1",
		EmbedModel:  "nomic-embed-text",
	})
	return s, paths
}

func decodeCache(t *testing.T, h http.HandlerFunc, url string, wantStatus int) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, url, nil))
	if rec.Code != wantStatus {
		t.Fatalf("%s: status = %d, want %d (body: %s)", url, rec.Code, wantStatus, rec.Body.String())
	}
	if wantStatus != http.StatusOK {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s decode: %v", url, err)
	}
	return out
}

func TestServer_CacheEntries_AttrsFilteredAndPaginated(t *testing.T) {
	s, _ := newCacheBrowserTestServer(t)

	// No filter — 5 entries total.
	got := decodeCache(t, s.handleCacheEntries, "/api/cache/entries?bucket=attrs", http.StatusOK)
	if got["total"].(float64) != 5 {
		t.Errorf("total = %v, want 5", got["total"])
	}
	entries := got["entries"].([]any)
	if len(entries) != 5 {
		t.Errorf("entries len = %d, want 5", len(entries))
	}

	// Filter ".md" — 3 hits (alpha.md, beta.md, epsilon.md).
	got = decodeCache(t, s.handleCacheEntries, "/api/cache/entries?bucket=attrs&q=.md", http.StatusOK)
	if got["total"].(float64) != 3 {
		t.Errorf("filtered total = %v, want 3", got["total"])
	}

	// Pagination — limit=2, offset=1.
	got = decodeCache(t, s.handleCacheEntries, "/api/cache/entries?bucket=attrs&limit=2&offset=1", http.StatusOK)
	if entries := got["entries"].([]any); len(entries) != 2 {
		t.Errorf("paged len = %d, want 2", len(entries))
	}
	if got["total"].(float64) != 5 {
		t.Errorf("paged total = %v, want 5 (full count, not page size)", got["total"])
	}
}

func TestServer_CacheEntries_BodiesBucket(t *testing.T) {
	s, _ := newCacheBrowserTestServer(t)
	got := decodeCache(t, s.handleCacheEntries, "/api/cache/entries?bucket=bodies", http.StatusOK)
	if got["total"].(float64) != 2 {
		t.Errorf("body total = %v, want 2", got["total"])
	}
}

func TestServer_CacheEntries_BadBucket(t *testing.T) {
	s, _ := newCacheBrowserTestServer(t)
	rec := httptest.NewRecorder()
	s.handleCacheEntries(rec, httptest.NewRequest(http.MethodGet, "/api/cache/entries?bucket=garbage", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for bad bucket", rec.Code)
	}
}

func TestServer_CacheEntry_AttrsDetail(t *testing.T) {
	s, paths := newCacheBrowserTestServer(t)
	url := "/api/cache/entry?bucket=attrs&path=" + paths[0]
	got := decodeCache(t, s.handleCacheEntry, url, http.StatusOK)
	if got["path"] != paths[0] {
		t.Errorf("path = %v, want %v", got["path"], paths[0])
	}
	if got["content_type"] != "text" {
		t.Errorf("content_type = %v, want text", got["content_type"])
	}
	extra, ok := got["extra"].(map[string]any)
	if !ok {
		t.Fatalf("extra missing or wrong type: %v", got["extra"])
	}
	if extra["basename"] == nil {
		t.Errorf("expected basename in extra, got %v", extra)
	}
}

func TestServer_CacheEntry_BodiesDetail(t *testing.T) {
	s, paths := newCacheBrowserTestServer(t)
	url := "/api/cache/entry?bucket=bodies&path=" + paths[0]
	got := decodeCache(t, s.handleCacheEntry, url, http.StatusOK)
	if got["body"] == "" || got["body"] == nil {
		t.Errorf("expected non-empty body, got %v", got["body"])
	}
}

func TestServer_CacheEntry_NotFound(t *testing.T) {
	s, _ := newCacheBrowserTestServer(t)
	rec := httptest.NewRecorder()
	s.handleCacheEntry(rec, httptest.NewRequest(http.MethodGet, "/api/cache/entry?bucket=attrs&path=/nonexistent/path", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestServer_CacheEntry_MissingPath(t *testing.T) {
	s, _ := newCacheBrowserTestServer(t)
	rec := httptest.NewRecorder()
	s.handleCacheEntry(rec, httptest.NewRequest(http.MethodGet, "/api/cache/entry?bucket=attrs", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing path", rec.Code)
	}
}
