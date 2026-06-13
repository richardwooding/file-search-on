package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

func TestAPIDiff(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()

	// Baseline: exported Alpha + Beta funcs, exported Widget type, plus an
	// unexported helper that must NOT appear in the diff.
	mustWriteFile(t, filepath.Join(a, "api.go"), "package p\n\n"+
		"func Alpha() {}\n"+
		"func Beta() {}\n"+
		"type Widget struct{}\n"+
		"func helper() {}\n")

	// Candidate: Beta removed, Gamma added, Widget kept, helper still private,
	// and a new exported Gadget type.
	mustWriteFile(t, filepath.Join(b, "api.go"), "package p\n\n"+
		"func Alpha() {}\n"+
		"func Gamma() {}\n"+
		"type Widget struct{}\n"+
		"type Gadget struct{}\n"+
		"func helper() {}\n")

	mkOpts := func(root string) search.Options {
		return search.Options{Root: root, Expr: "is_source"}
	}
	res, err := search.APIDiff(context.Background(), mkOpts(a), mkOpts(b), content.DefaultRegistry())
	if err != nil {
		t.Fatalf("APIDiff: %v", err)
	}

	if !res.Breaking {
		t.Errorf("Beta was removed; expected breaking=true")
	}
	has := func(syms []search.APISymbol, name, kind string) bool {
		for _, s := range syms {
			if s.Symbol == name && s.Kind == kind {
				return true
			}
		}
		return false
	}
	if !has(res.Removed, "Beta", "function") {
		t.Errorf("Removed = %+v, want Beta/function", res.Removed)
	}
	if has(res.Removed, "helper", "function") {
		t.Errorf("unexported helper must not be reported: %+v", res.Removed)
	}
	if !has(res.Added, "Gamma", "function") || !has(res.Added, "Gadget", "type") {
		t.Errorf("Added = %+v, want Gamma/function + Gadget/type", res.Added)
	}
	// Alpha + Widget unchanged → neither bucket.
	if has(res.Removed, "Alpha", "function") || has(res.Added, "Alpha", "function") {
		t.Errorf("Alpha unchanged, must not appear: removed=%+v added=%+v", res.Removed, res.Added)
	}
	if res.RemovedCount != len(res.Removed) || res.AddedCount != len(res.Added) {
		t.Errorf("counts out of sync: %+v", res)
	}
}

func TestAPIDiff_NoChange(t *testing.T) {
	a := t.TempDir()
	src := "package p\n\nfunc Exported() {}\ntype T struct{}\n"
	mustWriteFile(t, filepath.Join(a, "api.go"), src)

	opts := search.Options{Root: a, Expr: "is_source"}
	res, err := search.APIDiff(context.Background(), opts, opts, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("APIDiff: %v", err)
	}
	if res.Breaking || res.RemovedCount != 0 || res.AddedCount != 0 {
		t.Errorf("identical trees must produce no diff: %+v", res)
	}
	if res.ExportedA != res.ExportedB || res.ExportedA == 0 {
		t.Errorf("exported counts = %d / %d, want equal and non-zero", res.ExportedA, res.ExportedB)
	}
}
