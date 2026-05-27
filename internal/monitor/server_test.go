package monitor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/index"
)

func newTestServer(coll *Collector) *Server {
	return NewServer(Config{
		Version:     "test-1.2.3",
		Mode:        "mcp-stdio",
		Index:       index.NewMemory(),
		Collector:   coll,
		EmbedServer: "http://localhost:1", // unreachable → reachable:false fast
		EmbedModel:  "nomic-embed-text",
	})
}

func decode(t *testing.T, h http.HandlerFunc, path string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d", path, rec.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s decode: %v", path, err)
	}
	return out
}

func TestServer_Overview(t *testing.T) {
	s := newTestServer(nil)
	o := decode(t, s.handleOverview, "/api/overview")
	if o["version"] != "test-1.2.3" {
		t.Errorf("version = %v", o["version"])
	}
	if o["mode"] != "mcp-stdio" {
		t.Errorf("mode = %v", o["mode"])
	}
	if o["index_backing"] != "in-memory" {
		t.Errorf("index_backing = %v", o["index_backing"])
	}
}

func TestServer_Cache(t *testing.T) {
	s := newTestServer(nil)
	c := decode(t, s.handleCache, "/api/cache")
	if c["available"] != true {
		t.Fatalf("cache available = %v", c["available"])
	}
	if _, ok := c["attr"].(map[string]any); !ok {
		t.Errorf("missing attr block")
	}
	if _, ok := c["body"].(map[string]any); !ok {
		t.Errorf("missing body block")
	}
}

func TestServer_Activity(t *testing.T) {
	// nil collector → unavailable.
	if d := decode(t, newTestServer(nil).handleActivity, "/api/activity"); d["available"] != false {
		t.Errorf("nil-collector activity available = %v, want false", d["available"])
	}
	// with collector → available, reflects a recorded call.
	coll := NewCollector()
	coll.Record("search", 5*time.Millisecond, OutcomeOK, "", 3)
	d := decode(t, newTestServer(coll).handleActivity, "/api/activity")
	if d["available"] != true {
		t.Fatalf("activity available = %v", d["available"])
	}
	snap := d["snapshot"].(map[string]any)
	if snap["total_calls"].(float64) != 1 {
		t.Errorf("total_calls = %v, want 1", snap["total_calls"])
	}
}

func TestServer_Capabilities(t *testing.T) {
	s := newTestServer(nil)
	c := decode(t, s.handleCapabilities, "/api/capabilities")
	ct := c["content_types"].(map[string]any)
	if ct["total"].(float64) < 50 {
		t.Errorf("content_types total = %v, want many", ct["total"])
	}
	if len(c["project_types"].([]any)) < 10 {
		t.Errorf("project_types too few")
	}
	emb := c["embedder"].(map[string]any)
	if emb["reachable"] != false {
		t.Errorf("unreachable embedder should report reachable:false, got %v", emb["reachable"])
	}
}

func TestServer_Healthz(t *testing.T) {
	s := newTestServer(nil)
	h := decode(t, s.handleHealthz, "/healthz")
	if h["status"] != "ok" || h["index_open"] != true {
		t.Errorf("healthz = %+v", h)
	}
}

func TestForceLoopback(t *testing.T) {
	cases := map[string]string{
		":9090":           "127.0.0.1:9090",
		"9090":            "127.0.0.1:9090",
		"0.0.0.0:9090":    "127.0.0.1:9090", // non-loopback host forced to loopback
		"127.0.0.1:9090":  "127.0.0.1:9090",
		"localhost:9090":  "127.0.0.1:9090",
		"192.168.1.5:443": "127.0.0.1:443",
	}
	for in, want := range cases {
		if got := forceLoopback(in); got != want {
			t.Errorf("forceLoopback(%q) = %q, want %q", in, got, want)
		}
	}
}
