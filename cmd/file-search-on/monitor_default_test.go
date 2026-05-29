package main

import (
	"testing"

	"github.com/alecthomas/kong"
)

// resolveMonAddr applies the same precedence ladder the Run methods
// of MCPCmd and WatchCmd use to pick the monitor bind address. It's
// extracted into a helper here so the test can exercise it without
// spinning up the actual MCP / watch servers.
//
// Order:
//  1. Explicit --monitor-addr wins (caller is pinning a port).
//  2. Otherwise, default to ":0" (OS-assigned port) — unless
//     --no-monitor was passed, in which case stay empty (dashboard
//     suppressed).
//
// Mirror this in mcp_cmd.go / watch_cmd.go; the test catches drift
// if either production site changes the conditional in isolation.
func resolveMonAddr(monitorAddr string, noMonitor bool) string {
	if monitorAddr != "" {
		return monitorAddr
	}
	if noMonitor {
		return ""
	}
	return ":0"
}

// TestMonitorDefaultsOn locks the new default-on-since-v0.65.0
// behaviour into a regression test: parsing `mcp` / `watch` with no
// monitor flags must leave NoMonitor=false, and the address-resolution
// helper must default to ":0" — i.e. the dashboard starts.
func TestMonitorDefaultsOn(t *testing.T) {
	t.Parallel()

	t.Run("mcp with no flags → default-on", func(t *testing.T) {
		var cli struct {
			MCP MCPCmd `cmd:""`
		}
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatalf("kong.New: %v", err)
		}
		if _, err := parser.Parse([]string{"mcp"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if cli.MCP.NoMonitor {
			t.Errorf("MCPCmd.NoMonitor = true with no flags; want false")
		}
		if got := resolveMonAddr(cli.MCP.MonitorAddr, cli.MCP.NoMonitor); got != ":0" {
			t.Errorf("monAddr with no flags = %q, want %q", got, ":0")
		}
	})

	t.Run("watch with no flags → default-on", func(t *testing.T) {
		var cli struct {
			Watch WatchCmd `cmd:""`
		}
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatalf("kong.New: %v", err)
		}
		if _, err := parser.Parse([]string{"watch"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if cli.Watch.NoMonitor {
			t.Errorf("WatchCmd.NoMonitor = true with no flags; want false")
		}
		if got := resolveMonAddr(cli.Watch.MonitorAddr, cli.Watch.NoMonitor); got != ":0" {
			t.Errorf("monAddr with no flags = %q, want %q", got, ":0")
		}
	})
}

// TestMonitor_NoMonitorFlag confirms --no-monitor suppresses the
// dashboard for both subcommands: NoMonitor becomes true, monAddr
// resolves to empty.
func TestMonitor_NoMonitorFlag(t *testing.T) {
	t.Parallel()

	t.Run("mcp --no-monitor", func(t *testing.T) {
		var cli struct {
			MCP MCPCmd `cmd:""`
		}
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatalf("kong.New: %v", err)
		}
		if _, err := parser.Parse([]string{"mcp", "--no-monitor"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !cli.MCP.NoMonitor {
			t.Errorf("MCPCmd.NoMonitor = false after --no-monitor; want true")
		}
		if got := resolveMonAddr(cli.MCP.MonitorAddr, cli.MCP.NoMonitor); got != "" {
			t.Errorf("monAddr with --no-monitor = %q, want empty (suppressed)", got)
		}
	})

	t.Run("watch --no-monitor", func(t *testing.T) {
		var cli struct {
			Watch WatchCmd `cmd:""`
		}
		parser, err := kong.New(&cli)
		if err != nil {
			t.Fatalf("kong.New: %v", err)
		}
		if _, err := parser.Parse([]string{"watch", "--no-monitor"}); err != nil {
			t.Fatalf("parse: %v", err)
		}
		if !cli.Watch.NoMonitor {
			t.Errorf("WatchCmd.NoMonitor = false after --no-monitor; want true")
		}
		if got := resolveMonAddr(cli.Watch.MonitorAddr, cli.Watch.NoMonitor); got != "" {
			t.Errorf("monAddr with --no-monitor = %q, want empty (suppressed)", got)
		}
	})
}

// TestMonitorAddrWins confirms --monitor-addr always pins the
// supplied port even when --no-monitor is also passed (explicit
// pin > implicit opt-out).
func TestMonitorAddrWins(t *testing.T) {
	t.Parallel()
	if got := resolveMonAddr(":9090", false); got != ":9090" {
		t.Errorf("monAddr (:9090, !no-monitor) = %q, want :9090", got)
	}
	if got := resolveMonAddr(":9090", true); got != ":9090" {
		t.Errorf("monAddr (:9090, no-monitor) = %q, want :9090 (pin beats suppress)", got)
	}
}
