package search

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// unusedExportsLangs is the set of languages unused_exports analyses —
// those where both "exported" and "same package" are derivable today.
// Go: capitalised name + go.mod import path. Python: non-underscore name +
// package directory. Other languages need keyword-visibility / declared-
// package extraction (the cross-language rollout, issue #409 follow-up) and
// are silently skipped until then.
var unusedExportsLangs = map[string]bool{"go": true, "python": true}

// exportedInLang reports whether name is exported/public in language lang.
// Name-derivable: Go (upper-cased first rune) and Python (no leading
// underscore — the public/_private convention; dunders start with _ too, so
// they're excluded).
func exportedInLang(name, lang string) bool {
	switch lang {
	case "go":
		return isExportedName(name)
	case "python":
		return name != "" && !strings.HasPrefix(name, "_")
	}
	return false
}

// packageKeyFor returns a comparable package identity for a file (the unit
// across which intra- vs cross-package use is judged) and false when the
// file can't be attributed. Go uses the go.mod import path; Python uses the
// file's directory relative to root (one Python package per directory).
func packageKeyFor(root, path, lang, module string) (string, bool) {
	switch lang {
	case "go":
		if module == "" {
			return "", false
		}
		p := goPackageImportPath(root, path, module)
		return p, p != ""
	case "python":
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}
	return "", false
}

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

// UnusedExports lists exported symbols (functions / types) that are
// referenced ONLY from within their own package — they could be unexported
// to shrink the package's public API surface (issue #409). For each file it
// resolves a package identity and, for every exported symbol, checks that it
// is referenced at least once and that every referencing file lives in the
// defining package.
//
// Supported languages today are Go and Python (see unusedExportsLangs):
// Go uses capitalised-name visibility + the go.mod import path; Python uses
// the public/_private name convention + the package directory. Other wired
// languages are skipped until keyword-visibility / declared-package
// extraction lands. Builds on the cross-language type-usage references from
// #398 (an exported type used only as a field type in the same package is
// correctly seen as used; used in ANOTHER package it disqualifies).
//
// HEURISTIC, same caveats as dead_code: reflection / framework dispatch
// (kong `…Cmd`, Go test entries) is excluded, but external consumers outside
// the walked tree, interface satisfaction, and same-name collisions can
// still mislead — a review list, not an auto-unexport list.
func UnusedExports(ctx context.Context, opts Options, registry *content.Registry) (*UnusedExportsResult, error) {
	root := opts.Root
	if root == "" && len(opts.Roots) > 0 {
		root = opts.Roots[0]
	}
	if root == "" {
		root = "."
	}
	// module is the go.mod path used for Go package identity; empty means
	// Go files are skipped (no module to resolve against) but Python and
	// other path-based languages are still analysed.
	module := moduledPath(root)
	res := &UnusedExportsResult{Module: module, Candidates: []UnusedExport{}}

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
		lang  string
		multi bool // defined in more than one package → ambiguous, skip
	}
	defs := map[string]*defInfo{}
	refPkgs := map[string]map[string]bool{} // symbol -> set of referencing packages

	note := func(name, kind, pkg, path, lang string) {
		d := defs[name]
		if d == nil {
			defs[name] = &defInfo{kind: kind, path: path, pkg: pkg, lang: lang}
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
		lang, _ := r.Attrs.Extra["language"].(string)
		if !unusedExportsLangs[lang] {
			continue
		}
		pkg, ok := packageKeyFor(root, r.Path, lang, module)
		if !ok {
			continue
		}
		if funcs, ok := r.Attrs.Extra["functions"].([]string); ok {
			for _, fn := range funcs {
				if exportedInLang(fn, lang) {
					note(fn, "function", pkg, r.Path, lang)
				}
			}
		}
		if types, ok := r.Attrs.Extra["type_names"].([]string); ok {
			for _, t := range types {
				if exportedInLang(t, lang) {
					note(t, "type", pkg, r.Path, lang)
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
		if isReflectionDispatchedEntry(d.kind, name, d.path, d.lang) {
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
