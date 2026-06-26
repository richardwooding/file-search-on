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

func TestCoupling_CSharpNamespaces(t *testing.T) {
	root := t.TempDir()
	// A .sln selects the C# adapter; its content is irrelevant.
	mustWriteFile(t, filepath.Join(root, "App.sln"), "Microsoft Visual Studio Solution File\n")
	mk := func(dir, src string) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, "C.cs"), src)
	}
	// MyApp.App → MyApp.Svc, MyApp.Util (and System.*, excluded);
	// MyApp.Svc → MyApp.Util via a STATIC using (exercises longest-prefix);
	// MyApp.Util → nothing.
	mk("App",
		"using MyApp.Svc;\nusing MyApp.Util;\nusing System.Collections.Generic;\nnamespace MyApp.App;\npublic class App {}\n")
	mk("Svc",
		"using static MyApp.Util.Helper;\nnamespace MyApp.Svc;\npublic class Svc {}\n")
	mk("Util",
		"namespace MyApp.Util;\npublic class Helper { public static void Help() {} }\n")

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
		"MyApp.App":  {0, 2, 1.0},
		"MyApp.Svc":  {1, 1, 0.5},
		"MyApp.Util": {2, 0, 0.0},
	}
	for ns, w := range want {
		p, ok := got[ns]
		if !ok {
			t.Errorf("missing namespace %s in %+v", ns, res.Packages)
			continue
		}
		if p.Afferent != w.ca || p.Efferent != w.ce || p.Instability != w.i {
			t.Errorf("%s = {Ca:%d Ce:%d I:%.2f}, want {Ca:%d Ce:%d I:%.2f}",
				ns, p.Afferent, p.Efferent, p.Instability, w.ca, w.ce, w.i)
		}
	}
	// The .NET BCL namespace System.* must NOT count — never declared in-tree.
	if _, ok := got["System.Collections.Generic"]; ok {
		t.Error("BCL namespace System.Collections.Generic leaked into the first-party set")
	}
	if len(res.Packages) > 0 && res.Packages[0].Package != "MyApp.Util" {
		t.Errorf("ranked first = %s, want MyApp.Util (Ca=2)", res.Packages[0].Package)
	}
}

// TestCoupling_CSharpRootGlobMetachars guards the C# root detection against
// glob metacharacters in the project path itself — a "proj[1]" component
// would break a naive filepath.Glob(join(dir, "*.sln")) (the brackets are
// glob-interpreted), leaving the adapter unselected and the report empty.
func TestCoupling_CSharpRootGlobMetachars(t *testing.T) {
	root := filepath.Join(t.TempDir(), "proj[1]")
	if err := os.MkdirAll(filepath.Join(root, "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "B"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(root, "App.sln"), "Microsoft Visual Studio Solution File\n")
	mustWriteFile(t, filepath.Join(root, "A", "A.cs"), "using MyApp.B;\nnamespace MyApp.A;\npublic class A {}\n")
	mustWriteFile(t, filepath.Join(root, "B", "B.cs"), "namespace MyApp.B;\npublic class B {}\n")

	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	if len(res.Packages) == 0 {
		t.Fatal("C# adapter not selected for a path with glob metacharacters; report is empty")
	}
	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	if b, ok := got["MyApp.B"]; !ok || b.Afferent != 1 {
		t.Errorf("MyApp.B Ca = %d (present=%v), want 1", b.Afferent, ok)
	}
}

func TestCoupling_PythonPackages(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pyproject.toml"), "[project]\nname = \"demo\"\n")
	mk := func(pkg, file, src string) {
		dir := filepath.Join(root, pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(dir, file), src)
	}
	// app → svc, app → util (and os, excluded); svc → util; util → nothing.
	mk("app", "main.py",
		"import svc.service\nfrom util.helper import h\nimport os\n\ndef main():\n    pass\n")
	mk("svc", "service.py",
		"from util.helper import h\n\ndef s():\n    pass\n")
	mk("util", "helper.py",
		"def h():\n    pass\n")

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
		"app":  {0, 2, 1.0},
		"svc":  {1, 1, 0.5},
		"util": {2, 0, 0.0},
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
	// The stdlib import os must NOT count — not a first-party package.
	if _, ok := got["os"]; ok {
		t.Error("stdlib package os leaked into the first-party set")
	}
}

// TestCoupling_PythonRelativeImports verifies relative imports (the idiomatic
// way Python packages import internally, e.g. flask) are resolved against the
// importing file's package and counted — the absolute-only version produced
// an empty graph for such projects (#467).
func TestCoupling_PythonRelativeImports(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pyproject.toml"), "[project]\nname = \"demo\"\n")
	mk := func(pkg, file, src string) {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(dir, file), src)
	}
	// pkg → pkg.svc, pkg → pkg.util (the `from . import helper` is a self-edge,
	// dropped); pkg.svc → pkg.util via a two-dot relative import.
	mk("pkg", "app.py",
		"from .svc import s\nfrom .util import u\nfrom . import helper\n\ndef main():\n    pass\n")
	mk("pkg", "helper.py", "def h():\n    pass\n")
	mk("pkg/svc", "service.py", "from ..util import u\n\ndef s():\n    pass\n")
	mk("pkg/util", "helper.py", "def u():\n    pass\n")

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
		"pkg":      {0, 2, 1.0},
		"pkg.svc":  {1, 1, 0.5},
		"pkg.util": {2, 0, 0.0},
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
}

// TestCoupling_PythonSrcLayout checks import-root discovery: with a top-level
// src/, package nodes are dotted relative to src/, not the project root.
func TestCoupling_PythonSrcLayout(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "setup.py"), "from setuptools import setup\nsetup()\n")
	mk := func(pkg, file, src string) {
		dir := filepath.Join(root, "src", pkg)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(dir, file), src)
	}
	mk("pkga", "mod.py", "from pkgb.thing import t\n\ndef a():\n    pass\n")
	mk("pkgb", "thing.py", "def t():\n    pass\n")

	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	// Nodes are "pkga"/"pkgb" (relative to src/), not "src.pkga".
	if b, ok := got["pkgb"]; !ok || b.Afferent != 1 {
		t.Errorf("pkgb Ca = %d (present=%v), want 1 (imported by pkga)", b.Afferent, ok)
	}
	if _, leaked := got["src.pkga"]; leaked {
		t.Error("import root src/ not stripped: got node src.pkga")
	}
}

func TestCoupling_TypeScriptModules(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), "{\"name\":\"demo\"}\n")
	mk := func(dir, file, src string) {
		d := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, file), src)
	}
	// src/app → src/svc, src/app → src/util (a sibling "./local" and the bare
	// "react" must NOT count); src/svc → src/util; src/util → nothing.
	mk("src/app", "index.ts",
		"import { s } from \"../svc/service\";\nimport { h } from \"../util/helper\";\nimport { x } from \"./local\";\nimport React from \"react\";\nexport const app = s + h + x;\n")
	mk("src/app", "local.ts", "export const x = 0;\n")
	mk("src/svc", "service.ts", "import { h } from \"../util/helper\";\nexport const s = h;\n")
	mk("src/util", "helper.ts", "export const h = 1;\n")

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
		"src/app":  {0, 2, 1.0}, // imports svc + util; the sibling ./local is a self-edge (dropped)
		"src/svc":  {1, 1, 0.5},
		"src/util": {2, 0, 0.0},
	}
	for mod, w := range want {
		p, ok := got[mod]
		if !ok {
			t.Errorf("missing module %s in %+v", mod, res.Packages)
			continue
		}
		if p.Afferent != w.ca || p.Efferent != w.ce || p.Instability != w.i {
			t.Errorf("%s = {Ca:%d Ce:%d I:%.2f}, want {Ca:%d Ce:%d I:%.2f}",
				mod, p.Afferent, p.Efferent, p.Instability, w.ca, w.ce, w.i)
		}
	}
	// The bare specifier react must NOT count as a first-party module.
	if _, ok := got["react"]; ok {
		t.Error("bare specifier react leaked into the module set")
	}
}

// TestCoupling_JSDirectoryImport covers the directory-vs-file branch: `../b`
// where b/ is a directory (b/index.js) resolves to module b, not its parent.
func TestCoupling_JSDirectoryImport(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "package.json"), "{}\n")
	mk := func(dir, src string) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, "index.js"), src)
	}
	mk("a", "const { t } = require(\"../b\");\nmodule.exports = { t };\n")
	mk("b", "module.exports = { t: 1 };\n")

	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	if b, ok := got["b"]; !ok || b.Afferent != 1 {
		t.Errorf("module b Ca = %d (present=%v), want 1 (the ../b directory import from a)", b.Afferent, ok)
	}
}

// TestCoupling_CSharpNestedSolution verifies C# detection when the solution /
// project markers live one level down (e.g. under Src/) and the repo root has
// none — the real-world Newtonsoft.Json layout that #467 found unhandled.
func TestCoupling_CSharpNestedSolution(t *testing.T) {
	root := t.TempDir() // repo root: no C# marker here
	srcDir := filepath.Join(root, "Src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(ns, src string) {
		d := filepath.Join(srcDir, ns)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, "C.cs"), src)
	}
	// Markers live under Src/, not the repo root.
	mustWriteFile(t, filepath.Join(srcDir, "global.json"), "{}\n")
	mustWriteFile(t, filepath.Join(srcDir, "App.slnx"), "<Solution />\n")
	mk("App", "using MyApp.Util;\nnamespace MyApp.App;\npublic class App {}\n")
	mk("Util", "namespace MyApp.Util;\npublic class Helper {}\n")

	// Pointed at the REPO ROOT (not Src/) — detection must still find C#.
	res, err := search.Coupling(context.Background(), search.Options{Root: root, Workers: 1}, 0, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Coupling: %v", err)
	}
	if len(res.Packages) == 0 {
		t.Fatal("C# adapter not selected for a Src/-nested solution; report is empty")
	}
	got := map[string]search.PackageCoupling{}
	for _, p := range res.Packages {
		got[p.Package] = p
	}
	if u, ok := got["MyApp.Util"]; !ok || u.Afferent != 1 {
		t.Errorf("MyApp.Util Ca = %d (present=%v), want 1 (imported by MyApp.App)", u.Afferent, ok)
	}
}

// TestCoupling_KotlinPackages and TestCoupling_ScalaPackages verify the JVM
// adapter covers Kotlin and Scala (same package model as Java) — selected by
// a Gradle / sbt build file (#467).
func TestCoupling_KotlinPackages(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "build.gradle.kts"), "plugins { kotlin(\"jvm\") }\n")
	mk := func(dir, pkg, body string) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, "C.kt"), "package "+pkg+"\n\n"+body)
	}
	mk("app", "com.app", "import com.svc.Service\nimport com.util.Helper\nimport kotlin.collections.List\n\nclass App\n")
	mk("svc", "com.svc", "import com.util.Helper\n\nclass Service\n")
	mk("util", "com.util", "class Helper\n")
	assertABCGraph(t, root, "com.app", "com.svc", "com.util")
}

func TestCoupling_ScalaPackages(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "build.sbt"), "name := \"demo\"\n")
	mk := func(dir, pkg, body string) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, "C.scala"), "package "+pkg+"\n\n"+body)
	}
	mk("app", "com.app", "import com.svc.Service\nimport com.util.Helper\n\nobject App\n")
	mk("svc", "com.svc", "import com.util.Helper\n\nobject Service\n")
	mk("util", "com.util", "object Helper\n")
	assertABCGraph(t, root, "com.app", "com.svc", "com.util")
}

// TestCoupling_PHPNamespaces verifies PHP namespace coupling, which uses a
// backslash separator (App\Services) rather than a dot (#467).
func TestCoupling_PHPNamespaces(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "composer.json"), "{\"name\":\"demo/app\"}\n")
	mk := func(dir, body string) {
		d := filepath.Join(root, "src", dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, "C.php"), "<?php\n"+body)
	}
	// Demo\App → Demo\Svc, Demo\Util (Psr\Log is external); Demo\Svc → Demo\Util.
	// The Demo\Svc import is fully-qualified (leading backslash) to exercise
	// the leading-separator trim.
	mk("App", "namespace Demo\\App;\nuse \\Demo\\Svc\\Service;\nuse Demo\\Util\\Helper;\nuse Psr\\Log\\Logger;\nclass App {}\n")
	mk("Svc", "namespace Demo\\Svc;\nuse Demo\\Util\\Helper;\nclass Service {}\n")
	mk("Util", "namespace Demo\\Util;\nclass Helper {}\n")
	assertABCGraph(t, root, "Demo\\App", "Demo\\Svc", "Demo\\Util")
}

// TestCoupling_PerlPackages pins the #467 Perl ecosystem: a Makefile.PL dist
// with `package Foo::Bar;` declarations (::-separated) graphs like the JVM
// family. Demo::App → Demo::Svc, Demo::Util (strict / Carp are external pragmas
// / CPAN modules, not first-party nodes); Demo::Svc → Demo::Util.
func TestCoupling_PerlPackages(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Makefile.PL"), "use ExtUtils::MakeMaker;\nWriteMakefile(NAME => 'Demo');\n")
	mk := func(name, body string) {
		d := filepath.Join(root, "lib", "Demo")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, name), body)
	}
	mk("App.pm", "package Demo::App;\nuse strict;\nuse Demo::Svc;\nuse Demo::Util;\nuse Carp;\n1;\n")
	mk("Svc.pm", "package Demo::Svc;\nuse Demo::Util;\n1;\n")
	mk("Util.pm", "package Demo::Util;\n1;\n")
	assertABCGraph(t, root, "Demo::App", "Demo::Svc", "Demo::Util")
}

// TestCoupling_RubyGem pins the #519 Ruby ecosystem: a Gemfile gem, directory
// nodes, resolving both `require "lib-path"` (load-path) and `require_relative`
// to first-party files. lib/a → lib/b (require_relative) + lib/c (require);
// lib/b → lib/c; lib/c → ∅. `require "json"` (stdlib) backs no first-party file
// and must NOT create an edge.
func TestCoupling_RubyGem(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Gemfile"), "source 'https://rubygems.org'\ngemspec\n")
	mk := func(sub, body string) {
		d := filepath.Join(root, "lib", sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, "mod.rb"), body)
	}
	// a uses require_relative for the b edge and load-path require for the c
	// edge — exercising both resolution paths; require "json" is external.
	mk("a", "require 'json'\nrequire_relative '../b/mod'\nrequire 'c/mod'\nmodule A; end\n")
	mk("b", "require 'c/mod'\nmodule B; end\n")
	mk("c", "module C; end\n")
	assertABCGraph(t, root, "lib/a", "lib/b", "lib/c")
}

// TestCoupling_CIncludeGraph pins the #521 C/C++ ecosystem: a CMake project,
// directory nodes, resolving #include to first-party files via the includer's
// dir (file-relative) and the include/ root. include/a → include/b (a "../b/b.h"
// file-relative include) + include/c (a "c/c.h" include-root include); include/b
// → include/c. `#include <stdio.h>` (system) backs no first-party file → no edge.
func TestCoupling_CIncludeGraph(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "CMakeLists.txt"), "project(demo)\n")
	mk := func(sub, body string) {
		d := filepath.Join(root, "include", sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(d, sub+".h"), body)
	}
	mk("a", "#include \"../b/b.h\"\n#include \"c/c.h\"\n#include <stdio.h>\nint a(void);\n")
	mk("b", "#include \"c/c.h\"\nint b(void);\n")
	mk("c", "int c(void);\n")
	assertABCGraph(t, root, "include/a", "include/b", "include/c")
}

// assertABCGraph checks the canonical a→b, a→c; b→c; c→∅ coupling shape used
// by several per-language tests: a={Ca:0,Ce:2,I:1}, b={1,1,0.5}, c={2,0,0}.
func assertABCGraph(t *testing.T, root, a, b, c string) {
	t.Helper()
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
	}{a: {0, 2, 1.0}, b: {1, 1, 0.5}, c: {2, 0, 0.0}}
	for pkg, w := range want {
		p, ok := got[pkg]
		if !ok {
			t.Errorf("missing node %s in %+v", pkg, res.Packages)
			continue
		}
		if p.Afferent != w.ca || p.Efferent != w.ce || p.Instability != w.i {
			t.Errorf("%s = {Ca:%d Ce:%d I:%.2f}, want {Ca:%d Ce:%d I:%.2f}",
				pkg, p.Afferent, p.Efferent, p.Instability, w.ca, w.ce, w.i)
		}
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
