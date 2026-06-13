package search

import (
	"context"
	"errors"
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

// Coupling computes per-package afferent (Ca) / efferent (Ce) coupling and
// instability (I = Ce/(Ca+Ce)) over the first-party Go packages under the
// module root (issue #410). It resolves each Go file to its package import
// path (module path from go.mod + the file's directory) and counts
// distinct package→package import edges where the imported path is
// first-party (carries the module prefix). Packages are ranked
// most-depended-upon first (high Ca), then most unstable (high I) — the
// "fragile hub" seams a refactor is riskiest near.
//
// Go-only: package resolution keys on the go.mod module path, so opts.Root
// must be the module root. Returns an empty report (Module == "") when no
// go.mod is found there.
func Coupling(ctx context.Context, opts Options, top int, registry *content.Registry) (*CouplingResult, error) {
	root := opts.Root
	if root == "" && len(opts.Roots) > 0 {
		root = opts.Roots[0]
	}
	if root == "" {
		root = "."
	}
	module := moduledPath(root)
	res := &CouplingResult{Module: module, Packages: []PackageCoupling{}}
	if module == "" {
		return res, nil // no go.mod at the root — nothing first-party to resolve
	}

	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	if opts.Expr == "" {
		opts.Expr = "is_source"
	}

	results, walkErr := Walk(ctx, opts, registry)

	// efferent[P] = set of first-party packages P imports; afferent[P] =
	// set of first-party packages importing P. Every package seen as a
	// source dir OR an import target gets a node.
	efferent := map[string]map[string]bool{}
	afferent := map[string]map[string]bool{}
	ensure := func(m map[string]map[string]bool, k string) map[string]bool {
		if m[k] == nil {
			m[k] = map[string]bool{}
		}
		return m[k]
	}
	touch := func(pkg string) {
		ensure(efferent, pkg)
		ensure(afferent, pkg)
	}

	for _, r := range results {
		if r.Attrs == nil {
			continue
		}
		if lang, _ := r.Attrs.Extra["language"].(string); lang != "go" {
			continue
		}
		pkg := goPackageImportPath(root, r.Path, module)
		if pkg == "" {
			continue
		}
		touch(pkg)
		imports, _ := r.Attrs.Extra["imports"].([]string)
		for _, imp := range imports {
			if !isFirstParty(imp, module) || imp == pkg {
				continue
			}
			touch(imp)
			efferent[pkg][imp] = true
			afferent[imp][pkg] = true
		}
	}

	for pkg := range efferent {
		ca, ce := len(afferent[pkg]), len(efferent[pkg])
		inst := 0.0
		if ca+ce > 0 {
			inst = float64(ce) / float64(ca+ce)
		}
		res.Packages = append(res.Packages, PackageCoupling{
			Package:     pkg,
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
