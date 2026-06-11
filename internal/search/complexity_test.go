package search_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

func TestComplexity(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("a.go", "package a\n\nfunc Simple() {}\nfunc Hairy(x int) {\n\tif x > 0 {\n\t\tfor i := 0; i < x; i++ {\n\t\t\tif i > 1 {}\n\t\t}\n\t}\n}\n")

	rep, err := search.Complexity(t.Context(), search.Options{Root: dir, Expr: `is_source && language == "go"`}, content.DefaultRegistry(), 50)
	if err != nil {
		t.Fatalf("Complexity: %v", err)
	}
	if rep.TotalFunctions != 2 {
		t.Fatalf("TotalFunctions=%d want 2", rep.TotalFunctions)
	}
	// Sorted worst-first: Hairy (1+if+for+if=4) before Simple (1).
	if rep.Functions[0].Function != "Hairy" || rep.Functions[0].Complexity != 4 {
		t.Errorf("worst=%+v want Hairy/4", rep.Functions[0])
	}
	if rep.Functions[1].Function != "Simple" || rep.Functions[1].Complexity != 1 {
		t.Errorf("second=%+v want Simple/1", rep.Functions[1])
	}
}

func TestComplexity_TopCap(t *testing.T) {
	dir := t.TempDir()
	body := "package a\n\nfunc A(){}\nfunc B(){}\nfunc C(){}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := search.Complexity(t.Context(), search.Options{Root: dir, Expr: "is_source"}, content.DefaultRegistry(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TotalFunctions != 3 {
		t.Errorf("TotalFunctions=%d want 3", rep.TotalFunctions)
	}
	if len(rep.Functions) != 2 {
		t.Errorf("returned %d functions want 2 (top cap)", len(rep.Functions))
	}
}
