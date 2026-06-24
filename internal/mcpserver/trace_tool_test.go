package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTraceTool(t *testing.T) {
	root := t.TempDir()
	mkWrite(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	// caller() calls Target(); Target() calls helper(). So tracing Target
	// should show caller as a caller and helper as a callee.
	mkWrite(t, filepath.Join(root, "main.go"),
		"package main\n\nfunc Target() { helper() }\nfunc helper() {}\nfunc caller() { Target() }\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "trace",
		Arguments: TraceInput{codeGraphWalkInput: codeGraphWalkInput{Dir: root}, Symbol: "Target"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out TraceOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if out.Symbol != "Target" {
		t.Fatalf("symbol = %q, want Target", out.Symbol)
	}
	hasCaller := false
	for _, c := range out.Callers {
		if filepath.Base(c.Path) == "main.go" {
			hasCaller = true
		}
	}
	if !hasCaller {
		t.Errorf("expected main.go among callers of Target; got %+v", out.Callers)
	}
	hasCallee := false
	for _, c := range out.Callees {
		if c == "helper" {
			hasCallee = true
		}
	}
	if !hasCallee {
		t.Errorf("expected 'helper' among callees of Target; got %v", out.Callees)
	}
}
