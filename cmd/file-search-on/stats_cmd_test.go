package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatsCmd_Run_GroupByContentType builds a small tree with two
// distinct content types and confirms the histogram surfaces both.
func TestStatsCmd_Run_GroupByContentType(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "doc.md"), "# title\n\nbody\n")
	mustWriteFile(t, filepath.Join(tmp, "code.go"), "package main\n\nfunc main(){}\n")
	mustWriteFile(t, filepath.Join(tmp, "data.json"), `{"k":"v"}`)

	cmd := &StatsCmd{Dir: []string{tmp}, GroupBy: "content_type", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["groups"].([]any)
	if len(groups) < 3 {
		t.Errorf("expected at least 3 content-type groups, got %d: %v", len(groups), groups)
	}
}

// TestStatsCmd_Run_TotalCount verifies the aggregate count + size
// roll-ups land correctly for a known fixture.
func TestStatsCmd_Run_TotalCount(t *testing.T) {
	tmp := t.TempDir()
	for i, body := range []string{"alpha\n", "beta\n", "gamma\n"} {
		mustWriteFile(t, filepath.Join(tmp, "f"+string(rune('a'+i))+".txt"), body)
	}

	cmd := &StatsCmd{Dir: []string{tmp}, GroupBy: "content_type", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	totalCount, _ := got["total_count"].(float64)
	if int(totalCount) != 3 {
		t.Errorf("total_count = %v, want 3", totalCount)
	}
}

// TestStatsCmd_Run_GroupByExt exercises a non-default group_by — the
// bucketing-by-key code path that's different from content_type.
func TestStatsCmd_Run_GroupByExt(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "a.md"), "x\n")
	mustWriteFile(t, filepath.Join(tmp, "b.md"), "y\n")
	mustWriteFile(t, filepath.Join(tmp, "c.txt"), "z\n")

	cmd := &StatsCmd{Dir: []string{tmp}, GroupBy: "ext", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["groups"].([]any)
	// Expect one bucket for .md (count 2) and one for .txt (count 1).
	var mdCount, txtCount int
	for _, g := range groups {
		gg, _ := g.(map[string]any)
		name, _ := gg["name"].(string)
		cnt, _ := gg["count"].(float64)
		switch name {
		case ".md":
			mdCount = int(cnt)
		case ".txt":
			txtCount = int(cnt)
		}
	}
	if mdCount != 2 {
		t.Errorf(".md count = %d, want 2", mdCount)
	}
	if txtCount != 1 {
		t.Errorf(".txt count = %d, want 1", txtCount)
	}
}

// TestStatsCmd_Run_CELFilter pre-prunes the walk to markdown only
// and asserts non-matching files don't land in any group.
func TestStatsCmd_Run_CELFilter(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "doc.md"), "# md\n")
	mustWriteFile(t, filepath.Join(tmp, "code.go"), "package main\n")

	cmd := &StatsCmd{Dir: []string{tmp}, Expr: "is_markdown", GroupBy: "content_type", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	totalCount, _ := got["total_count"].(float64)
	if int(totalCount) != 1 {
		t.Errorf("CEL-filtered total_count = %v, want 1 (only the markdown)", totalCount)
	}
}

// TestStatsCmd_Run_EmptyDir confirms a walk over an empty dir
// returns a well-formed JSON with zero groups.
func TestStatsCmd_Run_EmptyDir(t *testing.T) {
	tmp := t.TempDir() // empty

	cmd := &StatsCmd{Dir: []string{tmp}, GroupBy: "content_type", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	totalCount, _ := got["total_count"].(float64)
	if int(totalCount) != 0 {
		t.Errorf("empty dir total_count = %v, want 0", totalCount)
	}
}

// TestStatsCmd_Run_MultipleDirs exercises the multi-root path —
// stats aggregates across all -d roots into a single histogram.
func TestStatsCmd_Run_MultipleDirs(t *testing.T) {
	tmp1, tmp2 := t.TempDir(), t.TempDir()
	mustWriteFile(t, filepath.Join(tmp1, "a.md"), "x\n")
	mustWriteFile(t, filepath.Join(tmp2, "b.md"), "y\n")

	cmd := &StatsCmd{Dir: []string{tmp1, tmp2}, GroupBy: "content_type", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	totalCount, _ := got["total_count"].(float64)
	if int(totalCount) != 2 {
		t.Errorf("multi-dir total_count = %v, want 2", totalCount)
	}
}
