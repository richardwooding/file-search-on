package search

import (
	"context"

	"github.com/richardwooding/file-search-on/internal/goresolve"
)

// ResolveGoDead computes precise dead Go functions/methods via type
// resolution (goresolve → go/packages), returned as SymbolDefs. ok is false
// when resolution can't run here (no `go` toolchain, not a buildable Go
// module, or no packages loaded) so the caller keeps the name-based result.
//
// This is the opt-in --resolve / resolve:true accuracy path (#447): it
// distinguishes same-named methods on different types and counts
// cross-package usage, eliminating the same-name over-matching the
// name-based dead_code suffers. Dev-environment only (needs the toolchain);
// callers degrade gracefully when ok is false.
func ResolveGoDead(ctx context.Context, root string) ([]SymbolDef, bool, error) {
	res, ok, err := goresolve.Resolve(ctx, root)
	if err != nil || !ok {
		return nil, false, err
	}
	dead := make([]SymbolDef, 0, len(res.DeadFuncs()))
	for _, s := range res.DeadFuncs() {
		dead = append(dead, SymbolDef{
			Path:     s.Path,
			Language: "go",
			Kind:     "function",
			Symbol:   s.Name,
			Owner:    s.Owner,
		})
	}
	return dead, true, nil
}

// MergeResolvedGoDead replaces the Go entries of a name-based dead-code
// result with the precise resolved set, leaving other languages untouched.
// Used when --resolve is on and resolution succeeded.
func MergeResolvedGoDead(nameBased, resolvedGo []SymbolDef) []SymbolDef {
	out := make([]SymbolDef, 0, len(nameBased)+len(resolvedGo))
	for _, d := range nameBased {
		if d.Language != "go" {
			out = append(out, d)
		}
	}
	return append(out, resolvedGo...)
}
