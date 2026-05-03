package content_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func TestCSVColumnCount(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		file string
		body string
		want int64
	}{
		{"data.csv", "a,b,c,d\n1,2,3,4\n", 4},
		{"data.tsv", "a\tb\tc\n1\t2\t3\n", 3},
		{"single.csv", "only_one_column\n1\n2\n", 1},
		{"leading-empty.csv", "\n\nx,y,z\n", 3},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(dir, tc.file)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			ct := content.DefaultRegistry().Detect(path)
			if ct == nil || ct.Name() != "csv" {
				t.Fatalf("Detect: got %v, want csv", ct)
			}
			attrs, err := ct.Attributes(path)
			if err != nil {
				t.Fatalf("Attributes: %v", err)
			}
			if got := attrs["column_count"]; got != tc.want {
				t.Errorf("column_count = %v, want %d", got, tc.want)
			}
		})
	}
}

func TestCSVEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.csv")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ct := content.DefaultRegistry().Detect(path)
	attrs, err := ct.Attributes(path)
	if err != nil {
		t.Fatalf("Attributes: %v", err)
	}
	if got := attrs["column_count"]; got != int64(0) {
		t.Errorf("column_count = %v, want 0", got)
	}
}
