package search

import (
	"os"
	"path/filepath"
	"strings"
)

// cppCouplingAdapter computes directory-module coupling for a C / C++ project
// (#521). C/C++ has no language-level module system pre-C++20 — the dependency
// graph IS the #include graph — so the coupling unit is the directory: node =
// a file's directory relative to the project root. One adapter spans both "c"
// and "cpp" (a .h header detects as "c" even in a C++ project, like the JS/TS
// adapter spans both its languages).
//
// The captured include text is bare (the tree-sitter path node strips the
// quotes / angle brackets), so `#include "format.h"` and `#include <algorithm>`
// are indistinguishable by syntax. Instead — like the Ruby adapter — an include
// is first-party only when it resolves to a real first-party FILE: the adapter
// records every walked file's path during the node pass, then resolves an
// include against the includer's directory AND the common include roots
// (include/, src/, the project root, mirroring -I search dirs). A `<algorithm>`
// / `<cerrno>` system header backs no first-party file, so it's never an edge.
//
// Limitation: include roots beyond include/ / src/ (custom -I dirs in the build
// file) aren't parsed, so an include resolvable only through such a dir reads
// as external. Documented; the same static-only caveat as the other adapters.
type cppCouplingAdapter struct {
	root         string
	cwd          string
	module       string
	includeRoots []string          // "" (root) + "include" / "src" when present
	files        map[string]string // rel file path (slash) -> the file's directory node
}

func (a *cppCouplingAdapter) matchesLanguage(lang string) bool {
	return lang == "c" || lang == "cpp"
}

func (a *cppCouplingAdapter) prepare(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	a.root = abs
	a.cwd, _ = os.Getwd()
	a.module = filepath.Base(abs)
	a.files = map[string]string{}
	// Include search roots, in resolution order: the includer's own dir is
	// tried first (in firstPartyImport), then these. "" is the project root
	// (covers `#include "src/foo.h"` from a root file); include/ and src/ are
	// the conventional -I roots.
	a.includeRoots = []string{""}
	for _, r := range []string{"include", "src"} {
		if dirExists(filepath.Join(abs, r)) {
			a.includeRoots = append(a.includeRoots, r)
		}
	}
	return a.module, true
}

// node returns a file's directory relative to the project root and records the
// file's rel path so firstPartyImport can tell a first-party header from a
// system one.
func (a *cppCouplingAdapter) node(path string, _ map[string]any) string {
	abs := a.absolutise(path)
	relFile, err := filepath.Rel(a.root, abs)
	if err != nil || relFile == ".." || strings.HasPrefix(relFile, ".."+string(filepath.Separator)) {
		return ""
	}
	dirNode := filepath.ToSlash(filepath.Dir(relFile))
	a.files[filepath.ToSlash(relFile)] = dirNode
	return dirNode
}

// firstPartyImport resolves an #include to the directory of the first-party
// file it targets, or ok=false for a system / external header (which backs no
// first-party file). Tries the includer's own directory first, then each
// include root.
func (a *cppCouplingAdapter) firstPartyImport(imp, fromNode string, _ map[string]bool) (string, bool) {
	imp = strings.TrimSpace(imp)
	if imp == "" {
		return "", false
	}
	base := fromNode
	if base == "." {
		base = ""
	}
	try := func(prefix string) (string, bool) {
		resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.FromSlash(prefix), filepath.FromSlash(imp))))
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			return "", false
		}
		dn, ok := a.files[resolved]
		return dn, ok
	}
	// Includer-relative first (`#include "sibling.h"`), then the -I roots.
	if dn, ok := try(base); ok {
		return dn, true
	}
	for _, r := range a.includeRoots {
		if dn, ok := try(r); ok {
			return dn, true
		}
	}
	return "", false
}

// absolutise resolves a possibly-relative walk path using the cwd captured in
// prepare (mirrors jstsCouplingAdapter.absolutise).
func (a *cppCouplingAdapter) absolutise(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if a.cwd != "" {
		return filepath.Join(a.cwd, path)
	}
	if p, err := filepath.Abs(path); err == nil {
		return p
	}
	return path
}
