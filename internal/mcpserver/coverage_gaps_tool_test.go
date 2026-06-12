package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCoverageGapsTool(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.23\n")
	mkWrite(t, filepath.Join(dir, "foo.go"), "package m\n\n"+
		"func covered() int {\n\tx := 1\n\treturn x\n}\n"+ // 3-6
		"func uncovered() int {\n\ty := 2\n\treturn y\n}\n") // 7-10
	mkWrite(t, filepath.Join(dir, "cov.out"), "mode: set\n"+
		"example.com/m/foo.go:3.20,6.2 2 1\n"+
		"example.com/m/foo.go:7.22,10.2 2 0\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "coverage_gaps",
		Arguments: CoverageGapsInput{Profile: filepath.Join(dir, "cov.out"), Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out CoverageGapsOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.ProfileMode != "set" || out.FilesAnalysed != 1 {
		t.Errorf("mode=%q files=%d, want set/1", out.ProfileMode, out.FilesAnalysed)
	}
	if out.Count != 1 || len(out.Gaps) != 1 {
		t.Fatalf("Count=%d want 1 (only uncovered()): %+v", out.Count, out.Gaps)
	}
	if g := out.Gaps[0]; g.Function != "uncovered" || !g.FullyUncovered {
		t.Errorf("gap = %+v, want uncovered + fully_uncovered", g)
	}
}

func TestCoverageGapsTool_RequiresProfile(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "coverage_gaps",
		Arguments: CoverageGapsInput{Dir: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected a tool error when profile is omitted")
	}
}
