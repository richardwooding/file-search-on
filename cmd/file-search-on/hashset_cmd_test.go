package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestHashSetBuildCmd_Run_FromText compiles a small text hashlist
// into a bbolt file and confirms the count summary lands on stdout.
func TestHashSetBuildCmd_Run_FromText(t *testing.T) {
	tmp := t.TempDir()
	in := filepath.Join(tmp, "list.txt")
	// Three sha256s (64 hex chars each — length-based auto-detection).
	mustWriteFile(t, in,
		"d2d2c790271471dee54c81a0d4f72f48d9b1b1c54c1f6f8a5c8e2e6cbdf09c5b\n"+
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\n"+
			"# a comment line\n"+
			"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08\n")

	out := filepath.Join(tmp, "out.db")
	cmd := &HashSetBuildCmd{From: in, Out: out, Format: "text", Quiet: true}
	stdout, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout, "sha256: 3") {
		t.Errorf("expected 'sha256: 3' in stdout, got %q", stdout)
	}
}

// TestHashSetBuildCmd_Run_MissingInput surfaces an error when the
// input file doesn't exist.
func TestHashSetBuildCmd_Run_MissingInput(t *testing.T) {
	tmp := t.TempDir()
	cmd := &HashSetBuildCmd{
		From:   filepath.Join(tmp, "does-not-exist.txt"),
		Out:    filepath.Join(tmp, "out.db"),
		Format: "text",
		Quiet:  true,
	}
	_, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err == nil {
		t.Fatalf("expected error for missing input, got nil")
	}
	if !strings.Contains(err.Error(), "open input") {
		t.Errorf("expected 'open input' in error, got %q", err.Error())
	}
}

// TestHashSetInfoCmd_Run_ReportsCounts builds a bbolt hashset then
// reads its counts back via the `info` subcommand.
func TestHashSetInfoCmd_Run_ReportsCounts(t *testing.T) {
	tmp := t.TempDir()
	in := filepath.Join(tmp, "list.txt")
	// Two sha1s (40 hex chars each) and one md5 (32 hex chars).
	mustWriteFile(t, in,
		"da39a3ee5e6b4b0d3255bfef95601890afd80709\n"+
			"a94a8fef8c91167b18b8e1c9a02d1b4e7d72a3a3\n"+
			"d41d8cd98f00b204e9800998ecf8427e\n")

	bolt := filepath.Join(tmp, "out.db")
	buildCmd := &HashSetBuildCmd{From: in, Out: bolt, Format: "text", Quiet: true}
	if _, err := captureStdout(t, func() error { return buildCmd.Run(t.Context()) }); err != nil {
		t.Fatalf("build: %v", err)
	}

	infoCmd := &HashSetInfoCmd{Path: bolt}
	stdout, err := captureStdout(t, func() error { return infoCmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("info run: %v", err)
	}
	if !strings.Contains(stdout, "sha1:   2") {
		t.Errorf("expected 'sha1:   2' in info output, got %q", stdout)
	}
	if !strings.Contains(stdout, "md5:    1") {
		t.Errorf("expected 'md5:    1' in info output, got %q", stdout)
	}
	if !strings.Contains(stdout, "total:  3") {
		t.Errorf("expected 'total:  3' in info output, got %q", stdout)
	}
}

// TestHashSetInfoCmd_Run_ReadsTextFile confirms `info` also accepts
// a plain-text hashlist (not just a built bbolt file) — the
// HashSet.Open helper handles both formats transparently.
func TestHashSetInfoCmd_Run_ReadsTextFile(t *testing.T) {
	tmp := t.TempDir()
	in := filepath.Join(tmp, "list.txt")
	mustWriteFile(t, in, "d41d8cd98f00b204e9800998ecf8427e\n")

	cmd := &HashSetInfoCmd{Path: in}
	stdout, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout, "md5:    1") {
		t.Errorf("expected 'md5:    1' in text-file info output, got %q", stdout)
	}
}

// TestHashSetInfoCmd_Run_MissingFile surfaces an error when the
// hashset path doesn't exist.
func TestHashSetInfoCmd_Run_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	cmd := &HashSetInfoCmd{Path: filepath.Join(tmp, "does-not-exist.db")}
	_, err := captureStdout(t, func() error { return cmd.Run(t.Context()) })
	if err == nil {
		t.Fatalf("expected error for missing hashset file, got nil")
	}
}
