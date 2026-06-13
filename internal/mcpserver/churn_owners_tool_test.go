package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/gitmeta"
)

func TestChurnOwnersTool(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	dir := t.TempDir()
	mustGitSearchTest(t, dir, "init", "-q", "-b", "main")
	mustGitSearchTest(t, dir, "config", "commit.gpgsign", "false")

	commit := func(rel, name, email string) {
		full := filepath.Join(dir, rel)
		mkWrite(t, full, "# "+rel+"\n")
		mustGitSearchTest(t, dir, "add", rel)
		mustGitSearchTest(t, dir, "-c", "user.name="+name, "-c", "user.email="+email,
			"commit", "-q", "-m", "add "+rel)
	}
	commit("solo/a.md", "Alice", "alice@example.com")
	commit("solo/b.md", "Alice", "alice@example.com")
	commit("shared/c.md", "Alice", "alice@example.com")
	commit("shared/d.md", "Bob", "bob@example.com")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "churn_owners",
		Arguments: ChurnOwnersInput{Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ChurnOwnersOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	byBase := map[string]int{} // base dir -> distinct authors
	share := map[string]float64{}
	for _, d := range out.Dirs {
		byBase[filepath.Base(d.Dir)] = d.DistinctAuthors
		share[filepath.Base(d.Dir)] = d.TopAuthorShare
	}
	if byBase["solo"] != 1 {
		t.Errorf("solo distinct_authors = %d, want 1 (%+v)", byBase["solo"], out.Dirs)
	}
	if byBase["shared"] != 2 {
		t.Errorf("shared distinct_authors = %d, want 2", byBase["shared"])
	}
	if share["solo"] != 1.0 {
		t.Errorf("solo top_author_share = %v, want 1.0", share["solo"])
	}
	// Ranked single-author-first.
	if len(out.Dirs) >= 1 && out.Dirs[0].DistinctAuthors != 1 {
		t.Errorf("first ranked dir should be single-author, got %d", out.Dirs[0].DistinctAuthors)
	}
}
