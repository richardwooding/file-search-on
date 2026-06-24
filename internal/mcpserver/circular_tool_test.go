package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCircularTool(t *testing.T) {
	root := t.TempDir()
	mkWrite(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	// a → b → a is a 2-node cycle.
	mkWrite(t, filepath.Join(root, "a", "a.go"), "package a\n\nimport \"example.com/m/b\"\n\nfunc A() { b.B() }\n")
	mkWrite(t, filepath.Join(root, "b", "b.go"), "package b\n\nimport \"example.com/m/a\"\n\nfunc B() { a.A() }\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "circular",
		Arguments: CircularInput{codeGraphWalkInput: codeGraphWalkInput{Dir: root}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out CircularOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Count != 1 || len(out.Cycles) != 1 {
		t.Fatalf("Count = %d, cycles = %+v, want one cycle", out.Count, out.Cycles)
	}
	c := out.Cycles[0]
	if c.Length != 2 || c.Nodes[0] != "example.com/m/a" || c.Nodes[1] != "example.com/m/b" {
		t.Errorf("cycle = %+v, want {a,b} length 2", c)
	}
}
