package mcpserver

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestWatchSearchFiresOnNewMatch creates a matching file shortly after
// the watch starts and asserts the bounded watch_search call returns it,
// stopping early via max_events.
func TestWatchSearchFiresOnNewMatch(t *testing.T) {
	dir := t.TempDir()
	ctx, cs := newSession(t)

	mdPath := filepath.Join(dir, "note.md")
	// Re-write the file a few times spaced out: the MCP handshake +
	// handler startup + recursive watcher registration take an
	// indeterminate amount of time, so a single early write can land
	// before the watch is armed. Each re-write emits a fresh WRITE
	// event, so a later one is guaranteed to be observed.
	go func() {
		for range 10 {
			time.Sleep(300 * time.Millisecond)
			_ = os.WriteFile(mdPath, []byte("# hello\n"), 0o644)
		}
	}()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "watch_search",
		Arguments: WatchSearchInput{
			Expr:            "is_markdown",
			Dir:             dir,
			DurationSeconds: 6,
			MaxEvents:       1,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out WatchSearchOutput
	mustDecodeStructured(t, res, &out)

	if out.Count != 1 {
		t.Fatalf("expected 1 match, got %d (%+v)", out.Count, out.Matches)
	}
	if got := filepath.Base(out.Matches[0].Path); got != "note.md" {
		t.Fatalf("expected note.md, got %s", got)
	}
	if !out.HitMaxEvents {
		t.Errorf("expected hit_max_events=true (max_events reached before duration)")
	}
}

// TestWatchSearchTimesOutEmpty asserts that a watch window with no
// matching activity returns an empty (non-nil) match set, not an error.
func TestWatchSearchTimesOutEmpty(t *testing.T) {
	dir := t.TempDir()
	ctx, cs := newSession(t)

	// Write a non-matching file during the window — must NOT surface.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(dir, "data.json"), []byte(`{"x":1}`), 0o644)
	}()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "watch_search",
		Arguments: WatchSearchInput{
			Expr:            "is_markdown",
			Dir:             dir,
			DurationSeconds: 1,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out WatchSearchOutput
	mustDecodeStructured(t, res, &out)

	if out.Count != 0 {
		t.Fatalf("expected 0 matches for is_markdown when only a JSON file appeared, got %d (%+v)", out.Count, out.Matches)
	}
	if out.Matches == nil {
		t.Errorf("expected non-nil empty matches slice")
	}
	if out.HitMaxEvents {
		t.Errorf("hit_max_events should be false when no matches collected")
	}
}

// TestWatchSearchInvalidExpr asserts a malformed CEL expression surfaces
// as a tool error rather than an empty result.
func TestWatchSearchInvalidExpr(t *testing.T) {
	dir := t.TempDir()
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "watch_search",
		Arguments: WatchSearchInput{
			Expr:            "this is not valid CEL %%%",
			Dir:             dir,
			DurationSeconds: 1,
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for an invalid CEL expression")
	}
}
