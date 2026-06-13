package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCallPathTool(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "a.go"), "package p\n\n"+
		"func a() { b() }\n"+
		"func b() { c() }\n"+
		"func c() {}\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "call_path",
		Arguments: CallPathInput{From: "a", To: "c", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out CallPathOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if !out.Reachable || out.Length != 2 || len(out.Path) != 3 {
		t.Fatalf("out = {reachable:%v length:%d steps:%d}, want true/2/3: %+v", out.Reachable, out.Length, len(out.Path), out.Path)
	}
	if out.Path[0].Symbol != "a" || out.Path[2].Symbol != "c" {
		t.Errorf("path endpoints = %s..%s, want a..c", out.Path[0].Symbol, out.Path[2].Symbol)
	}
}

func TestCallPathTool_RequiresFromAndTo(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "a.go"), "package p\n\nfunc a() {}\n")
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "call_path",
		Arguments: CallPathInput{From: "a", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected a tool error when 'to' is omitted")
	}
}
