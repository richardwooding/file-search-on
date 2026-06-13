package search_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestUnusedExports(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "a", "a.go"), "package a\n\n"+
		"type LocalOnly struct{}\n"+ // used only within package a → candidate
		"type CrossUsed struct{}\n"+ // used by package b → NOT a candidate
		"type RunCmd struct{}\n"+ // kong-style, reflection-dispatched → excluded
		"type Unref struct{}\n"+ // never referenced → dead_code's job, not here
		"func Exported() {}\n") // called only within a → candidate
	mustWriteFile(t, filepath.Join(root, "a", "a2.go"), "package a\n\n"+
		"type Holder struct{ R RunCmd }\n"+
		"func consume() { _ = LocalOnly{}; Exported() }\n")
	mustWriteFile(t, filepath.Join(root, "b", "b.go"), "package b\n\n"+
		"import \"example.com/m/a\"\n\n"+
		"func B() { _ = a.CrossUsed{} }\n")

	res, err := search.UnusedExports(context.Background(), search.Options{Root: root, Workers: 1}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("UnusedExports: %v", err)
	}
	if res.Module != "example.com/m" {
		t.Fatalf("module = %q, want example.com/m", res.Module)
	}

	got := map[string]string{} // symbol -> kind
	for _, c := range res.Candidates {
		got[c.Symbol] = c.Kind
	}
	if got["LocalOnly"] != "type" {
		t.Errorf("LocalOnly should be a type candidate (used only intra-package): %+v", res.Candidates)
	}
	if got["Exported"] != "function" {
		t.Errorf("Exported should be a function candidate (called only intra-package): %+v", res.Candidates)
	}
	for _, excluded := range []string{"CrossUsed", "RunCmd", "Unref", "Holder", "B"} {
		if _, ok := got[excluded]; ok {
			t.Errorf("%s must NOT be a candidate: %+v", excluded, res.Candidates)
		}
	}
}

func TestUnusedExports_NoGoMod(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "x.go"), "package p\n\nfunc Exported() {}\n")
	res, err := search.UnusedExports(context.Background(), search.Options{Root: root, Workers: 1}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("UnusedExports: %v", err)
	}
	if res.Module != "" || len(res.Candidates) != 0 {
		t.Errorf("no go.mod should yield empty report, got module=%q candidates=%+v", res.Module, res.Candidates)
	}
}
