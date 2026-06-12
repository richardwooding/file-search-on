package search_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestCodeGraph_Impact(t *testing.T) {
	dir := t.TempDir()
	// Call chain: top -> mid -> leaf. unrelated -> other (separate).
	// Plus a cycle: recA <-> recB, to prove the visited set terminates it.
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package p\n\n"+
		"func leaf() {}\n"+
		"func mid() { leaf() }\n"+
		"func top() { mid() }\n"+
		"func unrelated() { other() }\n"+
		"func other() {}\n"+
		"func recA() { recB() }\n"+
		"func recB() { recA() }\n")

	g := mustBuildGraph(t, dir)

	byName := func(deps []search.ImpactNode) map[string]int {
		m := map[string]int{}
		for _, d := range deps {
			m[d.Symbol] = d.Depth
		}
		return m
	}

	// Full closure of leaf: mid (direct) + top (transitive).
	deps := byName(g.Impact("leaf", 0))
	if len(deps) != 2 || deps["mid"] != 1 || deps["top"] != 2 {
		t.Errorf("Impact(leaf) = %v, want {mid:1, top:2}", deps)
	}
	if _, ok := deps["unrelated"]; ok {
		t.Errorf("Impact(leaf) leaked an unrelated function: %v", deps)
	}

	// max_depth=1 stops at direct callers.
	d1 := byName(g.Impact("leaf", 1))
	if len(d1) != 1 || d1["mid"] != 1 {
		t.Errorf("Impact(leaf, maxDepth=1) = %v, want {mid:1}", d1)
	}

	// A cycle must terminate and report the other node once.
	cyc := byName(g.Impact("recA", 0))
	if cyc["recB"] != 1 {
		t.Errorf("Impact(recA) = %v, want recB at depth 1 (cycle must terminate)", cyc)
	}
	if _, ok := cyc["recA"]; ok {
		t.Errorf("a function must not appear as its own dependent: %v", cyc)
	}

	// Defining path is attached.
	for _, n := range g.Impact("leaf", 0) {
		if len(n.Paths) == 0 {
			t.Errorf("%s has no defining path", n.Symbol)
		}
	}
}
