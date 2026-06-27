package search

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	coupling "github.com/richardwooding/go-coupling"

	"github.com/richardwooding/file-search-on/internal/content"
)

// PackageCoupling is the afferent/efferent coupling profile of one first-party
// package (Robert C. Martin's metrics).
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

// importGraph wraps the github.com/richardwooding/go-coupling first-party import
// graph together with the walk's cancellation state. The multi-language adapters
// + graph math live in that module (extracted from this package, #532); this
// file's job is to feed it the per-file attributes the walk already extracts.
type importGraph struct {
	module    string
	graph     *coupling.Graph
	cancelled bool
	reason    string
}

// buildImportGraph walks opts.Root, collects each source file's
// (language, imports, relative_imports, package) attributes into the input
// go-coupling needs, and builds the directed first-party import graph. The
// ecosystem (Go / Rust / JVM / C# / PHP / Perl / Python / JS-TS / C-C++) is
// detected by go-coupling from the build manifest at the root. On ctx
// cancellation it returns the partial graph with cancelled set and a nil error;
// other walk errors are returned alongside the partial graph.
func buildImportGraph(ctx context.Context, opts Options, registry *content.Registry) (*importGraph, error) {
	root := opts.Root
	if root == "" && len(opts.Roots) > 0 {
		root = opts.Roots[0]
	}
	if root == "" {
		root = "."
	}

	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	if opts.Expr == "" {
		opts.Expr = "is_source"
	}

	results, walkErr := Walk(ctx, opts, registry)

	files := make([]coupling.File, 0, len(results))
	for _, r := range results {
		if r.Attrs == nil {
			continue
		}
		lang, _ := r.Attrs.Extra["language"].(string)
		imports, _ := r.Attrs.Extra["imports"].([]string)
		relImports, _ := r.Attrs.Extra["relative_imports"].([]string)
		pkg, _ := r.Attrs.Extra["package"].(string)
		files = append(files, coupling.File{
			Path:            r.Path,
			Language:        lang,
			Imports:         imports,
			RelativeImports: relImports,
			Package:         pkg,
		})
	}

	g := coupling.Build(root, files)
	ig := &importGraph{module: g.Module(), graph: g}

	if walkErr != nil {
		switch {
		case errors.Is(walkErr, context.Canceled):
			ig.cancelled, ig.reason = true, "client_cancel"
			return ig, nil
		case errors.Is(walkErr, context.DeadlineExceeded):
			ig.cancelled, ig.reason = true, "timeout"
			return ig, nil
		}
		return ig, walkErr
	}
	return ig, nil
}

// Coupling computes per-package afferent (Ca) / efferent (Ce) coupling and
// instability (I = Ce/(Ca+Ce)) over the first-party packages under the project
// root (issue #410). Packages are ranked most-depended-upon first (high Ca),
// then most unstable (high I) — the "fragile hub" seams a refactor is riskiest
// near. The multi-language graph is built by go-coupling (#532); an empty
// report (Module == "") is returned when the root carries no recognised
// manifest.
func Coupling(ctx context.Context, opts Options, top int, registry *content.Registry) (*CouplingResult, error) {
	ig, err := buildImportGraph(ctx, opts, registry)
	res := &CouplingResult{
		Module:             ig.module,
		Packages:           []PackageCoupling{},
		Cancelled:          ig.cancelled,
		CancellationReason: ig.reason,
	}
	for _, c := range ig.graph.Coupling() {
		res.Packages = append(res.Packages, PackageCoupling{
			Package:     c.Package,
			Afferent:    c.Afferent,
			Efferent:    c.Efferent,
			Instability: c.Instability,
		})
	}
	if top > 0 && len(res.Packages) > top {
		res.Packages = res.Packages[:top]
	}
	return res, err
}

// goPackageImportPath maps a Go file's disk path to its package import path
// (module + the file's directory relative to root). Files directly in the
// module root resolve to the module path itself. Returns "" when the file sits
// outside root. Shared with unused_exports.go.
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
