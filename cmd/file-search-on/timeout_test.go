package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLITimeoutExitCode builds the binary, walks a directory under
// an absurdly tight --timeout, and asserts:
//   - the process exits with code 124 (matches GNU `timeout`)
//   - stderr contains the "timed out" warning
//   - stdout is the partial result set (could be empty on very fast
//     hardware; we only check there's no crash output)
//
// This is the acceptance test for the partial-results UX from the
// shell side.
func TestCLITimeoutExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI build/exec test in -short mode")
	}

	bin := filepath.Join(t.TempDir(), "fso")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Materialise enough markdown files that the walk has work to do.
	tree := t.TempDir()
	for _, n := range []string{"a.md", "b.md", "c.md", "d.md", "e.md"} {
		mustWriteFile(t, filepath.Join(tree, n), "# h\nbody body\n")
	}

	cmd := exec.Command(bin, "is_markdown", "-d", tree, "--timeout", "1ns", "-o", "bare")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit on --timeout 1ns; got 0 with output %q", out)
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if got := exitErr.ExitCode(); got != 124 {
		t.Errorf("exit code = %d, want 124 (GNU `timeout` convention); output=%q", got, out)
	}
	if !strings.Contains(string(out), "timed out") {
		t.Errorf("stderr should mention \"timed out\"; got %q", out)
	}
}

// TestCLITimeoutNoFlagNoTimeout makes sure the absence of --timeout
// preserves the historical behaviour: the walk runs to completion,
// exits 0, no timeout warning anywhere.
func TestCLITimeoutNoFlagNoTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI build/exec test in -short mode")
	}

	bin := filepath.Join(t.TempDir(), "fso")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	tree := t.TempDir()
	mustWriteFile(t, filepath.Join(tree, "x.md"), "# h\n")

	cmd := exec.Command(bin, "is_markdown", "-d", tree, "-o", "bare")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("unexpected non-zero exit: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "timed out") {
		t.Errorf("output mentions timeout but --timeout was unset: %q", out)
	}
}
