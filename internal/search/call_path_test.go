package search_test

import (
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestCodeGraph_CallPath(t *testing.T) {
	dir := t.TempDir()
	// Chain a -> b -> c -> d; island() is unreachable from a.
	mustWriteFile(t, filepath.Join(dir, "a.go"), "package p\n\n"+
		"func a() { b() }\n"+
		"func b() { c() }\n"+
		"func c() { d() }\n"+
		"func d() {}\n"+
		"func island() {}\n")

	g := mustBuildGraph(t, dir)

	names := func(steps []search.CallPathStep) []string {
		out := make([]string, len(steps))
		for i, s := range steps {
			out[i] = s.Symbol
		}
		return out
	}

	got := names(g.CallPath("a", "d", 0))
	want := []string{"a", "b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("CallPath(a,d) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CallPath(a,d) = %v, want %v", got, want)
		}
	}

	if p := g.CallPath("a", "island", 0); p != nil {
		t.Errorf("island is unreachable from a, want nil, got %v", names(p))
	}
	if p := g.CallPath("a", "a", 0); len(p) != 1 || p[0].Symbol != "a" {
		t.Errorf("CallPath(a,a) = %v, want [a]", names(p))
	}
	// d is 3 hops from a; max_depth 2 can't reach it.
	if p := g.CallPath("a", "d", 2); p != nil {
		t.Errorf("CallPath(a,d,maxDepth=2) should not reach d (3 hops), got %v", names(p))
	}
	// Defining path attached.
	for _, s := range g.CallPath("a", "d", 0) {
		if len(s.Paths) == 0 {
			t.Errorf("step %s has no defining path", s.Symbol)
		}
	}
}
