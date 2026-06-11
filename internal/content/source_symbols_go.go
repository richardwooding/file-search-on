package content

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// extractGoSymbols parses Go source via the stdlib AST and returns
// (functions, types, imports). The Go path is the gold standard: free,
// no new dependencies, and handles every Go syntactic edge case
// (grouped imports, type aliases, methods on receivers, generics)
// correctly.
//
// Parse errors are non-fatal — the parser produces a partial AST on
// broken files and partial extraction is better than nothing.
// Specifically, if the parser couldn't recover anything (f == nil),
// returns empty slices.
//
// Both top-level funcs and receiver-bound methods land in functions —
// agents asking "where is FooBar?" want both shapes, and capturing the
// method name only (not the receiver-qualified name) keeps CEL
// queries like `"FooBar" in functions` simple.
//
// references holds the bare callee names of every call expression
// (`foo()` → "foo"; `pkg.Foo()` / `x.Method()` → "Foo" / "Method") —
// the call-site half of the code graph (issue #363). Name-based, deduped.
//
// callEdges holds per-function call attribution as "caller\x00callee"
// pairs (issue #368): for each top-level FuncDecl, every callee in its
// body (including nested closures). Powers the calls() tool. Builder-
// internal — not a CEL variable.
//
// complexityRows holds per-function metrics as
// "func\x00complexity\x00startLine\x00endLine" (issue #364): gocyclo-style
// cyclomatic complexity (1 + branch points) + the line span. Builder-
// internal, like callEdges.
func extractGoSymbols(src []byte) (functions, types, imports, references, callEdges, complexityRows []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if f == nil {
		_ = err
		return nil, nil, nil, nil, nil, nil
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil {
				functions = append(functions, d.Name.Name)
				if d.Body != nil {
					for _, callee := range goCallees(d.Body) {
						callEdges = append(callEdges, d.Name.Name+"\x00"+callee)
					}
					cx := goComplexity(d.Body)
					start := fset.Position(d.Pos()).Line
					end := fset.Position(d.End()).Line
					complexityRows = append(complexityRows,
						fmt.Sprintf("%s\x00%d\x00%d\x00%d", d.Name.Name, cx, start, end))
				}
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if s.Name != nil {
						types = append(types, s.Name.Name)
					}
				case *ast.ImportSpec:
					if s.Path != nil {
						imports = append(imports, strings.Trim(s.Path.Value, `"`))
					}
				}
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if name := goCallee(call); name != "" {
				references = append(references, name)
			}
		}
		return true
	})
	return functions, types, imports, dedupeStrings(references), dedupeStrings(callEdges), complexityRows
}

// goComplexity returns the cyclomatic complexity of a function body —
// gocyclo's definition: 1 + one per branch point (if / for / range /
// case / comm-clause / && / ||).
func goComplexity(body *ast.BlockStmt) int {
	cx := 1
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause, *ast.CommClause:
			cx++
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				cx++
			}
		}
		return true
	})
	return cx
}

// goCallee returns the bare callee name of a call expression, or "" for
// shapes without a simple name (e.g. calls through a returned func value).
func goCallee(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if fn.Sel != nil {
			return fn.Sel.Name
		}
	}
	return ""
}

// goCallees returns every callee name reached from node (deduped).
func goCallees(node ast.Node) []string {
	var out []string
	ast.Inspect(node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if name := goCallee(call); name != "" {
				out = append(out, name)
			}
		}
		return true
	})
	return dedupeStrings(out)
}
