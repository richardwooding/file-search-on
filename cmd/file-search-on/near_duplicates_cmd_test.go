package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestNearDuplicatesCmd_Run_FindsSimilar seeds two markdown files
// that share a long stretch of body content but differ by one line
// (SimHash should cluster them) plus an unrelated file.
func TestNearDuplicatesCmd_Run_FindsSimilar(t *testing.T) {
	tmp := t.TempDir()
	shared := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 30)
	mustWriteFile(t, filepath.Join(tmp, "a.md"), shared+"version one\n")
	mustWriteFile(t, filepath.Join(tmp, "b.md"), shared+"version two\n")
	mustWriteFile(t, filepath.Join(tmp, "c.md"), strings.Repeat("completely different prose entirely unrelated\n", 30))

	cmd := &NearDuplicatesCmd{Dir: []string{tmp}, Threshold: 0.85, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["groups"].([]any)
	if len(groups) == 0 {
		t.Fatalf("expected at least one near-duplicate group at threshold 0.85, got 0 (full: %v)", got)
	}
	// First group should contain a.md and b.md.
	g, _ := groups[0].(map[string]any)
	members, _ := g["members"].([]any)
	if len(members) < 2 {
		t.Errorf("expected ≥2 members in the near-dup group, got %d", len(members))
	}
}

// TestNearDuplicatesCmd_Run_DifferentContentNoGroup confirms two
// files with no shared body content don't cluster at any sane
// threshold.
func TestNearDuplicatesCmd_Run_DifferentContentNoGroup(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "a.md"), strings.Repeat("alpha beta gamma delta\n", 40))
	mustWriteFile(t, filepath.Join(tmp, "b.md"), strings.Repeat("zeta eta theta iota kappa lambda mu nu xi\n", 40))

	cmd := &NearDuplicatesCmd{Dir: []string{tmp}, Threshold: 0.85, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["groups"].([]any)
	if len(groups) != 0 {
		t.Errorf("expected zero groups for wholly-different content, got %d: %v", len(groups), groups)
	}
}

// TestNearDuplicatesCmd_Run_SingleFileNoGroup confirms a single
// matched file produces no group (groups need ≥2 members).
func TestNearDuplicatesCmd_Run_SingleFileNoGroup(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "only.md"), "alone here\n")

	cmd := &NearDuplicatesCmd{Dir: []string{tmp}, Threshold: 0.85, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["groups"].([]any)
	if len(groups) != 0 {
		t.Errorf("expected zero groups for a single-file fixture, got %d", len(groups))
	}
}

// TestNearDuplicatesCmd_Run_EmptyDir confirms a clean run with no
// candidates returns valid JSON with zero groups.
func TestNearDuplicatesCmd_Run_EmptyDir(t *testing.T) {
	tmp := t.TempDir() // empty

	cmd := &NearDuplicatesCmd{Dir: []string{tmp}, Threshold: 0.85, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["groups"].([]any)
	if len(groups) != 0 {
		t.Errorf("expected zero groups for empty dir, got %d", len(groups))
	}
}
