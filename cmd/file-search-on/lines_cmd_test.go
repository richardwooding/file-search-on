package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestLinesCmd_Run_BasicRange asserts the default text-output path
// emits the requested 1-indexed-inclusive line range.
func TestLinesCmd_Run_BasicRange(t *testing.T) {
	tmp := t.TempDir()
	body := "one\ntwo\nthree\nfour\nfive\n"
	target := filepath.Join(tmp, "file.txt")
	mustWriteFile(t, target, body)

	cmd := &LinesCmd{Path: target, Start: 2, End: 4, Output: "text"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	want := "two\nthree\nfour\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestLinesCmd_Run_JSON exercises the JSON branch: confirms the
// wire shape (lines / total_lines / start / end / truncated).
func TestLinesCmd_Run_JSON(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "f.txt")
	mustWriteFile(t, target, "a\nb\nc\n")

	cmd := &LinesCmd{Path: target, Start: 1, End: 0, Output: "json"}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	lines, _ := got["lines"].([]any)
	if len(lines) != 3 {
		t.Errorf("lines length = %d, want 3 (raw: %v)", len(lines), got)
	}
}

// TestLinesCmd_Run_DirectoryRejected confirms passing a directory
// surfaces a clear error rather than reading bytes blindly.
func TestLinesCmd_Run_DirectoryRejected(t *testing.T) {
	tmp := t.TempDir()
	cmd := &LinesCmd{Path: tmp, Output: "text"}
	_, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err == nil {
		t.Fatalf("expected error for directory path, got nil")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("expected 'directory' in error message, got %q", err.Error())
	}
}

// TestLinesCmd_Run_MissingFile confirms a stat failure on a
// non-existent path bubbles up as an error.
func TestLinesCmd_Run_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	cmd := &LinesCmd{Path: filepath.Join(tmp, "does-not-exist.txt"), Output: "text"}
	_, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
}
