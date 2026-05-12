package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDetectProjectTool(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(dir, "docker-compose.yml"), "services:\n  web:\n    image: nginx\n")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "detect_project",
		Arguments: DetectProjectInput{Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}
	var out DetectProjectOutput
	mustDecodeStructured(t, res, &out)
	if len(out.ProjectTypes) != 2 {
		t.Fatalf("got %d types, want 2 (go + docker-compose); types=%v", len(out.ProjectTypes), out.ProjectTypes)
	}
	// projecttype.Detect sorts by Name: docker-compose < go.
	if out.ProjectTypes[0] != "docker-compose" || out.ProjectTypes[1] != "go" {
		t.Errorf("project_types = %v, want [docker-compose, go]", out.ProjectTypes)
	}
}

func TestFindProjectsTool(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "go-app"))
	mustMkdirAll(t, filepath.Join(root, "rust-app"))
	mustMkdirAll(t, filepath.Join(root, "skip-me"))
	mustWrite(t, filepath.Join(root, "go-app", "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(root, "rust-app", "Cargo.toml"), "[package]\nname = \"x\"\n")
	mustWrite(t, filepath.Join(root, "skip-me", "random.txt"), "x")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_projects",
		Arguments: FindProjectsInput{Dir: root},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}
	var out FindProjectsOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 2 {
		t.Fatalf("got %d projects, want 2; projects=%+v", out.Count, out.Projects)
	}

	// Types filter narrows to one project.
	res2, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "find_projects",
		Arguments: FindProjectsInput{Dir: root, Types: []string{"go"}},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out2 FindProjectsOutput
	mustDecodeStructured(t, res2, &out2)
	if out2.Count != 1 {
		t.Fatalf("filtered: got %d, want 1; projects=%+v", out2.Count, out2.Projects)
	}
	if out2.Projects[0].Types[0].Type != "go" {
		t.Errorf("filtered: type = %q, want go", out2.Projects[0].Types[0].Type)
	}
}
