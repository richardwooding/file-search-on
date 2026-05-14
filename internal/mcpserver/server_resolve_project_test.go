package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestResolveProjectForPathTool_GoModule verifies the walk-up
// behaviour: a file under cmd/ resolves to the project root that
// holds go.mod.
func TestResolveProjectForPathTool_GoModule(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "go.mod"), "module x\n")
	mainGo := filepath.Join(root, "cmd", "main.go")
	mustWrite(t, mainGo, "package main\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_project_for_path",
		Arguments: ResolveProjectForPathInput{
			Path: mainGo,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ResolveProjectForPathOutput
	mustDecodeStructured(t, res, &out)
	if out.ProjectRoot != root {
		t.Errorf("ProjectRoot=%q want %q", out.ProjectRoot, root)
	}
	if len(out.ProjectTypes) != 1 || out.ProjectTypes[0] != "go" {
		t.Errorf("ProjectTypes=%+v want [go]", out.ProjectTypes)
	}
	if out.Path != mainGo {
		t.Errorf("Path=%q want %q", out.Path, mainGo)
	}
	if len(out.Indicators) == 0 {
		t.Errorf("Indicators empty; expected go.mod entry")
	}
}

// TestResolveProjectForPathTool_NoProject verifies an "outside any
// project" lookup returns empty project_types without erroring.
func TestResolveProjectForPathTool_NoProject(t *testing.T) {
	root := t.TempDir()
	loose := filepath.Join(root, "loose.txt")
	mustWrite(t, loose, "")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_project_for_path",
		Arguments: ResolveProjectForPathInput{
			Path: loose,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ResolveProjectForPathOutput
	mustDecodeStructured(t, res, &out)
	if out.ProjectRoot != "" {
		t.Errorf("ProjectRoot=%q want empty (no enclosing project)", out.ProjectRoot)
	}
	if len(out.ProjectTypes) != 0 {
		t.Errorf("ProjectTypes=%+v want empty", out.ProjectTypes)
	}
}

// TestResolveProjectForPathTool_PolyglotDir verifies a directory
// firing multiple types surfaces both in project_types.
func TestResolveProjectForPathTool_PolyglotDir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(root, "docker-compose.yml"), "services: {}\n")
	mainGo := filepath.Join(root, "cmd", "main.go")
	mustWrite(t, mainGo, "package main\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "resolve_project_for_path",
		Arguments: ResolveProjectForPathInput{
			Path: mainGo,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out ResolveProjectForPathOutput
	mustDecodeStructured(t, res, &out)
	if out.ProjectRoot != root {
		t.Errorf("ProjectRoot=%q want %q", out.ProjectRoot, root)
	}
	have := map[string]bool{}
	for _, t := range out.ProjectTypes {
		have[t] = true
	}
	if !have["go"] || !have["docker-compose"] {
		t.Errorf("ProjectTypes=%+v should include both go and docker-compose", out.ProjectTypes)
	}
}

// TestResolveProjectForPathTool_MissingPath verifies the tool errors
// rather than walking from the working directory when path is empty.
func TestResolveProjectForPathTool_MissingPath(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "resolve_project_for_path",
		Arguments: ResolveProjectForPathInput{},
	})
	if err != nil {
		t.Fatalf("CallTool transport: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for empty path; got false")
	}
	var errText strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			errText.WriteString(tc.Text)
		}
	}
	if !strings.Contains(errText.String(), "path") {
		t.Errorf("error %q should mention 'path'", errText.String())
	}
}
