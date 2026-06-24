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
	// Cognitive complexity (#485) is plumbed through for Go: Hairy nests
	// if > for > if = 1 + 2 + 3 = 6; Simple is 0.
	if c := rep.Functions[0].CognitiveComplexity; c == nil || *c != 6 {
		t.Errorf("Hairy cognitive=%v want 6", c)
	}
	if c := rep.Functions[1].CognitiveComplexity; c == nil || *c != 0 {
		t.Errorf("Simple cognitive=%v want 0", c)
	}
}

// TestComplexity_CognitiveUnavailableForNonGo: tree-sitter languages don't
// compute cognitive complexity yet (#485 follow-up), so the field is nil —
// distinct from a genuine 0 — and never a misleading number.
func TestComplexity_CognitiveUnavailableForNonGo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.py"),
		[]byte("def branchy(x):\n    if x > 0:\n        for i in range(x):\n            if i % 2 == 0:\n                return i\n    return 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := search.Complexity(t.Context(), search.Options{Root: dir, Expr: `is_source && language == "python"`}, content.DefaultRegistry(), 50)
	if err != nil {
		t.Fatalf("Complexity: %v", err)
	}
	if len(rep.Functions) == 0 {
		t.Fatal("no functions found for python fixture")
	}
	for _, f := range rep.Functions {
		if f.CognitiveComplexity != nil {
			t.Errorf("%s: cognitive=%v, want nil (unavailable for python)", f.Function, *f.CognitiveComplexity)
		}
		if f.Complexity <= 0 {
			t.Errorf("%s: cyclomatic=%d, want > 0", f.Function, f.Complexity)
		}
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
