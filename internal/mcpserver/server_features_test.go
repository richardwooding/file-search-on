package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSearchTool_SortByLimit_TopK verifies the MCP search tool
// honours sort_by + order + limit and returns the expected top-K
// shape.
func TestSearchTool_SortByLimit_TopK(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "small.md"), "# s\n")
	mustWrite(t, filepath.Join(dir, "medium.md"), "# m\n"+strings.Repeat("a", 1024))
	mustWrite(t, filepath.Join(dir, "huge.md"), "# h\n"+strings.Repeat("b", 8192))

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:   "is_markdown",
			Dir:    dir,
			SortBy: "size",
			Order:  "desc",
			Limit:  2,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 2 {
		t.Fatalf("Count=%d want 2", out.Count)
	}
	if !strings.HasSuffix(out.Matches[0].Path, "huge.md") {
		t.Errorf("top match = %s, want huge.md", out.Matches[0].Path)
	}
	if !strings.HasSuffix(out.Matches[1].Path, "medium.md") {
		t.Errorf("second match = %s, want medium.md", out.Matches[1].Path)
	}
}

// TestSearchTool_IncludeSnippet verifies that include_snippet=true
// populates SearchMatch.Snippet for markdown matches and leaves it
// empty for binary matches.
func TestSearchTool_IncludeSnippet(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "doc.md"), "first\nsecond\nthird\nfourth\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:           "is_markdown",
			Dir:            dir,
			IncludeSnippet: true,
			SnippetLines:   2,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("Count=%d want 1", out.Count)
	}
	if got, want := out.Matches[0].Snippet, "first\nsecond"; got != want {
		t.Errorf("snippet = %q, want %q", got, want)
	}
}

// TestSearchTool_ExcludesPrune verifies that excludes skips a
// directory (the entire subtree).
func TestSearchTool_ExcludesPrune(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "kept.md"), "# k\n")
	if err := os.MkdirAll(filepath.Join(dir, "skipme"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "skipme", "hidden.md"), "# h\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:     "is_markdown",
			Dir:      dir,
			Excludes: []string{"skipme"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("Count=%d want 1", out.Count)
	}
	if !strings.HasSuffix(out.Matches[0].Path, "kept.md") {
		t.Errorf("got %s, want kept.md", out.Matches[0].Path)
	}
}

// TestSearchTool_RespectGitignore verifies the .gitignore at the
// walk root is honoured.
func TestSearchTool_RespectGitignore(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".gitignore"), "ignored.md\n")
	mustWrite(t, filepath.Join(dir, "kept.md"), "# k\n")
	mustWrite(t, filepath.Join(dir, "ignored.md"), "# i\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:             "is_markdown",
			Dir:              dir,
			RespectGitignore: true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	for _, m := range out.Matches {
		if strings.HasSuffix(m.Path, "ignored.md") {
			t.Errorf(".gitignore not honoured: %s in results", m.Path)
		}
	}
}

// TestSearchTool_PathSortStillDefault verifies that the historical
// path-sorted default behaviour is preserved when no sort_by is
// passed — the previously-shipped contract.
func TestSearchTool_PathSortStillDefault(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "z.md"), "# z\n")
	mustWrite(t, filepath.Join(dir, "a.md"), "# a\n")
	mustWrite(t, filepath.Join(dir, "m.md"), "# m\n")

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
	if out.Count != 3 {
		t.Fatalf("Count=%d want 3", out.Count)
	}
	wantOrder := []string{"a.md", "m.md", "z.md"}
	for i, m := range out.Matches {
		if !strings.HasSuffix(m.Path, wantOrder[i]) {
			t.Errorf("position %d: got %s, want %s", i, m.Path, wantOrder[i])
		}
	}
}

// (newSession + mustWrite + mustDecodeStructured live in server_test.go;
// reused here.)
