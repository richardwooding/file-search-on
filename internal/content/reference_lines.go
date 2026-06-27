package content

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"

	tssymbols "github.com/richardwooding/treesitter-symbols"
)

// ReferenceSite is one occurrence of a symbol reference within a file, at a
// 1-based line, tagged by how the symbol is used (issue #408).
type ReferenceSite struct {
	Line int    `json:"line"`
	Kind string `json:"kind"` // "call" | "type" | "value"
}

// ReferenceLines returns every line in src where `symbol` is referenced —
// called, used as a type, or (Go) passed as a value — for the given
// language. It is the positional, on-demand counterpart to the name-level
// reference extraction the code graph caches: a `references` tool resolves
// which files reference a symbol from the graph, then calls this per file to
// pinpoint the lines. Returns nil for unsupported languages or a symbol that
// never appears. Sites are deduped by (line, kind) and sorted by line.
func ReferenceLines(language string, src []byte, symbol string) []ReferenceSite {
	if symbol == "" || len(src) == 0 {
		return nil
	}
	if language == "go" {
		return goReferenceLines(src, symbol)
	}
	return tsReferenceLines(language, src, symbol)
}

func goReferenceLines(src []byte, symbol string) []ReferenceSite {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if f == nil {
		_ = err
		return nil
	}
	var out []ReferenceSite
	lineOf := func(p token.Pos) int { return fset.Position(p).Line }
	add := func(p token.Pos, kind string) { out = append(out, ReferenceSite{Line: lineOf(p), Kind: kind}) }

	var collectType func(expr ast.Expr)
	collectType = func(expr ast.Expr) {
		switch t := expr.(type) {
		case *ast.Ident:
			if t.Name == symbol {
				add(t.Pos(), "type")
			}
		case *ast.SelectorExpr:
			if t.Sel != nil && t.Sel.Name == symbol {
				add(t.Sel.Pos(), "type")
			}
		case *ast.StarExpr:
			collectType(t.X)
		case *ast.ArrayType:
			collectType(t.Elt)
		case *ast.Ellipsis:
			collectType(t.Elt)
		case *ast.MapType:
			collectType(t.Key)
			collectType(t.Value)
		case *ast.ChanType:
			collectType(t.Value)
		case *ast.ParenExpr:
			collectType(t.X)
		case *ast.IndexExpr:
			collectType(t.X)
			collectType(t.Index)
		case *ast.IndexListExpr:
			collectType(t.X)
			for _, idx := range t.Indices {
				collectType(idx)
			}
		}
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if goCallee(x) == symbol {
				add(x.Fun.Pos(), "call")
			}
			for _, arg := range x.Args {
				switch a := arg.(type) {
				case *ast.Ident:
					if a.Name == symbol {
						add(a.Pos(), "value")
					}
				case *ast.SelectorExpr:
					if a.Sel != nil && a.Sel.Name == symbol {
						add(a.Sel.Pos(), "value")
					}
				}
			}
		case *ast.Field:
			collectType(x.Type)
		case *ast.ValueSpec:
			collectType(x.Type)
		case *ast.TypeSpec:
			collectType(x.Type)
		case *ast.CompositeLit:
			collectType(x.Type)
		case *ast.TypeAssertExpr:
			collectType(x.Type)
		}
		return true
	})
	return dedupeSites(out)
}

func tsReferenceLines(language string, src []byte, symbol string) []ReferenceSite {
	sites := tssymbols.ReferenceSites(language, src, symbol)
	if len(sites) == 0 {
		return nil
	}
	out := make([]ReferenceSite, 0, len(sites))
	for _, s := range sites {
		out = append(out, ReferenceSite{Line: s.Line, Kind: s.Kind})
	}
	return dedupeSites(out)
}

// dedupeSites removes duplicate (line, kind) pairs and sorts by line then
// kind, so a symbol called and used as a type on the same line surfaces both
// kinds once.
func dedupeSites(sites []ReferenceSite) []ReferenceSite {
	if len(sites) == 0 {
		return nil
	}
	seen := make(map[ReferenceSite]struct{}, len(sites))
	out := sites[:0]
	for _, s := range sites {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
