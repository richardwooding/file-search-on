package search

import (
	"context"

	"github.com/richardwooding/file-search-on/internal/content"
)

// Cycle is one circular dependency — a strongly-connected component of size
// > 1 in the first-party import graph: a set of packages / crates / namespaces
// / directory-modules that mutually (transitively) depend on each other. Nodes
// is the sorted member SET, not an edge-ordered path.
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
// project root — strongly-connected components larger than one node in the
// directed import graph (Tarjan, in go-coupling). Language selection and node
// rules mirror Coupling (the manifest at opts.Root picks the ecosystem), so
// cycle detection is multi-language for free. Cycles are ranked largest-first,
// then alphabetically by their first node. An empty report (Module == "") is
// returned when the root carries no recognised manifest.
func Cycles(ctx context.Context, opts Options, registry *content.Registry) (*CyclesResult, error) {
	ig, err := buildImportGraph(ctx, opts, registry)
	res := &CyclesResult{
		Module:             ig.module,
		Cycles:             []Cycle{},
		Cancelled:          ig.cancelled,
		CancellationReason: ig.reason,
	}
	for _, c := range ig.graph.Cycles() {
		res.Cycles = append(res.Cycles, Cycle{Nodes: c.Nodes, Length: c.Length})
	}
	res.Count = len(res.Cycles)
	return res, err
}
