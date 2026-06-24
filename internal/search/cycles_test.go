package search_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

func cyclesOf(t *testing.T, root string) *search.CyclesResult {
	t.Helper()
	res, err := search.Cycles(context.Background(), search.Options{Root: root, Workers: 1}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}
	return res
}

// TestCycles_GoCycle: a → b → c → a is one 3-node cycle. (Go rejects import
// cycles at compile time, but file-search-on only parses imports, so a cyclic
// source tree is fine to analyse.)
func TestCycles_GoCycle(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	for _, d := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nimport \"example.com/m/b\"\n\nfunc A() { b.B() }\n")
	mustWriteFile(t, filepath.Join(root, "b", "b.go"), "package b\n\nimport \"example.com/m/c\"\n\nfunc B() { c.C() }\n")
	mustWriteFile(t, filepath.Join(root, "c", "c.go"), "package c\n\nimport \"example.com/m/a\"\n\nfunc C() { a.A() }\n")

	res := cyclesOf(t, root)
	if res.Count != 1 {
		t.Fatalf("Count = %d, want 1; cycles: %+v", res.Count, res.Cycles)
	}
	got := res.Cycles[0]
	if got.Length != 3 {
		t.Errorf("cycle length = %d, want 3", got.Length)
	}
	want := []string{"example.com/m/a", "example.com/m/b", "example.com/m/c"}
	if len(got.Nodes) != 3 || got.Nodes[0] != want[0] || got.Nodes[1] != want[1] || got.Nodes[2] != want[2] {
		t.Errorf("cycle nodes = %v, want %v (sorted)", got.Nodes, want)
	}
}

// TestCycles_Acyclic: a → b, a → c, b → c (a DAG) has no cycles.
func TestCycles_Acyclic(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	for _, d := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nimport (\n\t\"example.com/m/b\"\n\t\"example.com/m/c\"\n)\n\nfunc A() { b.B(); c.C() }\n")
	mustWriteFile(t, filepath.Join(root, "b", "b.go"), "package b\n\nimport \"example.com/m/c\"\n\nfunc B() { c.C() }\n")
	mustWriteFile(t, filepath.Join(root, "c", "c.go"), "package c\n\nfunc C() {}\n")

	res := cyclesOf(t, root)
	if res.Count != 0 {
		t.Errorf("acyclic graph reported %d cycles: %+v", res.Count, res.Cycles)
	}
}

// TestCycles_TwoNode: a → b → a is one 2-node cycle.
func TestCycles_TwoNode(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/m\n\ngo 1.26\n")
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWriteFile(t, filepath.Join(root, "a", "a.go"), "package a\n\nimport \"example.com/m/b\"\n\nfunc A() { b.B() }\n")
	mustWriteFile(t, filepath.Join(root, "b", "b.go"), "package b\n\nimport \"example.com/m/a\"\n\nfunc B() { a.A() }\n")

	res := cyclesOf(t, root)
	if res.Count != 1 || res.Cycles[0].Length != 2 {
		t.Fatalf("want one 2-node cycle, got %+v", res.Cycles)
	}
}

// TestCycles_RustCrates confirms cycle detection is multi-language: a Cargo
// workspace where crate a depends on b and b depends back on a.
func TestCycles_RustCrates(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Cargo.toml"), "[workspace]\nmembers = [\"a\", \"b\"]\n")
	for _, c := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, c, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(root, c, "Cargo.toml"), "[package]\nname = \""+c+"\"\nversion = \"0.1.0\"\n")
	}
	mustWriteFile(t, filepath.Join(root, "a", "src", "lib.rs"), "use b::go;\n\npub fn run() { go(); }\n")
	mustWriteFile(t, filepath.Join(root, "b", "src", "lib.rs"), "use a::run;\n\npub fn go() { run(); }\n")

	res := cyclesOf(t, root)
	if res.Count != 1 || res.Cycles[0].Length != 2 {
		t.Fatalf("want one 2-crate cycle (a <-> b), got %+v", res.Cycles)
	}
	if res.Cycles[0].Nodes[0] != "a" || res.Cycles[0].Nodes[1] != "b" {
		t.Errorf("cycle nodes = %v, want [a b]", res.Cycles[0].Nodes)
	}
}
