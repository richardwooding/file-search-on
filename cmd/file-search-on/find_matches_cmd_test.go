package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestFindMatchesCmd_Run_BasicMatch confirms the command surfaces a
// match line via the default grep-style output.
func TestFindMatchesCmd_Run_BasicMatch(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "a.go"), "package main\n\n// TODO: write the code\nfunc main() {}\n")
	mustWriteFile(t, filepath.Join(tmp, "b.go"), "package other\n\nfunc f() {}\n")

	cmd := &FindMatchesCmd{Pattern: `TODO`, Dir: []string{tmp}, Expr: `is_source && language == "go"`, Output: "default", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "TODO: write the code") {
		t.Errorf("expected match line in output, got %q", out)
	}
	if strings.Contains(out, "b.go") {
		t.Errorf("expected only a.go to match, found b.go in output %q", out)
	}
}

// TestFindMatchesCmd_Run_NoMatch confirms a clean run with zero
// matches still produces valid output, and signals "no match" via
// grep-style exit code 1 (the documented convention).
func TestFindMatchesCmd_Run_NoMatch(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "f.go"), "package main\n\nfunc main() {}\n")

	cmd := &FindMatchesCmd{Pattern: `XXXNEVERMATCH`, Dir: []string{tmp}, Output: "json", NoIndex: true}
	out, runErr := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	// runErr is non-nil for no-match (grep convention: exit 1); the
	// JSON output still lands on stdout regardless.
	if runErr == nil {
		t.Errorf("expected non-nil error for no-match (grep convention), got nil")
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	matches, _ := got["matches"].([]any)
	if len(matches) != 0 {
		t.Errorf("expected zero matches for pattern that doesn't match anything, got %d", len(matches))
	}
}

// TestFindMatchesCmd_Run_ContextWindow exercises the -C shortcut
// and asserts before/after lines land in the output.
func TestFindMatchesCmd_Run_ContextWindow(t *testing.T) {
	tmp := t.TempDir()
	body := "line one\nline two\nMATCH HERE\nline four\nline five\n"
	mustWriteFile(t, filepath.Join(tmp, "f.txt"), body)

	cmd := &FindMatchesCmd{Pattern: `MATCH`, Dir: []string{tmp}, Context: 1, Output: "default", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Default grep-style output uses "-" for context lines, ":" for match line.
	if !strings.Contains(out, "line two") {
		t.Errorf("expected context-before line 'line two' in output, got %q", out)
	}
	if !strings.Contains(out, "line four") {
		t.Errorf("expected context-after line 'line four' in output, got %q", out)
	}
	if !strings.Contains(out, "MATCH HERE") {
		t.Errorf("expected match line 'MATCH HERE' in output, got %q", out)
	}
}

// TestFindMatchesCmd_Run_CELScopeFilter confirms the CEL pre-prune
// narrows the candidate set before the regex scan fires.
func TestFindMatchesCmd_Run_CELScopeFilter(t *testing.T) {
	tmp := t.TempDir()
	mustWriteFile(t, filepath.Join(tmp, "go-side.go"), "// keyword\npackage main\n")
	mustWriteFile(t, filepath.Join(tmp, "text-side.txt"), "keyword in text\n")

	// Restrict to Go source — the .txt file should be invisible.
	cmd := &FindMatchesCmd{Pattern: `keyword`, Dir: []string{tmp}, Expr: `is_source && language == "go"`, Output: "json", NoIndex: true}
	out, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var got map[string]any
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&got); err != nil {
		t.Fatalf("decode JSON: %v\nraw: %q", err, out)
	}
	matches, _ := got["matches"].([]any)
	for _, m := range matches {
		mm, _ := m.(map[string]any)
		path, _ := mm["path"].(string)
		if strings.HasSuffix(path, ".txt") {
			t.Errorf("expected CEL filter to exclude .txt files, got match in %q", path)
		}
	}
}
