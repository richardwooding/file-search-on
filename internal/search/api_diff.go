package search

import (
	"context"
	"sort"
	"unicode"
	"unicode/utf8"

	"github.com/richardwooding/file-search-on/internal/content"
)

// APISymbol is one exported definition (a function or type) in an API diff.
type APISymbol struct {
	Symbol string `json:"symbol"`
	Kind   string `json:"kind"` // "function" | "type"
}

// APIDiffResult is the exported-symbol delta between two trees (issue #406).
// Removed (present in A, gone in B) is the breaking set; Added is new public
// surface. A signature change isn't detected at this granularity (v1 is
// name+kind presence) — but a kind change (func → type for the same name)
// shows correctly as that name removed under one kind and added under the
// other.
type APIDiffResult struct {
	Removed      []APISymbol `json:"removed"`
	Added        []APISymbol `json:"added"`
	Breaking     bool        `json:"breaking"`
	RemovedCount int         `json:"removed_count"`
	AddedCount   int         `json:"added_count"`
	ExportedA    int         `json:"exported_a"`
	ExportedB    int         `json:"exported_b"`
}

// APIDiff compares the EXPORTED function/type surface of two trees (treeA =
// the baseline, treeB = the candidate) and reports what was removed or added
// — a pre-release breaking-change gate, valuable for a repo that ships
// libraries. "Exported" = an upper-cased first rune (Go's rule; a heuristic
// for languages whose visibility isn't case-based). v1 is name+kind presence,
// not signatures.
func APIDiff(ctx context.Context, treeA, treeB Options, registry *content.Registry) (*APIDiffResult, error) {
	ga, err := BuildCodeGraph(ctx, treeA, registry)
	if err != nil {
		return nil, err
	}
	gb, err := BuildCodeGraph(ctx, treeB, registry)
	if err != nil {
		return nil, err
	}
	a, b := exportedSymbols(ga), exportedSymbols(gb)

	out := &APIDiffResult{ExportedA: countSymbols(a), ExportedB: countSymbols(b)}
	for name, kinds := range a {
		for kind := range kinds {
			if !b[name][kind] {
				out.Removed = append(out.Removed, APISymbol{Symbol: name, Kind: kind})
			}
		}
	}
	for name, kinds := range b {
		for kind := range kinds {
			if !a[name][kind] {
				out.Added = append(out.Added, APISymbol{Symbol: name, Kind: kind})
			}
		}
	}
	sortAPISymbols(out.Removed)
	sortAPISymbols(out.Added)
	out.RemovedCount = len(out.Removed)
	out.AddedCount = len(out.Added)
	out.Breaking = out.RemovedCount > 0
	return out, nil
}

// exportedSymbols returns a graph's exported function/type definitions as
// name -> set of kinds.
func exportedSymbols(g *CodeGraph) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for name, entries := range g.definedIn {
		if !isExportedName(name) {
			continue
		}
		for _, e := range entries {
			if e.kind != "function" && e.kind != "type" {
				continue
			}
			if out[name] == nil {
				out[name] = map[string]bool{}
			}
			out[name][e.kind] = true
		}
	}
	return out
}

func countSymbols(m map[string]map[string]bool) int {
	n := 0
	for _, kinds := range m {
		n += len(kinds)
	}
	return n
}

// isExportedName reports whether name's first rune is upper-case (Go's export
// convention; a heuristic elsewhere).
func isExportedName(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

func sortAPISymbols(s []APISymbol) {
	sort.Slice(s, func(i, j int) bool {
		if s[i].Symbol != s[j].Symbol {
			return s[i].Symbol < s[j].Symbol
		}
		return s[i].Kind < s[j].Kind
	})
}
