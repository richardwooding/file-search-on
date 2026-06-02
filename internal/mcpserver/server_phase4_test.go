package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStatsTool_GroupByLanguage verifies the group_by input flows
// through to ComputeStats and the Groups field is populated.
func TestStatsTool_GroupByLanguage(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.go"), "package main\n")
	mustWrite(t, filepath.Join(dir, "b.go"), "package main\n")
	mustWrite(t, filepath.Join(dir, "c.py"), "print(1)\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "stats",
		Arguments: StatsInput{
			Dir:     dir,
			Expr:    "is_source",
			GroupBy: "language",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if out.GroupBy != "language" {
		t.Errorf("GroupBy=%q want language", out.GroupBy)
	}
	if len(out.ContentTypes) != 0 {
		t.Errorf("ContentTypes should be empty when group_by!=content_type; got %v", out.ContentTypes)
	}
	byName := map[string]int64{}
	for _, b := range out.Groups {
		byName[b.Name] = b.Count
	}
	if byName["go"] != 2 || byName["python"] != 1 {
		t.Errorf("Groups histogram wrong: %+v", out.Groups)
	}
}

// TestStatsTool_DefaultGroupByBackCompat: when group_by is unset,
// both Groups and ContentTypes are populated — older agents that
// hard-coded `content_types[]` continue to work.
func TestStatsTool_DefaultGroupByBackCompat(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# a\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "stats",
		Arguments: StatsInput{Dir: dir},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out StatsOutput
	mustDecodeStructured(t, res, &out)
	if len(out.Groups) == 0 {
		t.Fatal("Groups empty")
	}
	if len(out.ContentTypes) == 0 {
		t.Fatal("ContentTypes empty; back-compat regression")
	}
}

// TestSearchTool_Dirs verifies multi-dir aggregation via the
// search tool's `dirs` field.
func TestSearchTool_Dirs(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	mustWrite(t, filepath.Join(dirA, "a.md"), "# a\n")
	mustWrite(t, filepath.Join(dirB, "b.md"), "# b\n")

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "search",
		Arguments: SearchInput{
			Dirs: []string{dirA, dirB},
			Expr: "is_markdown",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.Count != 2 {
		t.Fatalf("Count=%d want 2 (aggregated across two dirs); matches=%+v", out.Count, out.Matches)
	}
}

// TestReadLinesTool verifies the new read_lines tool returns the
// requested range with truncated/start/end/total fields populated.
func TestReadLinesTool(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		b.WriteString("line ")
		b.WriteString(itoaForTest(i))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "read_lines",
		Arguments: ReadLinesInput{
			Path:      p,
			StartLine: 3,
			EndLine:   5,
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out ReadLinesOutput
	mustDecodeStructured(t, res, &out)
	if len(out.Lines) != 3 {
		t.Fatalf("len(Lines)=%d want 3", len(out.Lines))
	}
	if out.Lines[0] != "line 3" || out.Lines[2] != "line 5" {
		t.Errorf("got Lines=%v", out.Lines)
	}
	if out.TotalLines != 10 {
		t.Errorf("TotalLines=%d want 10", out.TotalLines)
	}
}

// TestReadLinesTool_MissingPath errors cleanly.
func TestReadLinesTool_MissingPath(t *testing.T) {
	ctx, cs := newSession(t)
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_lines",
		Arguments: ReadLinesInput{},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError=true on empty path")
	}
}

// itoaForTest avoids reaching for strconv in a test file that
// already has enough imports — keeps the test focused on the MCP
// contract under inspection.
func itoaForTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestStatsTool_PruneBuildArtefacts confirms the new input plumbs
// through to the walker, parity-fixing the gap dogfooding surfaced:
// stats over a Go module would over-count by walking ./vendor. With
// prune_build_artefacts=true the vendor tree is excluded the same
// way the search tool already does it. Issue #277.
func TestStatsTool_PruneBuildArtefacts(t *testing.T) {
	dir := t.TempDir()
	// Marker file makes the dir a Go module — the project-type
	// detector then knows "vendor" is a Go build artefact and adds
	// it to the prune list.
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/foo\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\n")
	// Synthesise a vendor dir with two more .go files.
	vendorDir := filepath.Join(dir, "vendor", "github.com", "x", "y")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatalf("mkdir vendor: %v", err)
	}
	mustWrite(t, filepath.Join(vendorDir, "a.go"), "package y\n")
	mustWrite(t, filepath.Join(vendorDir, "b.go"), "package y\n")

	ctx, cs := newSession(t)

	// Baseline: prune_build_artefacts=false counts all 3 Go files.
	resAll, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "stats",
		Arguments: StatsInput{
			Dir:     dir,
			Expr:    "is_source && language == \"go\"",
			GroupBy: "language",
		},
	})
	if err != nil {
		t.Fatalf("baseline CallTool: %v", err)
	}
	var outAll StatsOutput
	mustDecodeStructured(t, resAll, &outAll)
	var allGo int64
	for _, b := range outAll.Groups {
		if b.Name == "go" {
			allGo = b.Count
		}
	}
	if allGo != 3 {
		t.Fatalf("baseline go count = %d, want 3 (main.go + 2 vendored)", allGo)
	}

	// With prune_build_artefacts=true the vendor subtree is pruned;
	// only main.go survives.
	resPruned, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "stats",
		Arguments: StatsInput{
			Dir:                 dir,
			Expr:                "is_source && language == \"go\"",
			GroupBy:             "language",
			PruneBuildArtefacts: true,
		},
	})
	if err != nil {
		t.Fatalf("pruned CallTool: %v", err)
	}
	var outPruned StatsOutput
	mustDecodeStructured(t, resPruned, &outPruned)
	var prunedGo int64
	for _, b := range outPruned.Groups {
		if b.Name == "go" {
			prunedGo = b.Count
		}
	}
	if prunedGo != 1 {
		t.Errorf("pruned go count = %d, want 1 (vendor pruned)", prunedGo)
	}
}
