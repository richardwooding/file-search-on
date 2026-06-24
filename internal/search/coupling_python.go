package search

import (
	"os"
	"path/filepath"
	"strings"
)

// pythonCouplingAdapter computes package-level coupling for a Python project
// (#467). Nodes are packages — the dotted path of a file's directory beneath
// the import root. The first-party boundary is the set of packages the tree
// itself occupies, so stdlib and third-party imports (os, numpy, …) are
// ignored. Unlike Java/C#, a Python file does not name its package in-source;
// it is implied by the directory layout, so node() derives it from the path.
//
// The import root is the directory dotted import paths are relative to: a
// top-level `src/` when present (the src-layout convention), else the project
// root. Absolute imports (`import a.b.c`, `from a.b import c`) resolve to the
// longest first-party package prefix; relative imports (`from . import x`,
// `from ..pkg import y`) are not surfaced by the extractor and so are not
// counted — a documented limitation (most are intra-package anyway).
type pythonCouplingAdapter struct {
	importRoot string // absolute dir that dotted import paths are rooted at
	cwd        string // captured in prepare to absolutise relative file paths without a per-call syscall
	module     string
}

func (a *pythonCouplingAdapter) matchesLanguage(lang string) bool { return lang == "python" }

func (a *pythonCouplingAdapter) prepare(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	a.importRoot = abs
	if src := filepath.Join(abs, "src"); dirExists(src) {
		a.importRoot = src // src-layout: packages live under src/
	}
	a.cwd, _ = os.Getwd()
	a.module = filepath.Base(abs)
	return a.module, true
}

// node returns the package a file belongs to: the dotted path of its
// directory relative to the import root. Top-level modules (a file directly
// in the import root) and files outside it are skipped.
func (a *pythonCouplingAdapter) node(path string, _ map[string]any) string {
	abs := path
	if !filepath.IsAbs(abs) {
		if a.cwd != "" {
			abs = filepath.Join(a.cwd, abs)
		} else if p, err := filepath.Abs(abs); err == nil {
			abs = p
		}
	}
	rel, err := filepath.Rel(a.importRoot, filepath.Dir(abs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "" // outside the import root
	}
	if rel == "." || rel == "" {
		return "" // top-level module — no package
	}
	return strings.ReplaceAll(filepath.ToSlash(rel), "/", ".")
}

func (a *pythonCouplingAdapter) firstPartyImport(imp, _ string, nodes map[string]bool) (string, bool) {
	return longestPackagePrefix(imp, nodes)
}
