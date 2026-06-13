package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCouplingTool(t *testing.T) {
	root := t.TempDir()
	mkWrite(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	mkWrite(t, filepath.Join(root, "a", "a.go"), "package a\n\n"+
		"import \"example.com/m/c\"\n\nfunc A() { c.C() }\n")
	mkWrite(t, filepath.Join(root, "c", "c.go"), "package c\n\nfunc C() {}\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "coupling",
		Arguments: CouplingInput{codeGraphWalkInput: codeGraphWalkInput{Dir: root}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out CouplingOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Module != "example.com/m" {
		t.Fatalf("module = %q, want example.com/m", out.Module)
	}
	byPkg := map[string]int{} // package -> afferent
	for _, p := range out.Packages {
		byPkg[p.Package] = p.Afferent
	}
	if byPkg["example.com/m/c"] != 1 {
		t.Errorf("c afferent = %d, want 1 (imported by a): %+v", byPkg["example.com/m/c"], out.Packages)
	}
	// c is depended-upon → ranked first.
	if len(out.Packages) > 0 && out.Packages[0].Package != "example.com/m/c" {
		t.Errorf("ranked first = %s, want example.com/m/c", out.Packages[0].Package)
	}
}
