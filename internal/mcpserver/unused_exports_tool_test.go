package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUnusedExportsTool(t *testing.T) {
	root := t.TempDir()
	mkWrite(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	mkWrite(t, filepath.Join(root, "a", "a.go"), "package a\n\n"+
		"type LocalOnly struct{}\n"+
		"type CrossUsed struct{}\n")
	mkWrite(t, filepath.Join(root, "a", "a2.go"), "package a\n\n"+
		"func consume() { _ = LocalOnly{} }\n")
	mkWrite(t, filepath.Join(root, "b", "b.go"), "package b\n\n"+
		"import \"example.com/m/a\"\n\nfunc B() { _ = a.CrossUsed{} }\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "unused_exports",
		Arguments: UnusedExportsInput{codeGraphWalkInput: codeGraphWalkInput{Dir: root}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out UnusedExportsOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Module != "example.com/m" {
		t.Fatalf("module = %q, want example.com/m", out.Module)
	}
	got := map[string]bool{}
	for _, c := range out.Candidates {
		got[c.Symbol] = true
	}
	if !got["LocalOnly"] {
		t.Errorf("LocalOnly should be flagged (intra-package only): %+v", out.Candidates)
	}
	if got["CrossUsed"] {
		t.Errorf("CrossUsed must NOT be flagged (used by package b): %+v", out.Candidates)
	}
}
