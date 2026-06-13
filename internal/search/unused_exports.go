package search

import (
	"context"
	"errors"
	"sort"

	"github.com/richardwooding/file-search-on/internal/content"
)

// UnusedExport is an exported symbol whose only references come from its
// own package — a candidate for unexporting to shrink the public surface.
type UnusedExport struct {
	Symbol  string `json:"symbol"`
	Kind    string `json:"kind"` // "function" | "type"
	Path    string `json:"path"`
	Package string `json:"package"`
}

// UnusedExportsResult is the unexport-candidate report (issue #409).
type UnusedExportsResult struct {
	Module             string         `json:"module"`
	Candidates         []UnusedExport `json:"candidates"`
	Cancelled          bool           `json:"cancelled,omitempty"`
	CancellationReason string         `json:"cancellation_reason,omitempty"`
}

// UnusedExports lists exported Go symbols (functions / types) that are
// referenced ONLY from within their own package — they could be unexported
// to shrink the package's public API surface (issue #409). It resolves each
// file to its package (go.mod module path + directory), then for every
// exported symbol checks that it is referenced at least once and that every
// referencing file lives in the defining package.
//
// Builds on the file→package resolution shared with `coupling` and the
// type-usage references from #398 (so an exported type used only as a field
// type elsewhere in the same package is correctly seen as used — and one
// used as a field type in ANOTHER package correctly disqualifies it).
//
// Go-only and HEURISTIC, same caveats as dead_code: reflection / framework
// dispatch (kong `…Cmd`, Go test entries) is excluded, but external
// consumers outside the walked tree, interface satisfaction, and same-name
// collisions can still mislead — a review list, not an auto-unexport list.
func UnusedExports(ctx context.Context, opts Options, registry *content.Registry) (*UnusedExportsResult, error) {
	root := opts.Root
	if root == "" && len(opts.Roots) > 0 {
		root = opts.Roots[0]
	}
	if root == "" {
		root = "."
	}
	module := moduledPath(root)
	res := &UnusedExportsResult{Module: module, Candidates: []UnusedExport{}}
	if module == "" {
		return res, nil
	}

	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	if opts.Expr == "" {
		opts.Expr = "is_source"
	}

	results, walkErr := Walk(ctx, opts, registry)

	type defInfo struct {
		kind  string
		path  string
		pkg   string
		multi bool // defined in more than one package → ambiguous, skip
	}
	defs := map[string]*defInfo{}
	refPkgs := map[string]map[string]bool{} // symbol -> set of referencing packages

	note := func(name, kind, pkg, path string) {
		d := defs[name]
		if d == nil {
			defs[name] = &defInfo{kind: kind, path: path, pkg: pkg}
			return
		}
		if d.pkg != pkg {
			d.multi = true
		}
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
		if funcs, ok := r.Attrs.Extra["functions"].([]string); ok {
			for _, fn := range funcs {
				if isExportedName(fn) {
					note(fn, "function", pkg, r.Path)
				}
			}
		}
		if types, ok := r.Attrs.Extra["type_names"].([]string); ok {
			for _, t := range types {
				if isExportedName(t) {
					note(t, "type", pkg, r.Path)
				}
			}
		}
		if refs, ok := r.Attrs.Extra["references"].([]string); ok {
			for _, ref := range refs {
				if refPkgs[ref] == nil {
					refPkgs[ref] = map[string]bool{}
				}
				refPkgs[ref][pkg] = true
			}
		}
	}

	for name, d := range defs {
		if d.multi {
			continue // same name in >1 package — can't attribute references
		}
		if isReflectionDispatchedEntry(d.kind, name, d.path, "go") {
			continue // kong …Cmd, go-test entries — dispatched, not statically used
		}
		users := refPkgs[name]
		if len(users) == 0 {
			continue // never referenced → that's dead_code, not an unexport candidate
		}
		intraOnly := true
		for pkg := range users {
			if pkg != d.pkg {
				intraOnly = false
				break
			}
		}
		if intraOnly {
			res.Candidates = append(res.Candidates, UnusedExport{
				Symbol:  name,
				Kind:    d.kind,
				Path:    d.path,
				Package: d.pkg,
			})
		}
	}

	sort.Slice(res.Candidates, func(i, j int) bool {
		a, b := res.Candidates[i], res.Candidates[j]
		if a.Package != b.Package {
			return a.Package < b.Package
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		return a.Kind < b.Kind
	})

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
