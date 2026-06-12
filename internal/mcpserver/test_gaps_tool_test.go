package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTestGapsTool(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "p/prod.go"),
		"package p\n\nfunc Tested() {}\nfunc Untested() {}\n")
	mkWrite(t, filepath.Join(dir, "p/prod_test.go"),
		"package p\n\nimport \"testing\"\n\nfunc TestTested(t *testing.T) { Tested() }\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "test_gaps",
		Arguments: TestGapsInput{codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out TestGapsOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Count != 1 || len(out.Gaps) != 1 {
		t.Fatalf("Count=%d len(Gaps)=%d want 1/1: %+v", out.Count, len(out.Gaps), out.Gaps)
	}
	g := out.Gaps[0]
	if filepath.Base(g.Path) != "prod.go" {
		t.Errorf("gap path = %s, want prod.go", g.Path)
	}
	if len(g.UntestedFunctions) != 1 || g.UntestedFunctions[0] != "Untested" {
		t.Errorf("untested = %v, want [Untested]", g.UntestedFunctions)
	}
}
