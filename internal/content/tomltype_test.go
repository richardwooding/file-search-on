package content_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTOMLDetection(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		file string
		body string
	}{
		{"config.toml", "[server]\nport = 8080\n"},
		{"pyproject.toml", "[tool.poetry]\nname = \"x\"\n"},
		// Empty TOML is valid.
		{"empty.toml", ""},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := detectAt(path)
			if ct == nil || ct.Name() != "toml" {
				t.Fatalf("Detect: got %v, want toml", ct)
			}
		})
	}
}
