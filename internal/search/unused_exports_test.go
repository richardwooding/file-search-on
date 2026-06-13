package search_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"

	"github.com/richardwooding/file-search-on/internal/search"
)

// TestUnusedExports_Python pins the Phase-A cross-language extension: Python
// uses the public/_private name convention + directory-as-package. A public
// function used only within its own package directory is a candidate; one
// used from another directory is not; a _private one is never reported.
func TestUnusedExports_Python(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"pkg", "other"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "pkg", "a.py"),
		"def local_only():\n    pass\n\ndef cross_used():\n    pass\n\ndef _private():\n    pass\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "b.py"),
		"def use_local():\n    local_only()\n") // references local_only intra-package
	mustWriteFile(t, filepath.Join(root, "other", "c.py"),
		"def use_cross():\n    cross_used()\n") // references cross_used from another package

	res, err := search.UnusedExports(context.Background(), search.Options{Root: root, Expr: "is_source"}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("UnusedExports: %v", err)
	}
	got := map[string]bool{}
	for _, c := range res.Candidates {
		got[c.Symbol] = true
	}
	if !got["local_only"] {
		t.Errorf("local_only should be a candidate (public, used only in pkg/): %+v", res.Candidates)
	}
	if got["cross_used"] {
		t.Errorf("cross_used must NOT be a candidate (used from other/): %+v", res.Candidates)
	}
	if got["_private"] {
		t.Errorf("_private is not exported in Python; must not be reported: %+v", res.Candidates)
	}
}

// TestUnusedExports_TypeScript pins Phase B keyword-visibility + file-as-
// module: an `export`ed function used only within its own file is a
// candidate; one imported and used from another file is not; a non-exported
// function is never reported.
func TestUnusedExports_TypeScript(t *testing.T) {
	root := t.TempDir()
	// a.ts exports localOnly (used only here), crossUsed (used in b.ts), and
	// defines a private helper. b.ts imports + uses crossUsed.
	mustWriteFile(t, filepath.Join(root, "a.ts"),
		"export function localOnly() { return helper(); }\n"+
			"export function crossUsed() {}\n"+
			"function helper() { return localOnly(); }\n")
	mustWriteFile(t, filepath.Join(root, "b.ts"),
		"import { crossUsed } from './a';\nexport function run() { crossUsed(); }\n")

	res, err := search.UnusedExports(context.Background(), search.Options{Root: root, Expr: "is_source"}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("UnusedExports: %v", err)
	}
	got := map[string]bool{}
	for _, c := range res.Candidates {
		got[c.Symbol] = true
	}
	if !got["localOnly"] {
		t.Errorf("localOnly should be a candidate (exported, used only in a.ts): %+v", res.Candidates)
	}
	if got["crossUsed"] {
		t.Errorf("crossUsed must NOT be a candidate (used from b.ts): %+v", res.Candidates)
	}
	if got["helper"] {
		t.Errorf("helper is not exported; must not be reported: %+v", res.Candidates)
	}
}

// TestUnusedExports_Java pins Phase C: `public` visibility + directory-as-
// package. A public method called only from within its own directory is a
// candidate; one called from another directory is not; a package-private
// method is never reported.
func TestUnusedExports_Java(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "a", "Lib.java"),
		"public class Lib {\n  public static void localUtil() {}\n  public static void crossUtil() {}\n  static void pkgUtil() {}\n}\n")
	mustWriteFile(t, filepath.Join(root, "a", "UseLocal.java"),
		"public class UseLocal {\n  void r() { Lib.localUtil(); }\n}\n") // calls localUtil intra-directory
	mustWriteFile(t, filepath.Join(root, "b", "UseCross.java"),
		"public class UseCross {\n  void r() { Lib.crossUtil(); }\n}\n") // calls crossUtil from another directory

	res, err := search.UnusedExports(context.Background(), search.Options{Root: root, Expr: "is_source"}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("UnusedExports: %v", err)
	}
	got := map[string]bool{}
	for _, c := range res.Candidates {
		got[c.Symbol] = true
	}
	if !got["localUtil"] {
		t.Errorf("localUtil should be a candidate (public, called only in a/): %+v", res.Candidates)
	}
	if got["crossUtil"] {
		t.Errorf("crossUtil must NOT be a candidate (called from b/): %+v", res.Candidates)
	}
	if got["pkgUtil"] {
		t.Errorf("pkgUtil is package-private (not public); must not be reported: %+v", res.Candidates)
	}
}

// TestUnusedExports_Kotlin pins the default-public negation path end-to-end:
// a public (default-visibility) top-level function called only within its
// own directory is a candidate; one called from another directory is not; a
// `private` function is never reported.
func TestUnusedExports_Kotlin(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "a", "lib.kt"),
		"fun localOnly() {}\nfun crossUsed() {}\nprivate fun privFn() {}\n")
	mustWriteFile(t, filepath.Join(root, "a", "use.kt"),
		"fun useLocal() { localOnly() }\n") // calls localOnly intra-directory
	mustWriteFile(t, filepath.Join(root, "b", "other.kt"),
		"fun useCross() { crossUsed() }\n") // calls crossUsed from another directory

	res, err := search.UnusedExports(context.Background(), search.Options{Root: root, Expr: "is_source"}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("UnusedExports: %v", err)
	}
	got := map[string]bool{}
	for _, c := range res.Candidates {
		got[c.Symbol] = true
	}
	if !got["localOnly"] {
		t.Errorf("localOnly should be a candidate (public, called only in a/): %+v", res.Candidates)
	}
	if got["crossUsed"] {
		t.Errorf("crossUsed must NOT be a candidate (called from b/): %+v", res.Candidates)
	}
	if got["privFn"] {
		t.Errorf("privFn is private; must not be reported: %+v", res.Candidates)
	}
}

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
