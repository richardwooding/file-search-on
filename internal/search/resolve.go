package search

import (
	"context"
	"strings"

	"github.com/richardwooding/file-search-on/internal/goresolve"
)

// splitQualified parses a who_calls/impact query symbol: "Owner.Method"
// scopes to that type's method; a bare "Name" matches any owner (a function
// and all same-named methods, each still resolved per call site).
func splitQualified(symbol string) (owner, name string) {
	if i := strings.LastIndexByte(symbol, '.'); i > 0 {
		return symbol[:i], symbol[i+1:]
	}
	return "", symbol
}

// ResolveGoWhoCalls returns the precise caller files of a Go symbol via type
// resolution — only callers of the EXACT symbol (e.g. Buffer.String, not
// every "String"). ok is false when resolution can't run (degrade to
// name-based). Issue #447.
func ResolveGoWhoCalls(ctx context.Context, root, symbol string) ([]Importer, bool, error) {
	res, ok, err := goresolve.Resolve(ctx, root)
	if err != nil || !ok {
		return nil, false, err
	}
	owner, name := splitQualified(symbol)
	seen := map[string]bool{}
	var out []Importer
	for _, s := range res.Callers(owner, name) {
		if seen[s.Path] {
			continue
		}
		seen[s.Path] = true
		out = append(out, Importer{Path: s.Path, Language: "go"})
	}
	return out, true, nil
}

// ResolveGoImpact returns the precise transitive caller closure of a Go
// symbol as ImpactNodes. ok is false when resolution can't run.
func ResolveGoImpact(ctx context.Context, root, symbol string) ([]ImpactNode, bool, error) {
	res, ok, err := goresolve.Resolve(ctx, root)
	if err != nil || !ok {
		return nil, false, err
	}
	owner, name := splitQualified(symbol)
	var out []ImpactNode
	for _, s := range res.Impact(owner, name) {
		out = append(out, ImpactNode{Symbol: s.Qualified(), Depth: s.Depth, Paths: []string{s.Path}})
	}
	return out, true, nil
}

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
