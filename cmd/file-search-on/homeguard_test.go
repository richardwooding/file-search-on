package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureUnderHome(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir() // sibling of home — not inside it
	under := filepath.Join(home, "proj")
	if err := os.MkdirAll(under, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("dir under home passes", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := ensureUnderHome([]string{under}, false); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("home itself passes (inclusive)", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := ensureUnderHome([]string{home}, false); err != nil {
			t.Errorf("expected nil for home itself, got %v", err)
		}
	})

	t.Run("dir outside home is refused", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		err := ensureUnderHome([]string{outside}, false)
		if err == nil {
			t.Fatal("expected refusal for dir outside home")
		}
		if !strings.Contains(err.Error(), "--allow-outside-home") {
			t.Errorf("error should name the opt-out flag, got %v", err)
		}
	})

	t.Run("opt-out allows outside dir", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := ensureUnderHome([]string{outside}, true); err != nil {
			t.Errorf("opt-out should pass, got %v", err)
		}
	})

	t.Run("one outside dir among several is refused", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := ensureUnderHome([]string{under, outside}, false); err == nil {
			t.Fatal("expected refusal when any dir is outside home")
		}
	})

	t.Run("empty dirs pass", func(t *testing.T) {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		if err := ensureUnderHome([]string{"", ""}, false); err != nil {
			t.Errorf("empty dirs should pass, got %v", err)
		}
	})
}

func TestEnsureUnderHome_NoHomeFailsClosed(t *testing.T) {
	home := t.TempDir()
	under := filepath.Join(home, "proj")
	if err := os.MkdirAll(under, 0o755); err != nil {
		t.Fatal(err)
	}
	// Unset both so os.UserHomeDir errors on every platform.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if err := ensureUnderHome([]string{under}, false); err == nil {
		t.Error("expected fail-closed error when $HOME is unresolvable")
	}
	// Opt-out still works without a resolvable home.
	if err := ensureUnderHome([]string{under}, true); err != nil {
		t.Errorf("opt-out should pass even without $HOME, got %v", err)
	}
}

// TestWatchCmd_RefusesOutsideHome confirms the guard fires at Run entry,
// before any index/watcher side effects, so an outside -d is rejected.
func TestWatchCmd_RefusesOutsideHome(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	c := &WatchCmd{Dir: []string{outside}}
	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected watch to refuse an outside -d directory")
	}
	if !strings.Contains(err.Error(), "home-guard") {
		t.Errorf("expected a home-guard error, got %v", err)
	}
}

// TestMCPCmd_RefusesOutsideHome confirms the mcp server refuses to start
// when an explicit root is outside $HOME (the guard runs first, so the
// server never binds).
func TestMCPCmd_RefusesOutsideHome(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m := &MCPCmd{WarmDir: outside}
	err := m.Run(context.Background())
	if err == nil {
		t.Fatal("expected mcp to refuse an outside --warm-dir")
	}
	if !strings.Contains(err.Error(), "home-guard") {
		t.Errorf("expected a home-guard error, got %v", err)
	}
}
