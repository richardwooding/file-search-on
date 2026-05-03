package content_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
)

func TestCSVAttributes(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		file        string
		body        string
		wantCount   int64
		wantColumns []string
	}{
		{"data.csv", "a,b,c,d\n1,2,3,4\n", 4, []string{"a", "b", "c", "d"}},
		{"data.tsv", "a\tb\tc\n1\t2\t3\n", 3, []string{"a", "b", "c"}},
		{"single.csv", "only_one_column\n1\n2\n", 1, []string{"only_one_column"}},
		{"leading-empty.csv", "\n\nx,y,z\n", 3, []string{"x", "y", "z"}},
		{
			"quoted.csv",
			`"a,b",c,"d""e",f` + "\n1,2,3,4\n",
			4,
			[]string{"a,b", "c", `d"e`, "f"},
		},
		{
			"quoted-tab.tsv",
			"\"col\twith\ttabs\"\tb\tc\n1\t2\t3\n",
			3,
			[]string{"col\twith\ttabs", "b", "c"},
		},
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
			if got := attrs["column_count"]; got != tc.wantCount {
				t.Errorf("column_count = %v, want %d", got, tc.wantCount)
			}
			gotCols, ok := attrs["csv_columns"].([]string)
			if !ok {
				t.Fatalf("csv_columns wrong type: %T", attrs["csv_columns"])
			}
			if !reflect.DeepEqual(gotCols, tc.wantColumns) {
				t.Errorf("csv_columns = %#v, want %#v", gotCols, tc.wantColumns)
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
	cols, ok := attrs["csv_columns"].([]string)
	if !ok {
		t.Fatalf("csv_columns wrong type: %T", attrs["csv_columns"])
	}
	if len(cols) != 0 {
		t.Errorf("csv_columns = %#v, want empty slice", cols)
	}
}
