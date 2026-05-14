package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSystemFilesDetection covers the five OS-generated metadata
// types — macOS / Windows / Linux. Each row picks one of the
// canonical filenames; the Thumbs.db variants get separate rows to
// confirm Filenames() lists more than just the headline name.
func TestSystemFilesDetection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		wantType string
	}{
		{".DS_Store", "system/macos-ds-store"},
		{".localized", "system/macos-localized"},
		{"Thumbs.db", "system/windows-thumbs-db"},
		{"ehthumbs.db", "system/windows-thumbs-db"},
		{"ehthumbs_vista.db", "system/windows-thumbs-db"},
		{"Desktop.ini", "system/windows-desktop-ini"},
		{".directory", "system/linux-directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, nil, 0o644); err != nil {
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

// TestSystemFilesEmptyAttributes confirms each new type returns an
// empty Attributes map (detection-only; no parsing).
func TestSystemFilesEmptyAttributes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".DS_Store", ".localized", "Thumbs.db", "Desktop.ini", ".directory"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("ignored"), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil {
				t.Fatalf("Detect %q: got nil", name)
			}
			attrs, err := ct.Attributes(t.Context(), nil, path)
			if err != nil {
				t.Fatalf("Attributes %q: %v", name, err)
			}
			if len(attrs) != 0 {
				t.Errorf("Attributes %q: got %d entries, want 0 (detection-only)", name, len(attrs))
			}
		})
	}
}
