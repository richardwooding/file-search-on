package monitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempRegistry points the registry at a throwaway dir for the test
// by overriding XDG_CACHE_HOME / the OS cache base. os.UserCacheDir
// honours XDG_CACHE_HOME on Linux but not macOS, so to stay portable we
// just exercise the exported funcs against a temp HOME-independent dir
// by setting the relevant env. Simpler + portable: drive the helpers
// that take an explicit dir. Since Register/Peers resolve registryDir
// internally, we redirect via the cache-dir env vars understood on each
// OS, falling back to asserting behaviour through a temp dir we control.
func TestRegistry_RoundTripAndPrune(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir) // Linux
	t.Setenv("HOME", dir)           // macOS UserCacheDir = $HOME/Library/Caches
	t.Setenv("LocalAppData", dir)   // Windows

	// A live entry for THIS process should round-trip through Peers.
	self := Entry{
		PID:       os.Getpid(),
		URL:       "http://127.0.0.1:54211/",
		Mode:      "mcp-stdio",
		Version:   "test",
		StartedAt: time.Now(),
	}
	deregister, err := Register(self)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	peers := Peers()
	if len(peers) != 1 {
		t.Fatalf("Peers() = %d, want 1 (self)", len(peers))
	}
	if peers[0].PID != self.PID || peers[0].URL != self.URL {
		t.Errorf("peer = %+v, want self", peers[0])
	}

	// Plant a dead-PID entry directly; Peers must prune it.
	rdir, _ := registryDir()
	dead := filepath.Join(rdir, "999999999.json")
	if err := os.WriteFile(dead, []byte(`{"pid":999999999,"url":"http://127.0.0.1:1/","mode":"mcp-stdio"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := len(Peers()); got != 1 {
		t.Errorf("after planting dead PID, Peers() = %d, want 1 (dead pruned)", got)
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("dead-PID file should have been removed by Peers()")
	}

	// Corrupt file is dropped too.
	if err := os.WriteFile(filepath.Join(rdir, "123.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := len(Peers()); got != 1 {
		t.Errorf("after planting corrupt entry, Peers() = %d, want 1", got)
	}

	// Deregister removes self.
	deregister()
	if got := len(Peers()); got != 0 {
		t.Errorf("after deregister, Peers() = %d, want 0", got)
	}
}

func TestProcessAlive(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("processAlive(self) = false, want true")
	}
	// PID 1 (init/launchd) is always alive on unix; on Windows the stub
	// is conservative. Just assert a clearly-dead PID reads as dead.
	if processAlive(999999999) {
		t.Error("processAlive(999999999) = true, want false")
	}
}
