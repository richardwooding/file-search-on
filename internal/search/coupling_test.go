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

func TestCoupling_RustWorkspace(t *testing.T) {
	root := t.TempDir()
	// Virtual workspace root (no [package]) + three member crates.
	mustWriteFile(t, filepath.Join(root, "Cargo.toml"),
		"[workspace]\nmembers = [\"a\", \"b\", \"c\"]\n")
	for _, c := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(root, c, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(root, c, "Cargo.toml"),
			"[package]\nname = \""+c+"\"\nversion = \"0.1.0\"\n")
	}
	// a → b, a → c (and std, which must NOT count); b → c; c → nothing.
	mustWriteFile(t, filepath.Join(root, "a", "src", "lib.rs"),
		"use b::hello;\nuse c::world;\nuse std::fmt;\n\npub fn run() { hello(); world(); }\n")
	mustWriteFile(t, filepath.Join(root, "b", "src", "lib.rs"),
		"use c::world;\n\npub fn hello() { world(); }\n")
	mustWriteFile(t, filepath.Join(root, "c", "src", "lib.rs"),
		"pub fn world() {}\n")

	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	if res.Module == "" {
		t.Fatalf("expected non-empty workspace identity")
	}

	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	want := map[string]struct {
		ca, ce int
		i      float64
	}{
		"a": {0, 2, 1.0}, // imports b + c
		"b": {1, 1, 0.5}, // imports c, imported by a
		"c": {2, 0, 0.0}, // imported by a + b
	}
	for crate, w := range want {
		p, ok := got[crate]
		if !ok {
			t.Errorf("missing crate %s in %+v", crate, res.Packages)
			continue
		}
		if p.Afferent != w.ca || p.Efferent != w.ce || p.Instability != w.i {
			t.Errorf("%s = {Ca:%d Ce:%d I:%.2f}, want {Ca:%d Ce:%d I:%.2f}",
				crate, p.Afferent, p.Efferent, p.Instability, w.ca, w.ce, w.i)
		}
	}
	// The std import must NOT leak in as a crate node.
	if _, ok := got["std"]; ok {
		t.Error("external crate std leaked into the crate set")
	}
	// Ranking: most-depended-upon (highest Ca) first.
	if len(res.Packages) > 0 && res.Packages[0].Package != "c" {
		t.Errorf("ranked first = %s, want c (Ca=2)", res.Packages[0].Package)
	}
}

// TestCoupling_RustHyphenName checks a hyphenated Cargo package name
// (`data-store`) is matched against its underscore import form (`data_store`).
func TestCoupling_RustHyphenName(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Cargo.toml"),
		"[workspace]\nmembers = [\"app\", \"data-store\"]\n")
	for _, c := range []string{"app", "data-store"} {
		if err := os.MkdirAll(filepath.Join(root, c, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(root, c, "Cargo.toml"),
			"[package]\nname = \""+c+"\"\nversion = \"0.1.0\"\n")
	}
	mustWriteFile(t, filepath.Join(root, "app", "src", "lib.rs"),
		"use data_store::open;\n\npub fn run() { open(); }\n")
	mustWriteFile(t, filepath.Join(root, "data-store", "src", "lib.rs"),
		"pub fn open() {}\n")

	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	// The edge app → data_store must register despite the hyphen/underscore
	// spelling difference (node ids normalised to the import form).
	ds, ok := got["data_store"]
	if !ok {
		t.Fatalf("missing crate data_store in %+v", res.Packages)
	}
	if ds.Afferent != 1 {
		t.Errorf("data_store Ca = %d, want 1 (imported by app)", ds.Afferent)
	}
	if app, ok := got["app"]; ok && app.Efferent != 1 {
		t.Errorf("app Ce = %d, want 1 (imports data_store)", app.Efferent)
	}
}

func TestCoupling_JavaPackages(t *testing.T) {
	root := t.TempDir()
	// pom.xml selects the Java adapter; its content is irrelevant.
	mustWriteFile(t, filepath.Join(root, "pom.xml"), "<project></project>\n")
	mk := func(pkgPath, pkg, body string) {
		dir := filepath.Join(root, "src", "main", "java", filepath.FromSlash(pkgPath))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(dir, "C.java"), "package "+pkg+";\n\n"+body)
	}
	// com.app → com.svc, com.app → com.util (and java.util, excluded);
	// com.svc → com.util; com.util → nothing.
	mk("com/app", "com.app",
		"import com.svc.Service;\nimport com.util.Helper;\nimport java.util.List;\n\npublic class C { }\n")
	// com.svc reaches com.util via a STATIC import — the FQN names a member
	// (com.util.Helper.help), so the edge only survives if firstPartyImport
	// resolves the longest declared-package prefix (com.util), not the class.
	mk("com/svc", "com.svc",
		"import static com.util.Helper.help;\n\npublic class C { }\n")
	mk("com/util", "com.util",
		"public class C { }\n")

	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}

	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	want := map[string]struct {
		ca, ce int
		i      float64
	}{
		"com.app":  {0, 2, 1.0}, // imports com.svc + com.util
		"com.svc":  {1, 1, 0.5}, // imports com.util, imported by com.app
		"com.util": {2, 0, 0.0}, // imported by com.app + com.svc
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
	// The JDK import java.util.* must NOT count — java.util is not declared
	// in-tree, so it never enters the first-party node set.
	if _, ok := got["java.util"]; ok {
		t.Error("JDK package java.util leaked into the first-party set")
	}
	if len(res.Packages) > 0 && res.Packages[0].Package != "com.util" {
		t.Errorf("ranked first = %s, want com.util (Ca=2)", res.Packages[0].Package)
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
