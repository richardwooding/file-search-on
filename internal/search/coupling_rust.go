package search

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// rustCouplingAdapter computes crate-level coupling for a Cargo project or
// workspace (#467). Nodes are crates; the first-party boundary is the set of
// workspace member crate names discovered from Cargo.toml manifests under
// the root. A `use <crate>::…` whose leading segment names a sibling member
// crate is an inter-crate edge; `crate::` / `self::` / `super::` are
// intra-crate (no edge) and any other leading segment is an external
// dependency.
//
// Crate granularity is chosen because a Rust `use` path's leading segment is
// always a crate name (or crate/self/super) — unambiguous, no module-tree
// resolution required. Module-level coupling within a single crate is a
// separate, harder problem deferred under #467.
type rustCouplingAdapter struct {
	crates map[string]bool // normalized first-party crate names
	dirs   []crateDir      // crate root dirs, nearest-ancestor lookup
	cwd    string          // working dir captured in prepare, to absolutise relative file paths in node without a per-call syscall
}

// crateDir associates a crate's manifest directory with its (normalized)
// name, for mapping a source file to its owning crate.
type crateDir struct {
	dir   string // absolute, cleaned manifest directory
	crate string // normalized crate name
}

func (a *rustCouplingAdapter) matchesLanguage(lang string) bool { return lang == "rust" }

// prepare walks root for Cargo.toml manifests, records each member crate's
// name + directory, and reports the workspace identity. ok=false when no
// named crate is found (e.g. a workspace-only manifest with no members yet).
func (a *rustCouplingAdapter) prepare(root string) (string, bool) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	// Reset state so a reused adapter never leaks crates/dirs across runs.
	a.crates = map[string]bool{}
	a.dirs = nil
	a.cwd, _ = os.Getwd() // captured once; node uses it instead of per-file filepath.Abs

	_ = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // skip unreadable entries, keep walking
		}
		if d.IsDir() {
			if path != absRoot && skipCargoDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() != "Cargo.toml" {
			return nil
		}
		name := cargoPackageName(path)
		if name == "" {
			return nil // workspace-only / unnamed manifest
		}
		norm := normalizeCrate(name)
		a.crates[norm] = true
		a.dirs = append(a.dirs, crateDir{dir: filepath.Dir(path), crate: norm})
		return nil
	})

	if len(a.crates) == 0 {
		return "", false
	}
	// Nearest-ancestor wins: longest manifest dir first.
	sort.Slice(a.dirs, func(i, j int) bool { return len(a.dirs[i].dir) > len(a.dirs[j].dir) })

	// Workspace identity: the root crate's name if the root is itself a
	// crate, else the root directory's base name.
	module := filepath.Base(absRoot)
	for _, cd := range a.dirs {
		if cd.dir == absRoot {
			module = cd.crate
			break
		}
	}
	return module, true
}

// node maps a Rust source file to its owning crate (the nearest ancestor
// manifest directory), or "" when the file sits outside every known crate.
func (a *rustCouplingAdapter) node(path string, _ map[string]any) string {
	abs := path
	if !filepath.IsAbs(abs) {
		switch {
		case a.cwd != "":
			abs = filepath.Join(a.cwd, abs) // cheap: no syscall, cwd cached in prepare
		default:
			if p, err := filepath.Abs(abs); err == nil {
				abs = p
			}
		}
	}
	for _, cd := range a.dirs {
		if abs == cd.dir || strings.HasPrefix(abs, cd.dir+string(filepath.Separator)) {
			return cd.crate
		}
	}
	return ""
}

// firstPartyImport resolves a Rust `use` argument to a first-party crate
// node. The leading path segment is the crate name; crate/self/super are
// intra-crate (returned as fromNode, which the caller skips as a self-edge).
func (a *rustCouplingAdapter) firstPartyImport(imp, fromNode string, _ map[string]bool) (string, bool) {
	leading := imp
	if before, _, ok := strings.Cut(imp, "::"); ok {
		leading = before
	}
	leading = strings.TrimSpace(leading)
	switch leading {
	case "crate", "self", "super":
		return fromNode, true // intra-crate — no inter-crate edge
	}
	norm := normalizeCrate(leading)
	if a.crates[norm] {
		return norm, true
	}
	return "", false
}

// normalizeCrate maps a Cargo package name to its import-path form: Cargo
// allows hyphens in package names, but Rust code references them with
// underscores (`my-crate` → `my_crate`). Node ids use the normalized form
// consistently so file→crate and import→crate resolve to the same node.
func normalizeCrate(name string) string {
	return strings.ReplaceAll(strings.TrimSpace(name), "-", "_")
}

// cargoPackageName returns the `[package] name` declared in a Cargo.toml, or
// "" when the manifest declares no package (a virtual workspace root) or
// can't be parsed.
func cargoPackageName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var manifest struct {
		Package struct {
			Name string `toml:"name"`
		} `toml:"package"`
	}
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return ""
	}
	return manifest.Package.Name
}

// skipCargoDir reports whether a directory should be pruned from the
// manifest walk — build output, VCS metadata, and hidden / vendored trees
// never hold first-party crate roots worth counting.
func skipCargoDir(name string) bool {
	switch name {
	case "target", ".git", "node_modules", "vendor":
		return true
	}
	return strings.HasPrefix(name, ".")
}
