package monitor

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/index"
)

func TestServer_ListenReturnsDynamicURL(t *testing.T) {
	s := NewServer(Config{Version: "test", Mode: "mcp-stdio", Index: index.NewMemory()})
	url, err := s.Listen(":0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer func() { _ = s.ln.Close() }()
	if !strings.HasPrefix(url, "http://127.0.0.1:") || strings.HasSuffix(url, ":0/") {
		t.Errorf("Listen URL = %q, want http://127.0.0.1:<nonzero>/", url)
	}
	if s.URL() != url {
		t.Errorf("URL() = %q, want %q", s.URL(), url)
	}
}

func TestController_EnsureStartedIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("LocalAppData", dir)

	ctx := t.Context()
	c := NewController(ctx, Config{Version: "test", Mode: "mcp-stdio", Index: index.NewMemory()}, ":0")

	url1, err := c.EnsureStarted()
	if err != nil {
		t.Fatalf("EnsureStarted: %v", err)
	}
	url2, err := c.EnsureStarted()
	if err != nil {
		t.Fatalf("EnsureStarted (2nd): %v", err)
	}
	if url1 != url2 {
		t.Errorf("EnsureStarted not idempotent: %q vs %q", url1, url2)
	}
	if _, running := c.Info(); !running {
		t.Error("Info() running = false after EnsureStarted")
	}

	// The lazily-started dashboard should actually be serving + registered.
	if !waitHealthy(url1, time.Second) {
		t.Fatalf("dashboard at %s never became healthy", url1)
	}
	if got := len(Peers()); got != 1 {
		t.Errorf("Peers() = %d after lazy start, want 1", got)
	}
}

func waitHealthy(url string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url + "healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
