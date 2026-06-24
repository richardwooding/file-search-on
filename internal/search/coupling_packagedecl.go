package search

import (
	"path/filepath"
)

// packageDeclAdapter computes package-level coupling for languages where each
// file declares its package / namespace in-source and the first-party
// boundary is the set of packages the tree itself declares — build-tool
// agnostic, no manifest parsing (#467). Java (packages) and C# (namespaces)
// share this model exactly:
//
//   - node = the file's declared package, read from the builder-internal
//     `package` attribute (populated by the tree-sitter extractor);
//   - an import resolves to the longest declared-package prefix of its FQN,
//     one rule that covers Java type / static / wildcard imports and C#
//     namespace / static usings alike.
//
// Imports into packages the tree never declares (the JDK, the .NET BCL,
// third-party libraries) are ignored because they're absent from the node
// set.
type packageDeclAdapter struct {
	lang   string // the `language` attribute value this adapter analyses ("java" / "csharp")
	module string
}

func (a *packageDeclAdapter) language() string { return a.lang }

// prepare records the project identity (the root directory's base name —
// these ecosystems have no single canonical module id without parsing a
// build file) and always proceeds: the first-party package set is derived
// from the walked files, so an empty tree simply yields an empty report.
func (a *packageDeclAdapter) prepare(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	a.module = filepath.Base(abs)
	return a.module, true
}

// node returns the file's declared package / namespace from the
// builder-internal `package` attribute. Files with no declaration (Java
// default package, C# top-level statements) are skipped.
func (a *packageDeclAdapter) node(_ string, extra map[string]any) string {
	pkg, _ := extra["package"].(string)
	return pkg
}

// firstPartyImport maps an import FQN to the first-party package that owns it
// via the longest declared-package prefix. Handles every form: Java plain
// (`com.x.Y` → `com.x`), static (`com.x.Y.member` → `com.x`), wildcard
// (`com.x.*` → `com.x`); C# namespace using (`X.Y` → `X.Y` directly) and
// static using (`X.Y.Type` → `X.Y`).
func (a *packageDeclAdapter) firstPartyImport(imp, _ string, nodes map[string]bool) (string, bool) {
	return longestPackagePrefix(imp, nodes)
}
