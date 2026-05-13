package search_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalk_PruneBuildArtefacts verifies that the canonical build-
// artefact basenames for every detected project type are
// automatically unioned into Excludes when PruneBuildArtefacts is
// set — and that the same walk without the flag visits everything.
func TestWalk_PruneBuildArtefacts(t *testing.T) {
	root := t.TempDir()

	// A Go project — go.mod declares the type.
	goProj := filepath.Join(root, "myapp")
	mustMkdir(t, goProj)
	mustWriteFile(t, filepath.Join(goProj, "go.mod"), "module example.com/x\n")
	mustWriteFile(t, filepath.Join(goProj, "main.go"), "package main\n")
	// vendor/ is what we want pruned. Drop a .go file inside so
	// the search expression matches it (it would otherwise be
	// invisible).
	vendor := filepath.Join(goProj, "vendor", "github.com", "foo")
	mustMkdir(t, vendor)
	mustWriteFile(t, filepath.Join(vendor, "vendored.go"), "package foo\n")

	// A sibling Node project under root with node_modules.
	nodeProj := filepath.Join(root, "frontend")
	mustMkdir(t, nodeProj)
	mustWriteFile(t, filepath.Join(nodeProj, "package.json"), `{"name":"frontend"}`)
	mustWriteFile(t, filepath.Join(nodeProj, "app.go"), "package main\n") // contrived
	nm := filepath.Join(nodeProj, "node_modules", "react")
	mustMkdir(t, nm)
	mustWriteFile(t, filepath.Join(nm, "react.go"), "package react\n")

	reg := content.DefaultRegistry()

	// Without the flag: all 4 .go files visible.
	all, err := search.Walk(t.Context(), search.Options{
		Root: root,
		Expr: `is_source && language == "go"`,
	}, reg)
	if err != nil {
		t.Fatalf("Walk (baseline): %v", err)
	}
	if len(all) != 4 {
		t.Errorf("baseline walk: got %d matches, want 4; paths=%v", len(all), pathsOf(all))
	}

	// With the flag: vendor/ + node_modules/ pruned; only the
	// two top-level .go files remain.
	pruned, err := search.Walk(t.Context(), search.Options{
		Root:                root,
		Expr:                `is_source && language == "go"`,
		PruneBuildArtefacts: true,
	}, reg)
	if err != nil {
		t.Fatalf("Walk (pruned): %v", err)
	}
	if len(pruned) != 2 {
		t.Errorf("pruned walk: got %d matches, want 2; paths=%v", len(pruned), pathsOf(pruned))
	}
	for _, r := range pruned {
		if filepath.Base(filepath.Dir(r.Path)) == "foo" || filepath.Base(filepath.Dir(r.Path)) == "react" {
			t.Errorf("pruned walk should not include %q (under vendor / node_modules)", r.Path)
		}
	}
}

// TestWalk_PruneCombinesWithUserExcludes verifies the prune list is
// unioned with the user's explicit --exclude, not replacing it.
func TestWalk_PruneCombinesWithUserExcludes(t *testing.T) {
	root := t.TempDir()
	goProj := filepath.Join(root, "app")
	mustMkdir(t, goProj)
	mustWriteFile(t, filepath.Join(goProj, "go.mod"), "module x\n")
	mustWriteFile(t, filepath.Join(goProj, "main.go"), "package main\n")
	// vendor/ pruned by --prune-build-artefacts.
	mustMkdir(t, filepath.Join(goProj, "vendor"))
	mustWriteFile(t, filepath.Join(goProj, "vendor", "v.go"), "package x\n")
	// generated/ pruned by user --exclude.
	mustMkdir(t, filepath.Join(goProj, "generated"))
	mustWriteFile(t, filepath.Join(goProj, "generated", "g.go"), "package x\n")

	pruned, err := search.Walk(t.Context(), search.Options{
		Root:                root,
		Expr:                `is_source && language == "go"`,
		Excludes:            []string{"generated"},
		PruneBuildArtefacts: true,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(pruned) != 1 {
		t.Errorf("got %d matches, want 1 (only main.go); paths=%v", len(pruned), pathsOf(pruned))
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func pathsOf(rs []search.Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Path
	}
	return out
}
