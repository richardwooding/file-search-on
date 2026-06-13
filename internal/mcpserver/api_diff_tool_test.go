package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAPIDiffTool(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	mkWrite(t, filepath.Join(a, "api.go"), "package p\n\nfunc Alpha() {}\nfunc Beta() {}\n")
	mkWrite(t, filepath.Join(b, "api.go"), "package p\n\nfunc Alpha() {}\nfunc Gamma() {}\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "api_diff",
		Arguments: APIDiffInput{TreeA: a, TreeB: b},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out APIDiffOutput
	mustDecodeStructured(t, res, &out)

	if out.ServerVersion == "" {
		t.Errorf("server_version not populated")
	}
	if !out.Breaking {
		t.Errorf("Beta removed; expected breaking=true")
	}
	if out.RemovedCount != 1 || out.Removed[0].Symbol != "Beta" {
		t.Errorf("Removed = %+v, want [Beta]", out.Removed)
	}
	if out.AddedCount != 1 || out.Added[0].Symbol != "Gamma" {
		t.Errorf("Added = %+v, want [Gamma]", out.Added)
	}
}

func TestAPIDiffTool_RequiresBothTrees(t *testing.T) {
	a := t.TempDir()
	mkWrite(t, filepath.Join(a, "api.go"), "package p\n\nfunc Alpha() {}\n")
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "api_diff",
		Arguments: APIDiffInput{TreeA: a},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Errorf("expected a tool error when tree_b is omitted")
	}
}
