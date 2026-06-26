package search

import (
	"os"
	"path/filepath"
	"strings"
)

// rubyCouplingAdapter computes directory-module coupling for a Ruby gem (#519).
// Like JS/TS, Ruby has no file-level package declaration, so the coupling unit
// is the directory: node = a file's directory relative to the project root.
//
// Ruby resolution is trickier than JS because both styles appear:
//   - `require_relative "foo/bar"` — relative to the requiring FILE's directory.
//   - `require "gem/foo"`          — relative to a load path (a gem's lib/).
//
// Both reach us merged in the `imports` attribute (the extractor's `^require`
// query captures require and require_relative alike) with no leading-dot to
// distinguish them, and the required thing is a FILE, not a directory — so
// directory granularity alone can't tell `require "json"` (stdlib → lib/json.rb,
// not first-party) from `require "sinatra/base"` (first-party → lib/sinatra/base.rb).
//
// To avoid that false-positive class, the adapter records every first-party
// file's load-path stem during the node pass (path minus the lib/ load root
// minus .rb) and resolves an import by stem lookup: an import is first-party
// only when a real first-party file backs it. The node returned is that file's
// directory.
//
// Limitation: Ruby autoloading (Zeitwerk, ActiveSupport autoload) expresses
// dependencies WITHOUT require statements, so a gem that autoloads its tree
// yields a sparse graph — only the edges that appear as explicit require /
// require_relative are seen. Documented; the same static-only caveat as every
// other adapter.
type rubyCouplingAdapter struct {
	root     string
	cwd      string
	module   string
	loadRoot string            // "lib" when <root>/lib exists, else ""
	stems    map[string]string // load-path file stem -> the file's directory node
}

func (a *rubyCouplingAdapter) matchesLanguage(lang string) bool { return lang == "ruby" }

func (a *rubyCouplingAdapter) prepare(root string) (string, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	a.root = abs
	a.cwd, _ = os.Getwd()
	a.module = filepath.Base(abs)
	a.stems = map[string]string{}
	// A gem's lib/ is the conventional load root: `require "gem/x"` ⇒
	// lib/gem/x.rb. Absent a lib/, the repo root itself is the load root.
	if dirExists(filepath.Join(abs, "lib")) {
		a.loadRoot = "lib"
	}
	return a.module, true
}

// node returns a file's directory relative to the project root and, as a side
// effect, records the file's load-path stem so firstPartyImport can resolve
// require "gem/x" / require_relative "x" to a real first-party file.
func (a *rubyCouplingAdapter) node(path string, _ map[string]any) string {
	abs := a.absolutise(path)
	relFile, err := filepath.Rel(a.root, abs)
	if err != nil || relFile == ".." || strings.HasPrefix(relFile, ".."+string(filepath.Separator)) {
		return ""
	}
	relSlash := filepath.ToSlash(relFile)
	dirNode := filepath.ToSlash(filepath.Dir(relFile)) // "." for a root-level file
	// Stem = the require path that would load this file: drop the load-root
	// prefix and the .rb extension. lib/sinatra/base.rb ⇒ "sinatra/base".
	stem := strings.TrimSuffix(relSlash, ".rb")
	if a.loadRoot != "" {
		stem = strings.TrimPrefix(stem, a.loadRoot+"/")
	}
	a.stems[stem] = dirNode
	return dirNode
}

// firstPartyImport resolves a require / require_relative target to the directory
// of the first-party file that backs it, or ok=false for stdlib / gem requires
// (no matching first-party file). Tries the load-path form first (require
// "gem/x"), then the file-relative form (require_relative "x" against fromNode).
func (a *rubyCouplingAdapter) firstPartyImport(imp, fromNode string, _ map[string]bool) (string, bool) {
	imp = strings.TrimSpace(imp)
	// (a) load-path require: the import string IS the stem.
	if dn, ok := a.stems[filepath.ToSlash(imp)]; ok {
		return dn, true
	}
	// (b) require_relative: resolve against the requiring file's directory,
	// then map to the load-path stem.
	base := fromNode
	if base == "." {
		base = ""
	}
	relTarget := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.FromSlash(base), filepath.FromSlash(imp))))
	if relTarget == ".." || strings.HasPrefix(relTarget, "../") {
		return "", false
	}
	stem := relTarget
	if a.loadRoot != "" {
		stem = strings.TrimPrefix(stem, a.loadRoot+"/")
	}
	if dn, ok := a.stems[stem]; ok {
		return dn, true
	}
	return "", false
}

// absolutise resolves a possibly-relative walk path using the cwd captured in
// prepare (mirrors jstsCouplingAdapter.absolutise).
func (a *rubyCouplingAdapter) absolutise(path string) string {
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
