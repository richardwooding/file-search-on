package mcpserver

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/gitmeta"
	"github.com/richardwooding/file-search-on/internal/index"
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

// TestSearchTool_WithGit_PoolReusesAcrossCalls confirms the
// gitmeta.Pool wiring: two consecutive search calls against the same
// repo share one *gitmeta.Cache. We can't reach inside the handler to
// compare pointers, but the pool's public Len() method lets us assert
// "exactly one entry created across both calls". Issue #271 follow-up.
func TestSearchTool_WithGit_PoolReusesAcrossCalls(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	dir := t.TempDir()
	mustGitSearchTest(t, dir, "init", "-q", "-b", "main")
	mustGitSearchTest(t, dir, "config", "user.email", "test@example.com")
	mustGitSearchTest(t, dir, "config", "user.name", "Pool Tester")
	mustGitSearchTest(t, dir, "config", "commit.gpgsign", "false")
	mustWrite(t, filepath.Join(dir, "a.md"), "# a\n")
	mustGitSearchTest(t, dir, "add", "a.md")
	mustGitSearchTest(t, dir, "commit", "-q", "-m", "Add a")

	// Build a server with an explicit pool we can inspect post-call.
	pool := gitmeta.NewPool()
	ctx := context.Background()
	server := New("test", index.NewMemory(), 0, EmbedDefaults{}, WithGitPool(pool))
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

	// Two consecutive searches against the same repo.
	for i := range 2 {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "search",
			Arguments: SearchInput{
				Expr:    "is_markdown && is_git_tracked",
				Dir:     dir,
				WithGit: true,
			},
		})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if res.GetError() != nil {
			t.Fatalf("call %d returned error: %v", i, res.GetError())
		}
		var out SearchOutput
		mustDecodeStructured(t, res, &out)
		if out.Count != 1 {
			t.Errorf("call %d: expected 1 match, got %d", i, out.Count)
		}
	}

	// The pool should have exactly one entry — both calls reused it.
	if got := pool.Len(); got != 1 {
		t.Errorf("pool.Len() = %d after two calls; want 1 (cache reuse failed)", got)
	}
}
