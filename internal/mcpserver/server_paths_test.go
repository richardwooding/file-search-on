package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/search"
)

// fakeHome redirects $HOME / %USERPROFILE% to a fresh tmp dir for the
// duration of t. Mirrors what a real MCP host's user environment looks
// like when an agent passes "~/something" — the server resolves it
// against THAT directory.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// TestTildeExpansion_Search exercises the walking-tool shape (Dir +
// Dirs both expand). Sanity-check: a file under $HOME/notes is found
// when the agent passes "~/notes" via Dir, and also when passed via
// Dirs (single-entry slice).
func TestTildeExpansion_Search(t *testing.T) {
	home := fakeHome(t)
	if err := os.MkdirAll(filepath.Join(home, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(home, "notes", "alpha.md"), "# alpha\n")

	ctx, cs := newSession(t)

	t.Run("Dir=~/notes", func(t *testing.T) {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "search",
			Arguments: SearchInput{
				Dir:  "~/notes",
				Expr: "is_markdown",
			},
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool error: %v", res.GetError())
		}
		var out struct {
			Matches []search.Match `json:"matches"`
		}
		mustDecodeStructured(t, res, &out)
		if len(out.Matches) != 1 {
			t.Fatalf("len(matches)=%d want 1; got %+v", len(out.Matches), out.Matches)
		}
		if !strings.HasSuffix(out.Matches[0].Path, "alpha.md") {
			t.Errorf("matched %q want suffix alpha.md", out.Matches[0].Path)
		}
	})

	t.Run("Dirs=[~/notes]", func(t *testing.T) {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "search",
			Arguments: SearchInput{
				Dirs: []string{"~/notes"},
				Expr: "is_markdown",
			},
		})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if res.IsError {
			t.Fatalf("tool error: %v", res.GetError())
		}
		var out struct {
			Matches []search.Match `json:"matches"`
		}
		mustDecodeStructured(t, res, &out)
		if len(out.Matches) != 1 {
			t.Errorf("Dirs=[~/notes]: len(matches)=%d want 1", len(out.Matches))
		}
	})
}

// TestTildeExpansion_Stats covers the same Dir/Dirs shape via the stats
// tool — confirms the wiring is consistent across handlers.
func TestTildeExpansion_Stats(t *testing.T) {
	home := fakeHome(t)
	mustWrite(t, filepath.Join(home, "a.md"), "# a\n")
	mustWrite(t, filepath.Join(home, "b.md"), "# b\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "stats",
		Arguments: StatsInput{
			Dir: "~",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if out.TotalCount != 2 {
		t.Errorf("TotalCount=%d want 2 (bare ~ should expand to home dir)", out.TotalCount)
	}
}

// TestTildeExpansion_ReadAttributes covers the single-path handler
// family (read_attributes). Verifies the Path input is tilde-expanded
// before filepath.Abs.
func TestTildeExpansion_ReadAttributes(t *testing.T) {
	home := fakeHome(t)
	p := filepath.Join(home, "doc.md")
	mustWrite(t, p, "---\ntitle: Hello\n---\n# h\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "read_attributes",
		Arguments: ReadAttributesInput{
			Path: "~/doc.md",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, _ := m["path"].(string); !strings.HasSuffix(got, "doc.md") || !strings.Contains(got, home) {
		t.Errorf("path=%q want suffix doc.md AND prefix %s", got, home)
	}
}

// TestTildeExpansion_DetectProject covers another single-path family
// (detect_project) on a fake go.mod under $HOME.
func TestTildeExpansion_DetectProject(t *testing.T) {
	home := fakeHome(t)
	mustWrite(t, filepath.Join(home, "go.mod"), "module x\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "detect_project",
		Arguments: DetectProjectInput{
			Dir: "~",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool error: %v", res.GetError())
	}
	var out DetectProjectOutput
	mustDecodeStructured(t, res, &out)
	if len(out.ProjectTypes) != 1 || out.ProjectTypes[0] != "go" {
		t.Errorf("ProjectTypes=%+v want [go]", out.ProjectTypes)
	}
}
