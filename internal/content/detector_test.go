package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func TestDetect(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		file string
		want string
	}{
		{"test.md", "markdown"},
		{"test.json", "json"},
		{"test.xml", "xml"},
		{"test.html", "html"},
	}

	for _, tc := range cases {
		path := filepath.Join(dir, tc.file)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
		ct := content.DefaultRegistry().Detect(path)
		if ct == nil {
			t.Errorf("Detect(%s): got nil, want %s", tc.file, tc.want)
			continue
		}
		if ct.Name() != tc.want {
			t.Errorf("Detect(%s): got %s, want %s", tc.file, ct.Name(), tc.want)
		}
	}
}
