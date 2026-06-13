package search_test

import (
	"context"
	"path/filepath"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestReferences(t *testing.T) {
	dir := t.TempDir()
	// Widget defined in a.go, used as a field type in b.go and a param type
	// in c.go. The definition site itself is not a reference.
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package p\n\ntype Widget struct{}\n")
	mustWriteFile(t, filepath.Join(dir, "b.go"), "package p\n\ntype Holder struct{ w Widget }\n")
	mustWriteFile(t, filepath.Join(dir, "c.go"), "package p\n\nfunc use(x Widget) {}\n")

	res, err := search.References(context.Background(), search.Options{Root: dir, Expr: "is_source"}, "Widget", "", contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("References: %v", err)
	}

	byBase := map[string]search.ReferenceSite{}
	for _, r := range res.References {
		byBase[filepath.Base(r.Path)] = r
	}
	if _, ok := byBase["b.go"]; !ok {
		t.Errorf("expected a Widget reference in b.go: %+v", res.References)
	}
	if _, ok := byBase["c.go"]; !ok {
		t.Errorf("expected a Widget reference in c.go: %+v", res.References)
	}
	if _, ok := byBase["a.go"]; ok {
		t.Errorf("a.go only DEFINES Widget — must not be a reference: %+v", res.References)
	}
	for _, r := range res.References {
		if r.Kind != "type" {
			t.Errorf("Widget usages should all be 'type', got %+v", r)
		}
		if r.Line == 0 {
			t.Errorf("reference missing a line: %+v", r)
		}
	}

	// kind filter excludes everything (no calls to Widget).
	calls, err := search.References(context.Background(), search.Options{Root: dir, Expr: "is_source"}, "Widget", "call", contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("References(call): %v", err)
	}
	if calls.Count != 0 {
		t.Errorf("Widget has no call sites; got %+v", calls.References)
	}
}
