//go:build darwin

package content

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Darwin-only tests for the xattr SYSCALL surface. Parser-level tests
// (parseQuarantineValue, mergeQuarantineAttrs, mergeWhereFromsAttrs,
// mergeFinderTagAttrs) live in xattrs_parse_test.go so they run on
// every CI runner regardless of OS.

func TestReadXattrs_RealFileWithQuarantine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Write a real quarantine xattr.
	if err := unix.Setxattr(path, xattrQuarantine,
		[]byte("0083;69c554fd;TestAgent;test-uuid"), 0); err != nil {
		t.Skipf("setxattr unsupported on this fs: %v", err)
	}

	attrs := ReadXattrs(path)
	if attrs["is_quarantined"] != true {
		t.Error("is_quarantined should be true")
	}
	if attrs["quarantine_agent"] != "TestAgent" {
		t.Errorf("quarantine_agent = %v", attrs["quarantine_agent"])
	}
	if got := attrs["xattr_count"].(int64); got < 1 {
		t.Errorf("xattr_count = %d, want >= 1", got)
	}
	keys, _ := attrs["xattr_keys"].([]string)
	found := false
	for _, k := range keys {
		if k == xattrQuarantine {
			found = true
		}
	}
	if !found {
		t.Errorf("quarantine key missing from xattr_keys = %v", keys)
	}
}

func TestReadXattrs_NoQuarantineNoFinder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// NB: macOS Sonoma+ automatically sets com.apple.provenance on
	// every new file, so the xattr surface is never empty in practice.
	// We assert the structural contract: no quarantine / Finder tags /
	// Finder comment — xattr_keys may carry the system-set entries.
	attrs := ReadXattrs(path)
	if _, ok := attrs["is_quarantined"]; ok {
		t.Errorf("is_quarantined should be absent for non-downloaded file")
	}
	if _, ok := attrs["finder_color"]; ok {
		t.Errorf("finder_color should be absent")
	}
	if _, ok := attrs["finder_tags"]; ok {
		t.Errorf("finder_tags should be absent")
	}
	if _, ok := attrs["has_finder_comment"]; ok {
		t.Errorf("has_finder_comment should be absent")
	}
}

func TestReadXattrs_MissingFile(t *testing.T) {
	attrs := ReadXattrs("/nonexistent/path/xyz.bin")
	if len(attrs) != 0 {
		t.Errorf("expected empty attrs for missing file, got %v", attrs)
	}
}
