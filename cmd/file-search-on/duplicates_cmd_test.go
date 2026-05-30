package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestDuplicatesCmd_Run_FindsSha256Duplicates seeds two files with
// identical bytes (and one unique sibling), then confirms the cmd
// surfaces the duplicate group.
func TestDuplicatesCmd_Run_FindsSha256Duplicates(t *testing.T) {
	tmp := t.TempDir()
	body := "same content across two files\n"
	mustWriteFile(t, filepath.Join(tmp, "a.txt"), body)
	mustWriteFile(t, filepath.Join(tmp, "b.txt"), body)
	mustWriteFile(t, filepath.Join(tmp, "different.txt"), "unique\n")

	cmd := &DuplicatesCmd{Dir: []string{tmp}, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["duplicates"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected exactly one duplicate group, got %d: %v", len(groups), groups)
	}
	g, _ := groups[0].(map[string]any)
	paths, _ := g["paths"].([]any)
	if len(paths) != 2 {
		t.Errorf("expected 2 paths in the duplicate group, got %d: %v", len(paths), paths)
	}
}

// TestDuplicatesCmd_Run_NoDuplicates seeds three unique files and
// asserts the cmd reports zero groups (clean exit).
func TestDuplicatesCmd_Run_NoDuplicates(t *testing.T) {
	tmp := t.TempDir()
	for i, body := range []string{"alpha\n", "beta\n", "gamma\n"} {
		mustWriteFile(t, filepath.Join(tmp, "f"+string(rune('a'+i))+".txt"), body)
	}

	cmd := &DuplicatesCmd{Dir: []string{tmp}, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["duplicates"].([]any)
	if len(groups) != 0 {
		t.Errorf("expected zero duplicate groups, got %d: %v", len(groups), groups)
	}
}

// TestDuplicatesCmd_Run_MinSizeFilter confirms the --min-size cap
// excludes small files from the dedup pass.
func TestDuplicatesCmd_Run_MinSizeFilter(t *testing.T) {
	tmp := t.TempDir()
	// Two tiny files with identical content — would dedupe without the floor.
	mustWriteFile(t, filepath.Join(tmp, "tiny-a.txt"), "x")
	mustWriteFile(t, filepath.Join(tmp, "tiny-b.txt"), "x")

	cmd := &DuplicatesCmd{Dir: []string{tmp}, MinSize: 100, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["duplicates"].([]any)
	if len(groups) != 0 {
		t.Errorf("expected zero groups after min-size filter, got %d: %v", len(groups), groups)
	}
}

// TestDuplicatesCmd_Run_CELScopeFilter scopes the walk to a single
// content family via the optional Expr arg.
func TestDuplicatesCmd_Run_CELScopeFilter(t *testing.T) {
	tmp := t.TempDir()
	body := "duplicate content\n"
	// Two identical markdown files — should dedupe.
	mustWriteFile(t, filepath.Join(tmp, "a.md"), body)
	mustWriteFile(t, filepath.Join(tmp, "b.md"), body)
	// Two identical text files — should NOT show under is_markdown filter.
	mustWriteFile(t, filepath.Join(tmp, "a.txt"), body)
	mustWriteFile(t, filepath.Join(tmp, "b.txt"), body)

	cmd := &DuplicatesCmd{Dir: []string{tmp}, Expr: "is_markdown", Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	groups, _ := got["duplicates"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected exactly one markdown-only duplicate group, got %d: %v", len(groups), groups)
	}
}
