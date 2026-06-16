package content

import (
	ts "github.com/odvcencio/gotreesitter"
)

// methodOwnerSpec describes, for one tree-sitter language, how to find a
// method's owning type: the node types that are method definitions, the
// enclosing container node types (class / struct / impl / …), and the
// field that names the container. Empty containerField means "the first
// named child that is a (type_)identifier".
type methodOwnerSpec struct {
	methodNodes   []string
	containerNode []string
	containerName string // field name on the container giving its name node
}

// tsMethodOwnerSpec drives tsMethodOwners (#445). Covers the class-based
// languages where "a method belongs to a type" is a clean syntactic
// nesting. Languages absent here (C — no methods; Perl / R / MATLAB — no
// class-method nesting we model) simply produce no owners, and their
// methods stay bare, exactly as before.
var tsMethodOwnerSpec = map[string]methodOwnerSpec{
	"java":       {methodNodes: []string{"method_declaration", "constructor_declaration"}, containerNode: []string{"class_declaration", "interface_declaration", "enum_declaration", "record_declaration"}, containerName: "name"},
	"csharp":     {methodNodes: []string{"method_declaration", "constructor_declaration"}, containerNode: []string{"class_declaration", "struct_declaration", "interface_declaration", "record_declaration"}, containerName: "name"},
	// Kotlin names its function_declaration / class_declaration with a bare
	// simple_identifier / type_identifier child (no `name` field) — handled
	// by the first-name-child fallback in symbolName.
	"kotlin":     {methodNodes: []string{"function_declaration"}, containerNode: []string{"class_declaration", "object_declaration"}, containerName: "name"},
	"scala":      {methodNodes: []string{"function_definition"}, containerNode: []string{"class_definition", "object_definition", "trait_definition"}, containerName: "name"},
	"php":        {methodNodes: []string{"method_declaration"}, containerNode: []string{"class_declaration", "interface_declaration", "trait_declaration", "enum_declaration"}, containerName: "name"},
	"python":     {methodNodes: []string{"function_definition"}, containerNode: []string{"class_definition"}, containerName: "name"},
	"ruby":       {methodNodes: []string{"method"}, containerNode: []string{"class", "module"}, containerName: "name"},
	"typescript": {methodNodes: []string{"method_definition"}, containerNode: []string{"class_declaration", "class"}, containerName: "name"},
	"javascript": {methodNodes: []string{"method_definition"}, containerNode: []string{"class_declaration", "class"}, containerName: "name"},
	"swift":      {methodNodes: []string{"function_declaration"}, containerNode: []string{"class_declaration", "protocol_declaration"}, containerName: "name"},
	// Rust methods live in `impl Type { fn … }`; the owner is the impl's
	// `type` field, not a `name`.
	"rust": {methodNodes: []string{"function_item"}, containerNode: []string{"impl_item"}, containerName: "type"},
	// C++ is intentionally omitted: a method's name is buried in a
	// function_declarator (function_definition → declarator → declarator),
	// not a direct field/child, so the parent-walk model doesn't capture it
	// cleanly. C++ methods stay bare for now.
}

// tsMethodOwners returns "method\x00owner" pairs for every method nested in
// a type container (#445), so the code graph can disambiguate same-named
// methods across types in the tree-sitter languages — the cross-language
// counterpart to goMethodOwners. Returns nil for languages without a spec
// or when the parse yields nothing.
func tsMethodOwners(language string, src []byte) []string {
	spec, ok := tsMethodOwnerSpec[language]
	if !ok {
		return nil
	}
	tl := tsLangFor(language)
	if tl == nil || tl.lang == nil {
		return nil
	}
	tree, err := tl.pool.Parse(src)
	if err != nil || tree == nil {
		return nil
	}
	methodSet := sliceToSet(spec.methodNodes)
	containerSet := sliceToSet(spec.containerNode)

	var out []string
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if n == nil {
			return
		}
		if methodSet[n.Type(tl.lang)] {
			if name := symbolName(n, "name", src, tl.lang); name != "" {
				if owner := enclosingOwner(n, containerSet, spec.containerName, src, tl.lang); owner != "" {
					out = append(out, name+"\x00"+owner)
				}
			}
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return out
}

// enclosingOwner walks up from a method node to the nearest container in
// containerSet and returns its name (via the containerName field, base
// identifier extracted), or "" if none.
func enclosingOwner(method *ts.Node, containerSet map[string]bool, nameField string, src []byte, lang *ts.Language) string {
	for p := method.Parent(); p != nil; p = p.Parent() {
		if containerSet[p.Type(lang)] {
			return baseTypeName(symbolName(p, nameField, src, lang))
		}
	}
	return ""
}

// nameNodeTypes are the node types that name a definition when a grammar
// exposes the name as a bare child rather than a `name` field (e.g. Kotlin
// simple_identifier / type_identifier).
var nameNodeTypes = map[string]bool{
	"identifier": true, "simple_identifier": true, "type_identifier": true,
	"field_identifier": true, "constant": true, "name": true,
}

// symbolName returns a node's name: the named `field` if present, else the
// first direct child whose type is a recognised name node. "" when neither.
func symbolName(n *ts.Node, field string, src []byte, lang *ts.Language) string {
	if c := n.ChildByFieldName(field, lang); c != nil {
		return c.Text(src)
	}
	for i := 0; i < n.ChildCount(); i++ {
		ch := n.Child(i)
		if ch != nil && nameNodeTypes[ch.Type(lang)] {
			return ch.Text(src)
		}
	}
	return ""
}

// baseTypeName strips a generic suffix and surrounding whitespace from a
// type expression: "Gen<T>" / "Gen[T]" → "Gen". Leaves plain names intact.
func baseTypeName(s string) string {
	for i, r := range s {
		if r == '<' || r == '[' || r == ' ' || r == '\n' || r == '\t' {
			return s[:i]
		}
	}
	return s
}

func sliceToSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}
