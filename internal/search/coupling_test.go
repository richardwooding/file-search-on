package search_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestCoupling(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	for _, d := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// a → b, a → c ; b → c ; c → (nothing)
	mustWriteFile(t, filepath.Join(root, "a", "a.go"), "package a\n\n"+
		"import (\n\t\"example.com/m/b\"\n\t\"example.com/m/c\"\n\t\"fmt\"\n)\n\n"+
		"func A() { b.B(); c.C(); fmt.Println() }\n")
	mustWriteFile(t, filepath.Join(root, "b", "b.go"), "package b\n\n"+
		"import \"example.com/m/c\"\n\nfunc B() { c.C() }\n")
	mustWriteFile(t, filepath.Join(root, "c", "c.go"), "package c\n\nfunc C() {}\n")

	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	if res.Module != "example.com/m" {
		t.Fatalf("module = %q, want example.com/m", res.Module)
	}

	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	want := map[string]struct {
		ca, ce int
		i      float64
	}{
		"example.com/m/a": {0, 2, 1.0}, // imports b + c, imported by none
		"example.com/m/b": {1, 1, 0.5}, // imports c, imported by a
		"example.com/m/c": {2, 0, 0.0}, // imports none, imported by a + b
	}
	for pkg, w := range want {
		p, ok := got[pkg]
		if !ok {
			t.Errorf("missing package %s in %+v", pkg, res.Packages)
			continue
		}
		if p.Afferent != w.ca || p.Efferent != w.ce || p.Instability != w.i {
			t.Errorf("%s = {Ca:%d Ce:%d I:%.2f}, want {Ca:%d Ce:%d I:%.2f}",
				pkg, p.Afferent, p.Efferent, p.Instability, w.ca, w.ce, w.i)
		}
	}

	// Ranking: most-depended-upon (highest Ca) first.
	if len(res.Packages) > 0 && res.Packages[0].Package != "example.com/m/c" {
		t.Errorf("ranked first = %s, want example.com/m/c (Ca=2)", res.Packages[0].Package)
	}
	// The third-party import (fmt) must NOT appear as a package node.
	if _, ok := got["fmt"]; ok {
		t.Error("third-party import fmt leaked into the package set")
	}
}

func TestCoupling_NoGoMod(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "x.go"), "package p\n\nfunc F() {}\n")
	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	if res.Module != "" || len(res.Packages) != 0 {
		t.Errorf("no go.mod should yield empty report, got module=%q packages=%+v", res.Module, res.Packages)
	}
}
