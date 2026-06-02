package mcpserver

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/gitmeta"
)

// TestSearchTool_WithGit_RoundTrip drives the MCP `search` tool with
// with_git=true against a temp git repo. Confirms is_git_tracked
// reaches the CEL filter and the matched file flows back through the
// in-memory transport — the end-to-end wiring sanity check.
func TestSearchTool_WithGit_RoundTrip(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	dir := t.TempDir()
	mustGitSearchTest(t, dir, "init", "-q", "-b", "main")
	mustGitSearchTest(t, dir, "config", "user.email", "test@example.com")
	mustGitSearchTest(t, dir, "config", "user.name", "MCP Tester")
	mustGitSearchTest(t, dir, "config", "commit.gpgsign", "false")

	mustWrite(t, filepath.Join(dir, "tracked.md"), "# tracked\n")
	mustGitSearchTest(t, dir, "add", "tracked.md")
	mustGitSearchTest(t, dir, "commit", "-q", "-m", "Add tracked")

	// Untracked sibling.
	mustWrite(t, filepath.Join(dir, "scratch.md"), "# scratch\n")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:    "is_markdown && is_git_tracked",
			Dir:     dir,
			WithGit: true,
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

	if out.Count != 1 {
		t.Fatalf("expected 1 tracked markdown match, got %d (%+v)", out.Count, out.Matches)
	}
	if got := filepath.Base(out.Matches[0].Path); got != "tracked.md" {
		t.Errorf("matched %s, want tracked.md", got)
	}
}

// TestSearchTool_WithGit_Off confirms with_git=false leaves the git_*
// predicates inactive: the same is_git_tracked filter returns zero.
func TestSearchTool_WithGit_Off(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	dir := t.TempDir()
	mustGitSearchTest(t, dir, "init", "-q", "-b", "main")
	mustGitSearchTest(t, dir, "config", "user.email", "test@example.com")
	mustGitSearchTest(t, dir, "config", "user.name", "MCP Tester")
	mustGitSearchTest(t, dir, "config", "commit.gpgsign", "false")
	mustWrite(t, filepath.Join(dir, "tracked.md"), "# tracked\n")
	mustGitSearchTest(t, dir, "add", "tracked.md")
	mustGitSearchTest(t, dir, "commit", "-q", "-m", "Add tracked")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Expr:    "is_markdown && is_git_tracked",
			Dir:     dir,
			WithGit: false,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 0 {
		t.Errorf("with_git=false → is_git_tracked filter should match nothing; got %d", out.Count)
	}
}

func mustGitSearchTest(t *testing.T, root string, args ...string) {
	t.Helper()
	full := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", full...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
