package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestFindMatchesTool_Basic verifies a single regex hit lands as
// line-level output with path + line + text populated.
func TestFindMatchesTool_Basic(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n\nfunc main() {\n\t// TODO: ship it\n}\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "find_matches",
		Arguments: FindMatchesInput{
			Dir:     dir,
			Expr:    "is_source",
			Pattern: "TODO",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out FindMatchesOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("Count=%d want 1; matches=%+v", out.Count, out.Matches)
	}
	m := out.Matches[0]
	if m.Line != 4 {
		t.Errorf("Line=%d want 4", m.Line)
	}
	if !strings.Contains(m.Text, "TODO") {
		t.Errorf("Text=%q lacks TODO", m.Text)
	}
	if m.ContentType != "source/go" {
		t.Errorf("ContentType=%q want source/go", m.ContentType)
	}
}

// TestFindMatchesTool_ContextWindows verifies context_before /
// context_after attach the expected surrounding lines.
func TestFindMatchesTool_ContextWindows(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "// L1\n// L2\n// L3\n// MATCH\n// L5\n// L6\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "find_matches",
		Arguments: FindMatchesInput{
			Dir:           dir,
			Expr:          "is_source",
			Pattern:       "MATCH",
			ContextBefore: 2,
			ContextAfter:  2,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out FindMatchesOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("Count=%d want 1", out.Count)
	}
	m := out.Matches[0]
	wantBefore := []string{"// L2", "// L3"}
	if !equalStrSlice(m.Before, wantBefore) {
		t.Errorf("Before=%v want %v", m.Before, wantBefore)
	}
	wantAfter := []string{"// L5", "// L6"}
	if !equalStrSlice(m.After, wantAfter) {
		t.Errorf("After=%v want %v", m.After, wantAfter)
	}
}

// TestFindMatchesTool_EmptyPattern verifies the tool rejects an empty
// pattern with IsError set rather than walking pointlessly.
func TestFindMatchesTool_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "TODO\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "find_matches",
		Arguments: FindMatchesInput{
			Dir:  dir,
			Expr: "is_source",
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport err: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for empty pattern; got false")
	}
}

// TestFindMatchesTool_InvalidRegex verifies a malformed pattern is
// surfaced via IsError, not a silent empty result.
func TestFindMatchesTool_InvalidRegex(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "TODO\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "find_matches",
		Arguments: FindMatchesInput{
			Dir:     dir,
			Pattern: "(unclosed",
		},
	})
	if err != nil {
		t.Fatalf("CallTool transport err: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for bad regex; got false")
	}
}

// TestFindMatchesTool_CELFilterPrunes verifies the CEL expr filters
// candidates before regex scan — only Go files match here.
func TestFindMatchesTool_CELFilterPrunes(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "// TODO go\n")
	mustWrite(t, filepath.Join(dir, "b.py"), "# TODO py\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "find_matches",
		Arguments: FindMatchesInput{
			Dir:     dir,
			Expr:    "is_source && language == \"go\"",
			Pattern: "TODO",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out FindMatchesOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("Count=%d want 1; matches=%+v", out.Count, out.Matches)
	}
	if !strings.HasSuffix(out.Matches[0].Path, "a.go") {
		t.Errorf("matched %q want a.go", out.Matches[0].Path)
	}
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
