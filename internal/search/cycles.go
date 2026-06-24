package search

import (
	"context"
	"sort"

	"github.com/richardwooding/file-search-on/internal/content"
)

// Cycle is one circular dependency — a strongly-connected component of size
// > 1 in the first-party import graph: a set of packages / crates / namespaces
// / directory-modules that mutually (transitively) depend on each other. A
// size-2 cycle is a direct a↔b pair; a larger one is a tangled group that
// contains at least one cycle through all its members. Nodes is the sorted
// member SET, not an edge-ordered path — there's no single canonical loop
// order, so consumers should treat it as a group rather than a sequence.
type Cycle struct {
	Nodes  []string `json:"nodes"`
	Length int      `json:"length"`
}

// CyclesResult is the circular-dependency report (issue #481).
type CyclesResult struct {
	Module             string  `json:"module"`
	Cycles             []Cycle `json:"cycles"`
	Count              int     `json:"count"`
	Cancelled          bool    `json:"cancelled,omitempty"`
	CancellationReason string  `json:"cancellation_reason,omitempty"`
}

// Cycles detects circular dependencies among the first-party nodes under the
// project root by finding strongly-connected components (Tarjan) larger than
// one node in the directed import graph. Language selection and node rules
// mirror Coupling — the manifest at opts.Root picks the adapter (go.mod ⇒ Go
// packages, Cargo.toml ⇒ Rust crates, plus JVM / C# / Python / JS-TS / PHP,
// #467) — so cycle detection is multi-language for free. An empty report
// (Module == "") is returned when the root carries no recognised manifest.
//
// Cycles are ranked largest-first (the broadest tangles), then alphabetically
// by their first node. Self-edges aren't reported: the import-graph builder
// drops a node importing itself, so size-1 SCCs are never cycles.
func Cycles(ctx context.Context, opts Options, registry *content.Registry) (*CyclesResult, error) {
	g, err := buildImportGraph(ctx, opts, registry)
	res := &CyclesResult{
		Module:             g.module,
		Cycles:             []Cycle{},
		Cancelled:          g.cancelled,
		CancellationReason: g.reason,
	}

	for _, comp := range tarjanSCC(g.efferent) {
		if len(comp) < 2 {
			continue // a single node is not a cycle (self-edges are filtered upstream)
		}
		sort.Strings(comp)
		res.Cycles = append(res.Cycles, Cycle{Nodes: comp, Length: len(comp)})
	}
	sort.Slice(res.Cycles, func(i, j int) bool {
		if res.Cycles[i].Length != res.Cycles[j].Length {
			return res.Cycles[i].Length > res.Cycles[j].Length
		}
		return res.Cycles[i].Nodes[0] < res.Cycles[j].Nodes[0]
	})
	res.Count = len(res.Cycles)
	return res, err
}

// tarjanSCC returns the strongly-connected components of a directed graph
// given as an adjacency map (node → set of successors). Iteration over nodes
// and successors is sorted, so the output is deterministic. Every successor
// is assumed to be a key in adj (the import-graph builder touches every node),
// so there are no missing-vertex edge cases. Import graphs are small (one node
// per package/crate/module), so the recursion depth is bounded in practice.
func tarjanSCC(adj map[string]map[string]bool) [][]string {
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	successors := func(n string) []string {
		s := make([]string, 0, len(adj[n]))
		for m := range adj[n] {
			s = append(s, m)
		}
		sort.Strings(s)
		return s
	}

	index := make(map[string]int, len(nodes))
	lowlink := make(map[string]int, len(nodes))
	onStack := make(map[string]bool, len(nodes))
	var stack []string
	var counter int
	var out [][]string

	var strongConnect func(v string)
	strongConnect = func(v string) {
		index[v] = counter
		lowlink[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range successors(v) {
			if _, seen := index[w]; !seen {
				strongConnect(w)
				if lowlink[w] < lowlink[v] {
					lowlink[v] = lowlink[w]
				}
			} else if onStack[w] {
				if index[w] < lowlink[v] {
					lowlink[v] = index[w]
				}
			}
		}

		if lowlink[v] == index[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			out = append(out, comp)
		}
	}

	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			strongConnect(n)
		}
	}
	return out
}
