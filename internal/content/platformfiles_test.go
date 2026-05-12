package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformfilesDetection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name     string
		wantType string
	}{
		{"Procfile", "platform/procfile"},
		{"Vagrantfile", "platform/vagrant"},
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
