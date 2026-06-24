package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/gitmeta"
)

func TestReviewTool(t *testing.T) {
	if !gitmeta.HasGitBinary() {
		t.Skip("git binary not on PATH")
	}
	dir := t.TempDir()
	mustGitSearchTest(t, dir, "init", "-q", "-b", "main")
	mustGitSearchTest(t, dir, "config", "commit.gpgsign", "false")

	commit := func(rel, body string) {
		mkWrite(t, filepath.Join(dir, rel), body)
		mustGitSearchTest(t, dir, "add", rel)
		mustGitSearchTest(t, dir, "-c", "user.name=Dev", "-c", "user.email=dev@example.com",
			"commit", "-q", "-m", "add "+rel)
	}
	commit("go.mod", "module example.com/m\n\ngo 1.26\n")
	commit("simple.go", "package m\n\nfunc Simple() int { return 1 }\n")

	// Second commit adds a high-complexity function (20 if-branches → ~21).
	var b strings.Builder
	b.WriteString("package m\n\nfunc Branchy(x int) int {\n\tr := 0\n")
	for i := 1; i <= 20; i++ {
		b.WriteString("\tif x > ")
		b.WriteByte(byte('0' + i%10))
		b.WriteString(" {\n\t\tr++\n\t}\n")
	}
	b.WriteString("\treturn r\n}\n")
	commit("branchy.go", b.String())

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "review",
		Arguments: ReviewInput{
			codeGraphWalkInput: codeGraphWalkInput{Dir: dir},
			Base:               "HEAD~1",
			SkipDeadCode:       true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ReviewOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Verdict != "fail" {
		t.Fatalf("verdict = %q, want fail (findings: %+v)", out.Verdict, out.Findings)
	}
	if out.FailCount == 0 {
		t.Errorf("FailCount = 0, want > 0")
	}
	var found bool
	for _, f := range out.Findings {
		if f.Rule == "complexity" && strings.HasSuffix(f.Path, "branchy.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("no complexity finding for branchy.go in %+v", out.Findings)
	}
}
