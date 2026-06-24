package search

import (
	"os"
	"path/filepath"
	"strings"
)

// jstsCouplingAdapter computes directory-module coupling for a JavaScript /
// TypeScript project (#467). JS/TS has no package/namespace declaration, so
// the coupling unit is the directory (a "module folder"): node = a file's
// directory relative to the project root. It spans both the "javascript"
// (.js/.mjs/.cjs/.jsx) and "typescript" (.ts/.tsx) languages.
//
// First-party is determined structurally, not from a node set: a relative
// import (./x, ../y) stays inside the repo and is first-party; a bare
// specifier (react, @scope/pkg) resolves from node_modules and is external.
// tsconfig path aliases (@/x) are first-party but need tsconfig parsing —
// deferred (#467), so they read as external for now.
type jstsCouplingAdapter struct {
	root   string // absolute project root
	cwd    string // captured in prepare to absolutise relative file paths
	module string
}

func (a *jstsCouplingAdapter) matchesLanguage(lang string) bool {
	return lang == "javascript" || lang == "typescript"
}

func (a *jstsCouplingAdapter) prepare(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	a.root = abs
	a.cwd, _ = os.Getwd()
	a.module = filepath.Base(abs)
	return a.module, true
}

// node returns a file's module: its directory relative to the project root
// ("." for files at the root), or "" when the file sits outside the root.
func (a *jstsCouplingAdapter) node(path string, _ map[string]any) string {
	abs := a.absolutise(path)
	rel, err := filepath.Rel(a.root, filepath.Dir(abs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel) // rel is "." for the root directory
}

// firstPartyImport resolves a relative import to the directory module it
// targets. fromNode is the importing file's module (its directory). A `./x`
// pointing at a sibling file resolves back to fromNode (the caller drops the
// self-edge); `./sub` where sub/ is a directory resolves to fromNode/sub;
// `../b/c` climbs out. Bare specifiers and aliases return ok=false.
func (a *jstsCouplingAdapter) firstPartyImport(imp, fromNode string, nodes map[string]bool) (string, bool) {
	imp = strings.TrimSpace(imp)
	if !strings.HasPrefix(imp, ".") {
		return "", false // bare specifier / alias → external
	}
	base := fromNode
	if base == "." {
		base = ""
	}
	// Resolve the import path relative to the importing file's directory.
	resolved := filepath.Clean(filepath.Join(filepath.FromSlash(base), filepath.FromSlash(imp)))
	resolvedSlash := filepath.ToSlash(resolved)
	if resolvedSlash == ".." || strings.HasPrefix(resolvedSlash, "../") {
		return "", false // climbs outside the project
	}
	// Directory import (./sub → sub/index.*) vs file import (./sib → sib.*):
	// the resolved path is a directory module iff it's in the node set (it
	// holds files) — checked against the set instead of an os.Stat per import,
	// which would be disk I/O on every edge of a large tree.
	if nodes[resolvedSlash] {
		return resolvedSlash, true
	}
	targetDir := filepath.ToSlash(filepath.Dir(resolved))
	if targetDir == "" {
		targetDir = "."
	}
	return targetDir, true
}

// absolutise resolves a possibly-relative walk path without a per-call
// syscall, using the cwd captured in prepare.
func (a *jstsCouplingAdapter) absolutise(path string) string {
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
