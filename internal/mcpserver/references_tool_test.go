package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReferencesTool(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "a.go"), "package p\n\nfunc Target() {}\n")
	mkWrite(t, filepath.Join(dir, "b.go"), "package p\n\nfunc caller() { Target() }\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "references",
		Arguments: ReferencesInput{Symbol: "Target", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ReferencesOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Count != 1 || len(out.References) != 1 {
		t.Fatalf("expected 1 reference to Target, got %+v", out.References)
	}
	r := out.References[0]
	if filepath.Base(r.Path) != "b.go" || r.Kind != "call" || r.Line != 3 {
		t.Errorf("reference = %+v, want b.go:3 call", r)
	}
}

func TestReferencesTool_RequiresSymbol(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "a.go"), "package p\n\nfunc F() {}\n")
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "references",
		Arguments: ReferencesInput{codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected a tool error when symbol is omitted")
	}
}
