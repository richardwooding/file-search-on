package content

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/richardwooding/codemetrics"
)

// goExportedName reports whether name's first rune is upper-case — Go's
// export convention. Mirrors search.isExportedName (kept local so the stable
// content package doesn't import search).
func goExportedName(name string) bool {
	if name == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

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
// references holds the bare names of every symbol the file *uses* — both
// call sites (`foo()` → "foo"; `pkg.Foo()` / `x.Method()` → "Foo" /
// "Method"; issue #363) AND type usages (a type named as a field type,
// parameter / result, composite-literal type, var/const type, embedding,
// type assertion, or generic argument; issue #398). Name-based, deduped.
// Type usages are what let dead_code see a type used only as a field type
// as "referenced" rather than a false positive, and are the basis for a
// true all-usages references tool. The call graph (callEdges) stays
// call-only — type usages never become call edges.
//
// callEdges holds per-function call attribution as "caller\x00callee"
// pairs (issue #368): for each top-level FuncDecl, every callee in its
// body (including nested closures). Powers the calls() tool. Builder-
// internal — not a CEL variable.
//
// complexityRows holds per-function metrics as
// "func\x00complexity\x00startLine\x00endLine\x00cognitive" (issue #364, #485):
// gocyclo-style cyclomatic complexity (1 + branch points), the line span, and
// SonarSource cognitive complexity. Builder-internal, like callEdges. The
// trailing cognitive field is Go-only today; the tree-sitter extractor emits
// the 4-field form, so consumers treat a missing 5th field as "unavailable".
func extractGoSymbols(src []byte) (functions, types, imports, references, callEdges, complexityRows, handlerBoundary []string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if f == nil {
		_ = err
		return nil, nil, nil, nil, nil, nil, nil
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name, edges, row := goFuncDeclSymbols(d, fset)
			if name != "" {
				functions = append(functions, name)
			}
			callEdges = append(callEdges, edges...)
			if row != "" {
				complexityRows = append(complexityRows, row)
			}
		case *ast.GenDecl:
			t, imp := goGenDeclSymbols(d)
			types = append(types, t...)
			imports = append(imports, imp...)
		}
	}
	references = append(references, goValueRefs(f)...)
	references = append(references, goCallRefs(f)...)
	references = append(references, goTypeRefs(f)...)
	return functions, types, imports, dedupeStrings(references), dedupeStrings(callEdges), complexityRows, goHandlerBoundary(f)
}

// goFuncDeclSymbols returns a FuncDecl's name, its call edges
// ("name\x00callee"), and its complexity row (issue #364, #485). name is empty
// for an unnamed decl; edges/row are empty for a body-less decl (external or
// forward declaration).
func goFuncDeclSymbols(d *ast.FuncDecl, fset *token.FileSet) (name string, callEdges []string, complexityRow string) {
	if d.Name == nil {
		return "", nil, ""
	}
	name = d.Name.Name
	if d.Body == nil {
		return name, nil, ""
	}
	for _, callee := range goCallees(d.Body) {
		callEdges = append(callEdges, name+"\x00"+callee)
	}
	cx := codemetrics.Cyclomatic(d.Body)
	cog := codemetrics.Cognitive(d)
	start := fset.Position(d.Pos()).Line
	end := fset.Position(d.End()).Line
	complexityRow = fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%d", name, cx, start, end, cog)
	return name, callEdges, complexityRow
}

// goGenDeclSymbols returns the type names and import paths declared by a
// GenDecl (a `type (...)` or `import (...)` group, or their single-spec forms).
func goGenDeclSymbols(d *ast.GenDecl) (types, imports []string) {
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
	return types, imports
}

// goCallRefs returns the bare callee name of every call site in the file
// (`foo()` → "foo"; `pkg.Foo()` / `x.Method()` → "Foo" / "Method"; #363).
func goCallRefs(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if name := goCallee(call); name != "" {
				out = append(out, name)
			}
		}
		return true
	})
	return out
}

// goPredeclared is the set of Go predeclared type names. They appear in
// type positions everywhere but never name a project-defined type, so
// goTypeRefs filters them to keep the reference set meaningful.
var goPredeclared = map[string]bool{
	"bool": true, "byte": true, "rune": true, "string": true, "error": true,
	"any": true, "comparable": true, "uintptr": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true, "complex64": true, "complex128": true,
}

// goTypeRefs collects the bare names of every type used in a type
// position across the file (issue #398): field types (which covers struct
// fields, function params/results, interface methods, and embeddings),
// var/const declared types, composite-literal types, type-assertion
// types, and the RHS of type definitions/aliases. Predeclared types are
// dropped. The names join `references` so a type used only as a field
// type counts as referenced.
func goTypeRefs(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			collectTypeIdents(x.Type, &out)
		case *ast.ValueSpec:
			collectTypeIdents(x.Type, &out)
		case *ast.TypeSpec:
			// `type Foo Bar` / `type Foo = Bar` — the RHS names a type.
			// struct / interface bodies are reached via their *ast.Field
			// children, so collectTypeIdents deliberately ignores them.
			collectTypeIdents(x.Type, &out)
		case *ast.CompositeLit:
			collectTypeIdents(x.Type, &out)
		case *ast.TypeAssertExpr:
			collectTypeIdents(x.Type, &out) // nil for the `x.(type)` switch guard
		}
		return true
	})
	return out
}

// collectTypeIdents appends the base type name(s) of a type expression to
// out, descending through pointers, slices/arrays, maps, channels,
// variadics, parens, and generic instantiations. Named types resolve to
// their bare name (`pkg.T` → "T", matching goCallee). Struct / interface /
// func type literals add nothing themselves — their members are visited as
// *ast.Field nodes by the caller's ast.Inspect.
func collectTypeIdents(expr ast.Expr, out *[]string) {
	// Leaf names go straight to out; composite types contribute their child
	// type expressions, which are walked in source order by the single
	// recursion below (preserving the original pre-order: X before Index, Key
	// before Value, etc.). Struct / interface / func type literals contribute
	// no children here — their members are visited as *ast.Field by the caller.
	var children []ast.Expr
	switch t := expr.(type) {
	case *ast.Ident:
		if !goPredeclared[t.Name] {
			*out = append(*out, t.Name)
		}
	case *ast.SelectorExpr:
		if t.Sel != nil {
			*out = append(*out, t.Sel.Name)
		}
	case *ast.StarExpr:
		children = []ast.Expr{t.X}
	case *ast.ArrayType:
		children = []ast.Expr{t.Elt}
	case *ast.Ellipsis:
		children = []ast.Expr{t.Elt}
	case *ast.MapType:
		children = []ast.Expr{t.Key, t.Value}
	case *ast.ChanType:
		children = []ast.Expr{t.Value}
	case *ast.ParenExpr:
		children = []ast.Expr{t.X}
	case *ast.IndexExpr: // generic instantiation Foo[T]
		children = []ast.Expr{t.X, t.Index}
	case *ast.IndexListExpr: // Foo[T, U]
		children = append([]ast.Expr{t.X}, t.Indices...)
	}
	for _, child := range children {
		collectTypeIdents(child, out)
	}
}

// goValueRefs captures function/method names used as VALUES — passed as a
// call argument (`AddTool(s, t, h.searchHandler)`) rather than called
// (#421). These join `references` so a handler registered via a callback is
// seen as used by dead_code / who_calls. Scoped to call-argument position
// to bound over-capture (non-function args named like a function still get
// captured, but that only adds harmless entries to the reference set).
func goValueRefs(f *ast.File) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, arg := range call.Args {
			switch a := arg.(type) {
			case *ast.Ident:
				out = append(out, a.Name)
			case *ast.SelectorExpr:
				if a.Sel != nil {
					out = append(out, a.Sel.Name)
				}
			}
		}
		return true
	})
	return out
}

// goHandlerBoundary extracts the export-pinning signals that spare symbols
// from the unused_exports "package-local" false positive. It returns three
// relations tag-encoded into one []string (mirroring the call_edges "\x00"
// convention) so they ride in a single attribute:
//
//	"v\x00<func>"           — <func> is passed as a VALUE to a call (a handler
//	                          registered via the AddTool / HandleFunc pattern, #421).
//	"s\x00<func>\x00<Type>" — exported <Type> appears in <func>'s signature
//	                          (a parameter or result type).
//	"i\x00<Method>"         — <Method> is declared on an interface (#505).
//	"p\x00external_test"    — this file is an external test package
//	                          (`package <pkg>_test`); its references are
//	                          cross-boundary (#511).
//
// The aggregator (search.UnusedExports) joins them:
//   - #504: an exported type in the signature of a function registered as a
//     value is bound to that external generic API — e.g. mcp.AddTool[In, Out]
//     infers In/Out from the handler signature — and must stay exported.
//   - #505: a method whose name is declared on a first-party interface can't
//     be unexported without breaking interface satisfaction.
//
// Both are exempt from the unexport-candidate list even though every textual
// reference sits inside the defining package.
//
// Only EXPORTED signature types are emitted (unused_exports only judges
// exported symbols), which bounds the output. Takes the AST already parsed by
// extractGoSymbols (no second parse); the result caches in the attribute
// index alongside the other symbol data, so the cost is paid once per
// (file, size, mtime).
func goHandlerBoundary(f *ast.File) []string {
	if f == nil {
		return nil
	}
	var out []string
	// External test package marker (#511): a `package <pkg>_test` file
	// IMPORTS the package under test, so a reference it makes to an exported
	// symbol is cross-boundary — exactly why that symbol is exported. The
	// aggregator attributes such references under a distinct package key so
	// the symbol isn't reported as a unexport candidate.
	if f.Name != nil && strings.HasSuffix(f.Name.Name, "_test") {
		out = append(out, "p\x00external_test")
	}
	for _, name := range goValueRefs(f) {
		out = append(out, "v\x00"+name)
	}
	out = append(out, goSignatureTypeSignals(f)...)
	out = append(out, goInterfaceMethodSignals(f)...)
	return dedupeStrings(out)
}

// goSignatureTypeSignals emits "s\x00<func>\x00<Type>" for each EXPORTED type
// named in a function's parameter or result list (#504). The aggregator pins
// such types as exported because an external generic API (e.g.
// mcp.AddTool[In, Out]) infers them from the handler signature.
func goSignatureTypeSignals(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Type == nil {
			continue
		}
		for _, t := range goExportedSignatureTypes(fn) {
			out = append(out, "s\x00"+fn.Name.Name+"\x00"+t)
		}
	}
	return out
}

// goExportedSignatureTypes returns the deduped exported type names appearing
// in fn's parameters and results, in source order (params before results).
func goExportedSignatureTypes(fn *ast.FuncDecl) []string {
	var sigTypes []string
	for _, fl := range []*ast.FieldList{fn.Type.Params, fn.Type.Results} {
		if fl == nil {
			continue
		}
		for _, field := range fl.List {
			if field != nil {
				collectTypeIdents(field.Type, &sigTypes)
			}
		}
	}
	var out []string
	for _, t := range dedupeStrings(sigTypes) {
		if goExportedName(t) {
			out = append(out, t)
		}
	}
	return out
}

// goInterfaceMethodSignals emits "i\x00<Method>" for each exported method name
// declared on a first-party interface (#505): such a method can't be
// unexported without breaking interface satisfaction.
func goInterfaceMethodSignals(f *ast.File) []string {
	var out []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			for _, nm := range goExportedMethodNames(interfaceType(spec)) {
				out = append(out, "i\x00"+nm)
			}
		}
	}
	return out
}

// interfaceType returns the *ast.InterfaceType a type spec defines (with a
// non-nil method list), or nil for any other spec.
func interfaceType(spec ast.Spec) *ast.InterfaceType {
	ts, ok := spec.(*ast.TypeSpec)
	if !ok {
		return nil
	}
	it, ok := ts.Type.(*ast.InterfaceType)
	if !ok || it.Methods == nil {
		return nil
	}
	return it
}

// goExportedMethodNames returns the exported method names declared directly on
// it (embedded interfaces carry no Names and contribute nothing). Unexported
// methods — sealed-interface markers like isNode() — are skipped because
// unused_exports judges only exported symbols. A nil interface yields nil.
func goExportedMethodNames(it *ast.InterfaceType) []string {
	if it == nil {
		return nil
	}
	var out []string
	for _, m := range it.Methods.List {
		if m == nil {
			continue
		}
		for _, nm := range m.Names {
			if nm != nil && goExportedName(nm.Name) {
				out = append(out, nm.Name)
			}
		}
	}
	return out
}

// goFunctionSpans returns the 1-based inclusive line span of every top-level
// func / method declaration with a body (issue #366). Reuses the same go/ast
// parse as extractGoSymbols; a nil AST (unrecoverable parse) yields nil.
func goFunctionSpans(src []byte) []FunctionSpan {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if f == nil {
		_ = err
		return nil
	}
	var spans []FunctionSpan
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || d.Name == nil || d.Body == nil {
			continue
		}
		spans = append(spans, FunctionSpan{
			Name:      d.Name.Name,
			StartLine: fset.Position(d.Pos()).Line,
			EndLine:   fset.Position(d.End()).Line,
		})
	}
	return spans
}

// goMethodOwners returns "method\x00owner" pairs for every receiver-bound
// method in the file (#445): `func (b *Buffer) String() string` →
// "String\x00Buffer". The owner is the receiver's base type name (pointer
// and generic type-parameter wrappers stripped). Lets the code graph
// disambiguate same-named methods on different types (the classic two
// `String()` methods that otherwise collapse to one bare name). Top-level
// funcs (no receiver) contribute nothing. Reuses the same go/ast parse;
// nil AST yields nil.
func goMethodOwners(src []byte) []string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if f == nil {
		_ = err
		return nil
	}
	var out []string
	for _, decl := range f.Decls {
		d, ok := decl.(*ast.FuncDecl)
		if !ok || d.Name == nil || d.Recv == nil || len(d.Recv.List) == 0 {
			continue
		}
		if owner := goReceiverType(d.Recv.List[0].Type); owner != "" {
			out = append(out, d.Name.Name+"\x00"+owner)
		}
	}
	return out
}

// goReceiverType returns the base type name of a method receiver,
// unwrapping a pointer (`*T` → "T") and a generic instantiation
// (`T[K]` / `*T[K, V]` → "T").
func goReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return goReceiverType(t.X)
	case *ast.IndexExpr: // generic receiver T[K]
		return goReceiverType(t.X)
	case *ast.IndexListExpr: // generic receiver T[K, V]
		return goReceiverType(t.X)
	}
	return ""
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
