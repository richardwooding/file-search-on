package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestFindDuplicateFunctionsTool(t *testing.T) {
	dir := t.TempDir()
	dup := "func process(items []int) int {\n" +
		"\ttotal := 0\n" +
		"\tfor _, x := range items {\n" +
		"\t\ttotal += x * 2\n" +
		"\t}\n" +
		"\treturn total\n" +
		"}\n"
	mkWrite(t, filepath.Join(dir, "a/a.go"), "package a\n\n"+dup)
	mkWrite(t, filepath.Join(dir, "b/b.go"), "package b\n\n"+dup)

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_duplicate_functions",
		Arguments: FindDuplicateFunctionsInput{Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out FindDuplicateFunctionsOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.GroupCount != 1 || len(out.Groups) != 1 {
		t.Fatalf("GroupCount=%d len(Groups)=%d want 1/1: %+v", out.GroupCount, len(out.Groups), out.Groups)
	}
	g := out.Groups[0]
	if g.Count != 2 {
		t.Fatalf("group has %d members, want 2: %+v", g.Count, g.Members)
	}
	for _, m := range g.Members {
		if m.Symbol != "process" {
			t.Errorf("unexpected clustered symbol %q", m.Symbol)
		}
		if m.StartLine == 0 || m.EndLine < m.StartLine {
			t.Errorf("bad span for %s: %d-%d", m.Symbol, m.StartLine, m.EndLine)
		}
	}
}
