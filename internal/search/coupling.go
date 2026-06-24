package search

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// PackageCoupling is the afferent/efferent coupling profile of one
// first-party package (Robert C. Martin's metrics).
type PackageCoupling struct {
	Package     string  `json:"package"`     // import path
	Afferent    int     `json:"afferent"`    // Ca: distinct first-party packages that import this one
	Efferent    int     `json:"efferent"`    // Ce: distinct first-party packages this one imports
	Instability float64 `json:"instability"` // I = Ce / (Ca + Ce); 0 when isolated
}

// CouplingResult is the package-coupling report (issue #410).
type CouplingResult struct {
	Module             string            `json:"module"`
	Packages           []PackageCoupling `json:"packages"`
	Cancelled          bool              `json:"cancelled,omitempty"`
	CancellationReason string            `json:"cancellation_reason,omitempty"`
}

// couplingAdapter encapsulates the per-language pieces the coupling metric
// needs; the graph math (Ca/Ce/I + ranking, in Coupling) is entirely
// language-agnostic. Adapters are stateful — prepare resolves and caches
// the first-party scope, then node/firstPartyImport consult it. Each
// ecosystem differs in two ways Go happens to make trivial (issue #467):
//
//  1. the first-party boundary — Go: the go.mod module path; Rust: the set
//     of workspace member crate names; Java/C#: the set of packages /
//     namespaces the repo's own files declare (passed to firstPartyImport as
//     `nodes`), and
//  2. mapping a file to a "node" — Go: package = import path = module +
//     directory; Rust: crate, by nearest-ancestor manifest; Java/C#: the
//     file's declared package / namespace, read from its attributes.
type couplingAdapter interface {
	// language is the source `language` attribute value this adapter
	// analyses (matched against FileAttributes.Extra["language"]).
	language() string
	// prepare resolves the first-party scope rooted at root, caches any
	// per-language state on the adapter, and returns the report identity
	// (CouplingResult.Module) plus whether anything is analysable. ok=false
	// ⇒ Coupling returns an empty report without walking.
	prepare(root string) (module string, ok bool)
	// node maps a source file to its node id, or "" to skip the file. extra
	// is the file's FileAttributes.Extra (Java reads its declared package
	// from it; Go/Rust derive the node from the path alone).
	node(path string, extra map[string]any) string
	// firstPartyImport reports whether import string imp (from a file whose
	// node is fromNode) targets a first-party node, and if so returns that
	// node id. nodes is the set of every node the repo's own files occupy —
	// the first-party boundary for adapters (Java) that derive it from the
	// tree rather than a manifest; manifest-based adapters (Go/Rust) ignore
	// it. Returns ok=false for external dependencies.
	firstPartyImport(imp, fromNode string, nodes map[string]bool) (node string, ok bool)
}

// couplingAdapterFor selects the adapter for a tree by its build manifest:
// go.mod ⇒ Go (packages), Cargo.toml ⇒ Rust (crates), pom.xml / Gradle ⇒
// Java (packages), a .sln / .csproj / props ⇒ C# (namespaces) (#467). Falls
// back to the Go adapter, whose prepare reports ok=false when there is no
// go.mod — yielding an empty report, the historical behaviour.
func couplingAdapterFor(root string) couplingAdapter {
	switch {
	case fileExists(filepath.Join(root, "go.mod")):
		return &goCouplingAdapter{}
	case fileExists(filepath.Join(root, "Cargo.toml")):
		return &rustCouplingAdapter{}
	case hasAnyFile(root, "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"):
		return &packageDeclAdapter{lang: "java"}
	case isCSharpRoot(root):
		return &packageDeclAdapter{lang: "csharp"}
	default:
		return &goCouplingAdapter{}
	}
}

// hasAnyFile reports whether any of names exists as a regular file in dir.
func hasAnyFile(dir string, names ...string) bool {
	for _, n := range names {
		if fileExists(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}

// isCSharpRoot reports whether dir looks like a C# / .NET project root: a
// solution or project file, or a modern SDK-style root marker. C# has no
// single canonical root manifest (unlike go.mod / Cargo.toml / pom.xml), so
// the .sln / .csproj globs are the strongest signal.
func isCSharpRoot(dir string) bool {
	if hasAnyFile(dir, "Directory.Build.props", "Directory.Packages.props", "global.json") {
		return true
	}
	return hasGlobMatch(dir, "*.sln") || hasGlobMatch(dir, "*.csproj")
}

// hasGlobMatch reports whether any entry in dir matches the shell pattern.
func hasGlobMatch(dir, pattern string) bool {
	m, err := filepath.Glob(filepath.Join(dir, pattern))
	return err == nil && len(m) > 0
}

// fileExists reports whether path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Coupling computes per-package afferent (Ca) / efferent (Ce) coupling and
// instability (I = Ce/(Ca+Ce)) over the first-party packages under the
// project root (issue #410). It counts distinct package→package import
// edges where the imported path is first-party. Packages are ranked
// most-depended-upon first (high Ca), then most unstable (high I) — the
// "fragile hub" seams a refactor is riskiest near.
//
// The graph math here is language-agnostic; the first-party boundary and
// the file/import → node mappings come from a couplingAdapter selected by
// the build manifest at opts.Root: go.mod ⇒ Go packages, Cargo.toml ⇒ Rust
// crates (#467). An empty report (Module == "") is returned when the root
// carries no recognised manifest.
func Coupling(ctx context.Context, opts Options, top int, registry *content.Registry) (*CouplingResult, error) {
	root := opts.Root
	if root == "" && len(opts.Roots) > 0 {
		root = opts.Roots[0]
	}
	if root == "" {
		root = "."
	}

	adapter := couplingAdapterFor(root)
	unit, ok := adapter.prepare(root)
	res := &CouplingResult{Module: unit, Packages: []PackageCoupling{}}
	if !ok {
		return res, nil // nothing first-party to resolve at root
	}

	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	if opts.Expr == "" {
		opts.Expr = "is_source"
	}

	results, walkErr := Walk(ctx, opts, registry)

	// efferent[P] = set of first-party nodes P imports; afferent[P] = set
	// of first-party nodes importing P. Every node seen as a source file
	// OR an import target gets an entry.
	efferent := map[string]map[string]bool{}
	afferent := map[string]map[string]bool{}
	ensure := func(m map[string]map[string]bool, k string) map[string]bool {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		return m[k]
	}
	touch := func(node string) {
		ensure(efferent, node)
		ensure(afferent, node)
	}

	// Pass 1: map each first-party file to its node and collect the node set
	// — the first-party boundary for adapters that derive it from the tree
	// (Java). fileNode caches the per-result node so pass 2 doesn't recompute.
	fileNode := make([]string, len(results))
	nodes := map[string]bool{}
	for i, r := range results {
		if r.Attrs == nil {
			continue
		}
		if lang, _ := r.Attrs.Extra["language"].(string); lang != adapter.language() {
			continue
		}
		node := adapter.node(r.Path, r.Attrs.Extra)
		if node == "" {
			continue
		}
		fileNode[i] = node
		nodes[node] = true
		touch(node)
	}

	// Pass 2: resolve each file's imports to first-party nodes and record the
	// node→node edges.
	for i, r := range results {
		node := fileNode[i]
		if node == "" {
			continue
		}
		imports, _ := r.Attrs.Extra["imports"].([]string)
		for _, imp := range imports {
			target, ok := adapter.firstPartyImport(imp, node, nodes)
			if !ok || target == node {
				continue
			}
			touch(target)
			efferent[node][target] = true
			afferent[target][node] = true
		}
	}

	for node := range efferent {
		ca, ce := len(afferent[node]), len(efferent[node])
		inst := 0.0
		if ca+ce > 0 {
			inst = float64(ce) / float64(ca+ce)
		}
		res.Packages = append(res.Packages, PackageCoupling{
			Package:     node,
			Afferent:    ca,
			Efferent:    ce,
			Instability: inst,
		})
	}

	sort.Slice(res.Packages, func(i, j int) bool {
		a, b := res.Packages[i], res.Packages[j]
		if a.Afferent != b.Afferent {
			return a.Afferent > b.Afferent
		}
		if a.Instability != b.Instability {
			return a.Instability > b.Instability
		}
		return a.Package < b.Package
	})
	if top > 0 && len(res.Packages) > top {
		res.Packages = res.Packages[:top]
	}

	if walkErr != nil {
		switch {
		case errors.Is(walkErr, context.Canceled):
			res.Cancelled = true
			res.CancellationReason = "client_cancel"
			return res, nil
		case errors.Is(walkErr, context.DeadlineExceeded):
			res.Cancelled = true
			res.CancellationReason = "timeout"
			return res, nil
		}
		return res, walkErr
	}
	return res, nil
}

// goCouplingAdapter resolves Go packages: the first-party boundary is the
// go.mod module path and a package node is module + the file's directory.
type goCouplingAdapter struct {
	root   string
	module string
}

func (a *goCouplingAdapter) language() string { return "go" }

func (a *goCouplingAdapter) prepare(root string) (string, bool) {
	a.root = root
	a.module = moduledPath(root)
	return a.module, a.module != ""
}

func (a *goCouplingAdapter) node(path string, _ map[string]any) string {
	return goPackageImportPath(a.root, path, a.module)
}

func (a *goCouplingAdapter) firstPartyImport(imp, _ string, _ map[string]bool) (string, bool) {
	if isFirstParty(imp, a.module) {
		return imp, true // a Go import string IS the package path; boundary is the go.mod prefix
	}
	return "", false
}

// goPackageImportPath maps a Go file's disk path to its package import path
// (module + the file's directory relative to root). Files directly in the
// module root resolve to the module path itself. Returns "" when the file
// sits outside root.
func goPackageImportPath(root, path, module string) string {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == "" {
		return module
	}
	return module + "/" + rel
}

// isFirstParty reports whether an import path belongs to the module (the
// module path itself or a subpackage of it).
func isFirstParty(imp, module string) bool {
	return imp == module || strings.HasPrefix(imp, module+"/")
}
