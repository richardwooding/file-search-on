package search

import (
	"path/filepath"
	"strings"
)

// javaCouplingAdapter computes package-level coupling for a Java project
// (#467). Nodes are declared packages; the first-party boundary is the set
// of packages the repo's own files declare (no build-file parsing — robust
// across Maven, Gradle, and plain source trees). An `import com.x.Y` is an
// inter-package edge when `com.x` is one of those declared packages;
// imports into third-party / JDK packages (java.util, com.google.*, …) are
// ignored because those packages are never declared in-tree.
//
// Java packages are explicit and unambiguous (unlike Rust's module tree), so
// package granularity needs no resolution beyond reading each file's
// declared `package` (surfaced as the builder-internal `package` attribute)
// and trimming the trailing type name off each import FQN.
type javaCouplingAdapter struct {
	module string
}

func (a *javaCouplingAdapter) language() string { return "java" }

// prepare records the project identity (the root directory's base name —
// Java has no single canonical module id without parsing a build file) and
// always proceeds: the first-party package set is derived from the walked
// files, so an empty tree simply yields an empty report.
func (a *javaCouplingAdapter) prepare(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	a.module = filepath.Base(abs)
	return a.module, true
}

// node returns the file's declared package, read from the builder-internal
// `package` attribute (populated by the tree-sitter extractor). Files with
// no package declaration (the default package) are skipped.
func (a *javaCouplingAdapter) node(_ string, extra map[string]any) string {
	pkg, _ := extra["package"].(string)
	return pkg
}

// firstPartyImport maps an import FQN to the first-party package that owns
// it, by finding the longest prefix that is a declared package. One rule
// covers every Java import form: wildcard (`com.x.*` → trimmed to `com.x`,
// matches directly), plain type (`com.x.Y` → `com.x`), and static member
// (`import static com.x.Y.method` → `com.x`, since neither the member nor
// the type `com.x.Y` is a declared package but `com.x` is). Longest-match
// is correct because a type lives in exactly one package, so its package is
// the longest declared-package prefix of its FQN.
func (a *javaCouplingAdapter) firstPartyImport(imp, _ string, nodes map[string]bool) (string, bool) {
	p := strings.TrimSuffix(strings.TrimSpace(imp), ".*")
	for p != "" {
		if nodes[p] {
			return p, true
		}
		i := strings.LastIndex(p, ".")
		if i <= 0 {
			break
		}
		p = p[:i]
	}
	return "", false
}
