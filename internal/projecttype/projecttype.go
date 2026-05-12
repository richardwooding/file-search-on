// Package projecttype detects what kind of project lives in a
// directory — Go module, Node app, Rust crate, Terraform stack, etc.
// — by checking for canonical indicator files (go.mod, package.json,
// Cargo.toml, *.tf, …) in the directory's own listing (not recursive).
//
// Mirrors the FilenameMatcher pattern from internal/content but at a
// directory granularity. A directory can match multiple project types
// at once: a Go module that also ships docker-compose.yml matches
// both `go` and `docker-compose` simultaneously, exactly like the
// cross-predicate firing for file content types (PR #95).
//
// Custom user-registered project types via CEL expressions are NOT
// in this MVP — a follow-up PR will add a CEL-driven extension path
// over the same registry.
package projecttype

import (
	"path/filepath"
	"sort"
	"sync"
)

// ProjectType describes a kind of project and the indicators that
// identify it. Indicators are evaluated against a directory's
// own listing (basenames only — no recursion into subdirectories).
// Any single indicator matching is enough to count the directory as
// this project type (OR semantics across the list).
type ProjectType struct {
	// Name is the stable identifier, lowercase + dashes (e.g. "go",
	// "node", "java-maven"). Used as the bucket key in MCP / CLI
	// output and the future is_X_project predicate root.
	Name string
	// Description is a short human-readable label.
	Description string
	// Indicators is the OR-list. The first one that matches wins
	// (and is returned to the caller for "why did this match"
	// debuggability).
	Indicators []Indicator
}

// Indicator is a single match rule against a directory's contents.
// Exactly one of HasFile or HasGlob should be set per indicator.
type Indicator struct {
	// HasFile is a case-insensitive exact basename match. Most
	// project indicators are this shape (go.mod, package.json,
	// Cargo.toml, pyproject.toml, Gemfile, pom.xml).
	HasFile string
	// HasGlob is a glob (filepath.Match) over basenames. Used by
	// Terraform (`*.tf`), .NET (`*.csproj`), and similar
	// extension-based project signals.
	HasGlob string
}

// String describes the indicator in a human-readable form, surfaced
// to MCP / CLI consumers so they can see WHY a project matched.
func (i Indicator) String() string {
	if i.HasFile != "" {
		return i.HasFile
	}
	return i.HasGlob
}

// Match couples a matched ProjectType with the indicator that fired.
// Surfaced in detect_project and find_projects output so consumers
// can audit detection decisions.
type Match struct {
	Type      string `json:"type"`
	Indicator string `json:"indicator"`
}

// Registry holds the registered project types. Built-in types
// self-register via init() into DefaultRegistry; future custom-type
// registration (CEL-driven) will use the same path.
type Registry struct {
	mu    sync.RWMutex
	types []*ProjectType
}

var defaultRegistry = &Registry{}

// Register adds a project type to the default registry. Called from
// init() in builtins.go (and, eventually, from CEL-driven custom-type
// config loaders).
func Register(t *ProjectType) {
	defaultRegistry.mu.Lock()
	defaultRegistry.types = append(defaultRegistry.types, t)
	defaultRegistry.mu.Unlock()
}

// DefaultRegistry returns the package-level registry — the singleton
// every consumer (MCP tool, CLI subcommand) uses.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// Types returns a snapshot of every registered project type, sorted
// by Name. Used by --list-style help output and tests.
func (r *Registry) Types() []*ProjectType {
	r.mu.RLock()
	out := make([]*ProjectType, len(r.types))
	copy(out, r.types)
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// match reports whether any indicator on this ProjectType fires
// against the supplied directory listing. Returns the matching
// indicator on success; empty Indicator + false on no match.
func (t *ProjectType) match(listing []string) (Indicator, bool) {
	for _, ind := range t.Indicators {
		for _, name := range listing {
			if ind.HasFile != "" && equalFold(name, ind.HasFile) {
				return ind, true
			}
			if ind.HasGlob != "" {
				ok, err := filepath.Match(ind.HasGlob, name)
				if err == nil && ok {
					return ind, true
				}
			}
		}
	}
	return Indicator{}, false
}

// equalFold is filepath-style case-insensitive comparison for ASCII
// basenames — same convention as the content detector's
// FilenameMatcher pass (#94).
func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
