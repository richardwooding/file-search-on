package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestImpactTool(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "a.go"), "package p\n\n"+
		"func leaf() {}\n"+
		"func mid() { leaf() }\n"+
		"func top() { mid() }\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "impact",
		Arguments: ImpactInput{Symbol: "leaf", codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ImpactOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Symbol != "leaf" || out.Count != 2 || out.MaxDepthReached != 2 {
		t.Fatalf("out = {symbol:%q count:%d max_depth:%d}, want leaf/2/2: %+v", out.Symbol, out.Count, out.MaxDepthReached, out.Dependents)
	}
	depth := map[string]int{}
	for _, d := range out.Dependents {
		depth[d.Symbol] = d.Depth
	}
	if depth["mid"] != 1 || depth["top"] != 2 {
		t.Errorf("dependents = %v, want {mid:1, top:2}", depth)
	}
}

func TestImpactTool_RequiresSymbol(t *testing.T) {
	dir := t.TempDir()
	mkWrite(t, filepath.Join(dir, "a.go"), "package p\n\nfunc x() {}\n")
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "impact",
		Arguments: ImpactInput{codeGraphWalkInput: codeGraphWalkInput{Dir: dir}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected a tool error when symbol is omitted")
	}
}
