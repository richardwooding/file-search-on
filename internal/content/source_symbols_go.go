package content

import (
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
func extractGoSymbols(src []byte) (functions, types, imports, references []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if f == nil {
		_ = err
		return nil, nil, nil, nil
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name != nil {
				functions = append(functions, d.Name.Name)
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
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			references = append(references, fn.Name)
		case *ast.SelectorExpr:
			if fn.Sel != nil {
				references = append(references, fn.Sel.Name)
			}
		}
		return true
	})
	return functions, types, imports, dedupeStrings(references)
}
