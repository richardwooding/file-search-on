package search

import (
	"context"
	"os"
	"sort"

	"github.com/richardwooding/file-search-on/internal/content"
)

// ReferenceSite is one usage of a symbol — a file + 1-based line + how the
// symbol is used (issue #408).
type ReferenceSite struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"`     // "call" | "type" | "value"
	Language string `json:"language"` // language of the referencing file
}

// ReferencesResult is the find-all-usages report for a symbol.
type ReferencesResult struct {
	Symbol string `json:"symbol"`
	// DefinedOn lists the types the queried symbol is a method on (#445),
	// when any — a disambiguation hint that these name-based results may
	// mix usages of same-named methods on different types. Go only for now.
	DefinedOn          []string        `json:"defined_on,omitempty"`
	References         []ReferenceSite `json:"references"`
	Count              int             `json:"count"`
	TotalFiles         int64           `json:"total_files"`
	Cancelled          bool            `json:"cancelled,omitempty"`
	CancellationReason string          `json:"cancellation_reason,omitempty"`
}

// References finds every usage of a symbol — calls, type usages, and (Go)
// value passing — each with file + line, the canonical "find references" IDE
// primitive and the complement to find_definition (issue #408). It uses the
// code graph's name→file reference index to pre-filter to just the files
// that reference the symbol (cheap on a warm index), then parses only those
// to pinpoint the exact lines, so cost scales with usages, not tree size.
//
// kind ("call" / "type" / "value") filters the sites; empty returns all.
// Coverage follows the reference graph: Go + the tree-sitter languages for
// calls / type usages, Go-only for value passing. Heuristic and name-based,
// like who_calls — same-name symbols across packages collapse together.
func References(ctx context.Context, opts Options, symbol, kind string, registry *content.Registry) (*ReferencesResult, error) {
	g, err := BuildCodeGraph(ctx, opts, registry)
	if err != nil {
		return nil, err
	}
	res := &ReferencesResult{
		Symbol:             symbol,
		References:         []ReferenceSite{},
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	if symbol == "" {
		return res, nil
	}
	res.DefinedOn = g.OwnersOf(symbol)
	for _, c := range g.WhoCalls(symbol) {
		if err := ctx.Err(); err != nil {
			res.Cancelled = true
			res.CancellationReason = "client_cancel"
			break
		}
		src, err := os.ReadFile(c.Path)
		if err != nil {
			continue // file vanished / unreadable since the walk — skip
		}
		for _, s := range content.ReferenceLines(c.Language, src, symbol) {
			if kind != "" && s.Kind != kind {
				continue
			}
			res.References = append(res.References, ReferenceSite{
				Path:     c.Path,
				Line:     s.Line,
				Kind:     s.Kind,
				Language: c.Language,
			})
		}
	}
	sort.Slice(res.References, func(i, j int) bool {
		a, b := res.References[i], res.References[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Kind < b.Kind
	})
	res.Count = len(res.References)
	return res, nil
}
