package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepofilesDetection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		wantType string
	}{
		{"LICENSE", "repo/license"},
		{"LICENCE", "repo/license"},
		{"COPYING", "repo/license"},
		{"CHANGELOG", "repo/changelog"},
		{"HISTORY", "repo/changelog"},
		{"CONTRIBUTING", "repo/contributing"},
		{"CODEOWNERS", "repo/codeowners"},
		{"OWNERS", "repo/codeowners"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte("placeholder\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil {
				t.Fatalf("Detect %q: got nil, want %s", tc.name, tc.wantType)
			}
			if ct.Name() != tc.wantType {
				t.Errorf("Detect %q: got %q, want %q", tc.name, ct.Name(), tc.wantType)
			}
		})
	}
}

// TestLicenseMarkdownPrecedence verifies that LICENSE.md still detects
// as markdown (the exact-name match needs the EXACT basename, so the
// extension pass takes over for the .md variant).
func TestLicenseMarkdownPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "LICENSE.md")
	if err := os.WriteFile(path, []byte("# MIT License\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ct := detectAt(path)
	if ct == nil || ct.Name() != "markdown" {
		t.Errorf("Detect LICENSE.md: got %v, want markdown (exact-name shouldn't fire for .md variant)", ct)
	}
}
