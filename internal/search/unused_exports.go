package search

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// unusedExportsLangs is the set of languages unused_exports analyses. Each
// needs a visibility signal and a package identity:
//   - Go: capitalised name + go.mod import path.
//   - Python: non-underscore name + package directory.
//   - Rust: `pub` (the `exported_symbols` attribute) + module directory.
//   - TypeScript / JavaScript: `export` (the `exported_symbols` attribute) +
//     the file itself (ES module = file).
//   - Java / C#: `public` (the `exported_symbols` attribute) + directory
//     (one package per directory by convention — approximate for C#, whose
//     namespace can decouple from the directory).
//
// Default-public languages (Kotlin / Scala / PHP, which need negation-style
// visibility) and others are silently skipped until that lands.
var unusedExportsLangs = map[string]bool{
	"go": true, "python": true, "rust": true, "typescript": true, "javascript": true,
	"java": true, "csharp": true,
}

// exportedInLang reports whether name is exported/public in a language whose
// visibility is NAME-derivable: Go (upper-cased first rune) and Python (no
// leading underscore — the public/_private convention; dunders start with _
// too, so they're excluded). Keyword-visibility languages (Rust / TS / JS)
// don't use this — their public set comes from the `exported_symbols`
// attribute instead.
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
// file can't be attributed. Go uses the go.mod import path; Python and Rust
// use the file's directory (one package / module per directory); TS/JS use
// the file itself (an ES module is a file — an export used only within its
// own file is the unexport candidate).
func packageKeyFor(root, path, lang, module string) (string, bool) {
	switch lang {
	case "go":
		if module == "" {
			return "", false
		}
		p := goPackageImportPath(root, path, module)
		return p, p != ""
	case "python", "rust", "java", "csharp":
		return dirKey(root, path)
	case "typescript", "javascript":
		return path, true
	}
	return "", false
}

// dirKey returns the file's directory relative to root (slash-normalised),
// the package identity for directory-as-package languages. False when the
// file sits outside root.
func dirKey(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
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
		// Visibility: keyword-visibility languages carry the public subset
		// in the `exported_symbols` attribute; name-convention languages
		// (Go / Python) derive it from the name.
		expAttr, hasExp := r.Attrs.Extra["exported_symbols"].([]string)
		var expSet map[string]bool
		if hasExp {
			expSet = make(map[string]bool, len(expAttr))
			for _, e := range expAttr {
				expSet[e] = true
			}
		}
		isExported := func(name string) bool {
			if hasExp {
				return expSet[name]
			}
			return exportedInLang(name, lang)
		}
		if funcs, ok := r.Attrs.Extra["functions"].([]string); ok {
			for _, fn := range funcs {
				if isExported(fn) {
					note(fn, "function", pkg, r.Path, lang)
				}
			}
		}
		if types, ok := r.Attrs.Extra["type_names"].([]string); ok {
			for _, t := range types {
				if isExported(t) {
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
