package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestFindDuplicatesTool_Basic verifies the find_duplicates tool
// groups byte-identical files and exposes wasted_bytes correctly.
func TestFindDuplicatesTool_Basic(t *testing.T) {
	dir := t.TempDir()
	body := strings.Repeat("x", 256)
	mustWrite(t, filepath.Join(dir, "a.txt"), body)
	mustWrite(t, filepath.Join(dir, "b.txt"), body)
	mustWrite(t, filepath.Join(dir, "c.txt"), "unique\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "find_duplicates",
		Arguments: FindDuplicatesInput{
			Dir: dir,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out FindDuplicatesOutput
	mustDecodeStructured(t, res, &out)
	if out.DuplicateGroups != 1 {
		t.Fatalf("DuplicateGroups=%d want 1; %+v", out.DuplicateGroups, out.Duplicates)
	}
	if out.WastedBytes != 256 {
		t.Errorf("WastedBytes=%d want 256", out.WastedBytes)
	}
	if out.Duplicates[0].Count != 2 {
		t.Errorf("group Count=%d want 2", out.Duplicates[0].Count)
	}
}

// TestFindDuplicatesTool_MinSize verifies min_size filters small files.
func TestFindDuplicatesTool_MinSize(t *testing.T) {
	dir := t.TempDir()
	small := "x\n"
	big := strings.Repeat("y", 200)
	mustWrite(t, filepath.Join(dir, "small-a.txt"), small)
	mustWrite(t, filepath.Join(dir, "small-b.txt"), small)
	mustWrite(t, filepath.Join(dir, "big-a.txt"), big)
	mustWrite(t, filepath.Join(dir, "big-b.txt"), big)

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "find_duplicates",
		Arguments: FindDuplicatesInput{
			Dir:     dir,
			MinSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out FindDuplicatesOutput
	mustDecodeStructured(t, res, &out)
	if out.DuplicateGroups != 1 {
		t.Fatalf("DuplicateGroups=%d want 1 (only the big pair); %+v", out.DuplicateGroups, out.Duplicates)
	}
}

// TestStatsTool_GroupByMTimeMonth verifies the new time-bucket
// group_by keys land cleanly via the MCP tool.
func TestStatsTool_GroupByMTimeMonth(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []struct {
		name string
		mt   time.Time
	}{
		{"a.md", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"b.md", time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)},
		{"c.md", time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, []byte("# h\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, f.mt, f.mt); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "stats",
		Arguments: StatsInput{
			Dir:     dir,
			Expr:    "is_markdown",
			GroupBy: "mtime_month",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if out.GroupBy != "mtime_month" {
		t.Errorf("GroupBy=%q want mtime_month", out.GroupBy)
	}
	byName := map[string]int64{}
	for _, b := range out.Groups {
		byName[b.Name] = b.Count
	}
	if byName["2024-03"] != 2 {
		t.Errorf("2024-03 count=%d want 2", byName["2024-03"])
	}
	if byName["2025-01"] != 1 {
		t.Errorf("2025-01 count=%d want 1", byName["2025-01"])
	}
}
