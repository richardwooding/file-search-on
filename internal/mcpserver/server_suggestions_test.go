package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/richardwooding/file-search-on/internal/index"
)

// TestSearchSuggestionsOnTimeout verifies the MCP `search` tool
// populates Suggestions on cancellation. Issue #168 sub-feature C.
//
// We force a timeout with a 1ms cap on a tree where the lax filter
// (expr="true") would walk everything, then assert the response
// surfaces the bump-timeout AND the lax-filter heuristics.
func TestSearchSuggestionsOnTimeout(t *testing.T) {
	dir := t.TempDir()
	for i := range 200 {
		mustWrite(t, filepath.Join(dir, sprintfPad(i)+".md"), "# h\nbody\n")
	}

	ctx := t.Context()
	server := New("test", index.NewMemory(), 0, EmbedDefaults{})
	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	tinyTimeout := 0.001
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:           "true",
			Dir:            dir,
			TimeoutSeconds: &tinyTimeout,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)

	if !out.Cancelled {
		t.Fatalf("expected cancelled=true; got %+v", out)
	}
	if len(out.Suggestions) == 0 {
		t.Fatal("expected at least one suggestion on cancellation, got 0")
	}

	// Bump-timeout should fire (reason=timeout).
	if !anyContains(out.Suggestions, "Walk hit the timeout") {
		t.Errorf("bump-timeout suggestion missing: %v", out.Suggestions)
	}

	// Lax-filter should fire (expr="true").
	if !anyContains(out.Suggestions, "CEL filter is empty") {
		t.Errorf("lax-filter suggestion missing: %v", out.Suggestions)
	}

	// Missing-prunes should fire (no excludes, no respect_gitignore).
	if !anyContains(out.Suggestions, "node_modules") {
		t.Errorf("missing-prunes suggestion missing: %v", out.Suggestions)
	}
}

// TestSearchSuggestionsEmptyOnSuccess verifies that successful walks
// (no cancellation) don't populate Suggestions. Suggestions are an
// agent-recovery hint, not a permanent advice field.
func TestSearchSuggestionsEmptyOnSuccess(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# heading\n")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr: "is_markdown",
			Dir:  dir,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)

	if out.Cancelled {
		t.Fatalf("expected cancelled=false for tiny successful walk; got %+v", out)
	}
	if len(out.Suggestions) > 0 {
		t.Errorf("suggestions populated on successful walk: %v", out.Suggestions)
	}
}

func anyContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
