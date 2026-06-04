package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSearchCmd_FormatStreamNoBogusFooter is the regression for issue
// #313: a custom --format template streams its matches, and the
// "N file(s) found" footer must NOT be printed for it (it's scripting
// output like --o bare / json). Previously the un-counted template
// branch left the counter at 0 and the footer printed a bogus
// "0 file(s) found" despite real matches.
func TestSearchCmd_FormatStreamNoBogusFooter(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# A\n\nalpha alpha alpha\n")
	mustWriteFile(t, filepath.Join(dir, "b.md"), "# B\n\nbeta beta beta\n")

	cmd := &SearchCmd{
		Dir:     []string{dir},
		Expr:    "is_markdown",
		Format:  "{{.Path}}",
		Output:  "default",
		NoIndex: true,
	}
	stdout, stderr := runCapturingBoth(t, func() error { return cmd.Run(t.Context()) })

	// Both files were emitted via the template...
	if !strings.Contains(stdout, "a.md") || !strings.Contains(stdout, "b.md") {
		t.Errorf("template output missing matches:\n%s", stdout)
	}
	// ...but no footer (and definitely not the bogus 0).
	if strings.Contains(stderr, "file(s) found") {
		t.Errorf("--format must not print a count footer (#313); stderr:\n%s", stderr)
	}
}

// TestSearchCmd_UnsortedStreamFooterCounts is the positive control: the
// streaming default-mode path (reached via --unsorted) still emits the
// correct count, proving the #313 fix only suppressed the template case.
func TestSearchCmd_UnsortedStreamFooterCounts(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "a.md"), "# A\n\nalpha\n")
	mustWriteFile(t, filepath.Join(dir, "b.md"), "# B\n\nbeta\n")

	cmd := &SearchCmd{
		Dir:      []string{dir},
		Expr:     "is_markdown",
		Output:   "default",
		Unsorted: true,
		NoIndex:  true,
	}
	_, stderr := runCapturingBoth(t, func() error { return cmd.Run(t.Context()) })
	if !strings.Contains(stderr, "2 file(s) found") {
		t.Errorf("streaming default footer should report the count; stderr:\n%s", stderr)
	}
}

// runCapturingBoth captures stdout and stderr for fn, fatally failing on
// a run error. Nests the existing single-stream capture helpers.
func runCapturingBoth(t *testing.T, fn func() error) (stdout, stderr string) {
	t.Helper()
	stderr, err := captureStderr(t, func() error {
		var e error
		stdout, e = captureStdout(t, fn)
		return e
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return stdout, stderr
}
