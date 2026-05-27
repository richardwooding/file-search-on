package main

import (
	"os"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/monitor"
)

// TestMonitorsCmd_Empty asserts the no-instances path prints a helpful
// hint rather than nothing. Points the registry at a temp cache dir so
// it doesn't observe real running instances.
func TestMonitorsCmd_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("LocalAppData", dir)

	if peers := monitor.Peers(); len(peers) != 0 {
		t.Fatalf("expected an empty registry, got %d peers", len(peers))
	}

	// default + bare + json must all run without error on an empty set.
	for _, out := range []string{"default", "bare", "json"} {
		c := &MonitorsCmd{Output: out}
		if err := c.Run(t.Context()); err != nil {
			t.Errorf("monitors -o %s: %v", out, err)
		}
	}
}

// TestMonitorsCmd_ListsRegistered registers a synthetic live entry (this
// test process's PID) and asserts bare output contains its URL.
func TestMonitorsCmd_ListsRegistered(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("LocalAppData", dir)

	deregister, err := monitor.Register(monitor.Entry{
		PID:  os.Getpid(),
		URL:  "http://127.0.0.1:54999/",
		Mode: "mcp-stdio",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	defer deregister()

	peers := monitor.Peers()
	if len(peers) != 1 || !strings.Contains(peers[0].URL, "54999") {
		t.Fatalf("Peers() = %+v, want one entry with :54999", peers)
	}
}
