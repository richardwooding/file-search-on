package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestListPresetsTool verifies the catalog round-trips through the
// MCP tool — every preset has a non-empty name + description.
func TestListPresetsTool(t *testing.T) {
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_presets",
		Arguments: ListPresetsInput{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out ListPresetsOutput
	mustDecodeStructured(t, res, &out)
	if len(out.Presets) == 0 {
		t.Fatal("expected at least one preset, got 0")
	}

	for _, p := range out.Presets {
		if p.Name == "" {
			t.Errorf("preset with empty name: %+v", p)
		}
		if p.Description == "" {
			t.Errorf("preset %q has empty description", p.Name)
		}
	}
}

// TestQueryPresetTool runs the `system_metadata` preset against a
// fixture containing a fake .DS_Store and confirms the preset's
// CEL filter matches.
func TestQueryPresetTool(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, ".DS_Store"), "")
	mustWrite(t, filepath.Join(dir, "regular.txt"), "not metadata")

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "query_preset",
		Arguments: QueryPresetInput{
			Name: "system_metadata",
			Dir:  dir,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 1 {
		t.Fatalf("expected 1 system_metadata match, got %d (%+v)", out.Count, out.Matches)
	}
	if got := filepath.Base(out.Matches[0].Path); got != ".DS_Store" {
		t.Errorf("expected .DS_Store, got %s", got)
	}
}

// TestQueryPresetTool_UnknownPresetErrors confirms missing preset
// names return a tool error rather than silently degrading.
func TestQueryPresetTool_UnknownPresetErrors(t *testing.T) {
	dir := t.TempDir()

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "query_preset",
		Arguments: QueryPresetInput{
			Name: "definitely-not-a-preset",
			Dir:  dir,
		},
	})
	if err != nil {
		t.Fatalf("transport CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true for unknown preset, got false")
	}
}

// TestQueryPresetTool_LimitOverride confirms the per-call Limit
// input overrides the preset's default cap. We use Thumbs.db /
// Desktop.ini / .DS_Store / .localized / .directory — five distinct
// is_system_metadata content types — so all match the preset
// without needing subdirectories.
func TestQueryPresetTool_LimitOverride(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".DS_Store", ".localized", "Thumbs.db", "Desktop.ini", ".directory"} {
		mustWrite(t, filepath.Join(dir, name), "")
	}

	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "query_preset",
		Arguments: QueryPresetInput{
			Name:  "system_metadata",
			Dir:   dir,
			Limit: 2,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 2 {
		t.Errorf("expected limit=2 to cap matches, got %d", out.Count)
	}
}
