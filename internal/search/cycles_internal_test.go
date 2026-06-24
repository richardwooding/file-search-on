package search

import (
	"sort"
	"testing"
)

// TestTarjanSCC checks the SCC algorithm directly on a hand-built graph with
// two disjoint cycles and an acyclic tail, independent of the walk/extractor.
func TestTarjanSCC(t *testing.T) {
	edge := func(pairs ...[2]string) map[string]map[string]bool {
		adj := map[string]map[string]bool{}
		touch := func(n string) {
			if adj[n] == nil {
				adj[n] = map[string]bool{}
			}
		}
		for _, p := range pairs {
			touch(p[0])
			touch(p[1])
			adj[p[0]][p[1]] = true
		}
		return adj
	}
	// Cycle1: a→b→a. Cycle2: x→y→z→x. DAG: d→a (d not in any cycle), z→d? no.
	adj := edge(
		[2]string{"a", "b"}, [2]string{"b", "a"},
		[2]string{"x", "y"}, [2]string{"y", "z"}, [2]string{"z", "x"},
		[2]string{"d", "a"}, // d depends into cycle1 but isn't part of it
	)

	var multi [][]string
	for _, comp := range tarjanSCC(adj) {
		if len(comp) > 1 {
			sort.Strings(comp)
			multi = append(multi, comp)
		}
	}
	// Expect exactly two multi-node SCCs: {a,b} and {x,y,z}.
	if len(multi) != 2 {
		t.Fatalf("got %d multi-node SCCs, want 2: %v", len(multi), multi)
	}
	sort.Slice(multi, func(i, j int) bool { return multi[i][0] < multi[j][0] })
	ab, xyz := multi[0], multi[1]
	if len(ab) != 2 || ab[0] != "a" || ab[1] != "b" {
		t.Errorf("SCC #1 = %v, want [a b]", ab)
	}
	if len(xyz) != 3 || xyz[0] != "x" || xyz[1] != "y" || xyz[2] != "z" {
		t.Errorf("SCC #2 = %v, want [x y z]", xyz)
	}
}
