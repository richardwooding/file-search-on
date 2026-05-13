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
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/google/cel-go/cel"
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

	// BuildExcludes is the list of canonical build-artefact
	// basenames typically present in this kind of project (e.g.
	// "vendor" for Go, "node_modules" for Node, "target" for Rust).
	// The walker unions these into its excludes when
	// search.Options.PruneBuildArtefacts is set, so a search over a
	// monorepo doesn't grovel through dependency caches by default.
	// Empty for project types that have no canonical artefact dir.
	BuildExcludes []string

	// compiled holds the cel.Program for each Indicator that uses
	// CELExpr. Same length as Indicators when set; nil entries for
	// HasFile / HasGlob indicators. Built by Register() so the
	// hot path doesn't re-compile per evaluation.
	compiled []cel.Program
}

// Indicator is a single match rule against a directory's contents.
// Exactly one of HasFile / HasGlob / CELExpr should be set per
// indicator.
type Indicator struct {
	// HasFile is a case-insensitive exact basename match. Most
	// built-in project indicators are this shape (go.mod,
	// package.json, Cargo.toml, pyproject.toml, Gemfile, pom.xml).
	HasFile string
	// HasGlob is a glob (filepath.Match) over basenames. Used by
	// Terraform (`*.tf`), .NET (`*.csproj`), and similar
	// extension-based project signals.
	HasGlob string
	// CELExpr is a CEL expression evaluated against a directory
	// context with two list-of-string variables:
	//
	//   files    — basenames of files in the inspected dir
	//   subdirs  — basenames of immediate subdirectories
	//
	// Example: `"services" in subdirs && "foo.yaml" in files`. The
	// expression must compile at Register() time; bad CEL fails
	// the registration. Used by user-defined custom project types
	// loaded from --project-type-config YAML.
	CELExpr string
}

// String describes the indicator in a human-readable form, surfaced
// to MCP / CLI consumers so they can see WHY a project matched.
func (i Indicator) String() string {
	switch {
	case i.HasFile != "":
		return i.HasFile
	case i.HasGlob != "":
		return i.HasGlob
	case i.CELExpr != "":
		return "cel:" + i.CELExpr
	}
	return ""
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

// NewRegistry returns an empty Registry — useful for tests that want
// isolation from the package-level defaultRegistry. Production code
// almost always uses DefaultRegistry().
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a project type to this registry. Compiles any
// CEL-expression indicators eagerly — bad CEL is returned as an
// error so callers (config loaders, tests) can surface it cleanly.
func (r *Registry) Register(t *ProjectType) error {
	if err := compileIndicators(t); err != nil {
		return fmt.Errorf("Register(%q): %w", t.Name, err)
	}
	r.mu.Lock()
	r.types = append(r.types, t)
	r.mu.Unlock()
	return nil
}

// Register adds a project type to the package-level default
// registry. Panics on CEL compile failure — appropriate for init()
// callers (built-in types) where a bad indicator is a programming
// bug. Config-driven callers should use Registry.Register on
// DefaultRegistry() directly to get an error return.
func Register(t *ProjectType) {
	if err := defaultRegistry.Register(t); err != nil {
		panic(fmt.Errorf("projecttype.Register(%q): %w", t.Name, err))
	}
}

// compileIndicators populates ProjectType.compiled with cel.Program
// entries for every CELExpr indicator. HasFile / HasGlob indicators
// get a nil slot at the same index. Returns the first compile error
// (with the indicator's CEL source attached for debuggability).
func compileIndicators(t *ProjectType) error {
	if !hasCELIndicator(t.Indicators) {
		t.compiled = nil
		return nil
	}
	progs := make([]cel.Program, len(t.Indicators))
	for i, ind := range t.Indicators {
		if ind.CELExpr == "" {
			continue
		}
		prog, err := compileDirCEL(ind.CELExpr)
		if err != nil {
			return fmt.Errorf("indicator[%d] CEL compile: %w", i, err)
		}
		progs[i] = prog
	}
	t.compiled = progs
	return nil
}

func hasCELIndicator(inds []Indicator) bool {
	for _, ind := range inds {
		if ind.CELExpr != "" {
			return true
		}
	}
	return false
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
// files contains file basenames; subdirs contains immediate
// subdirectory basenames. CEL indicators evaluate against both.
func (t *ProjectType) match(files, subdirs []string) (Indicator, bool) {
	for i, ind := range t.Indicators {
		switch {
		case ind.HasFile != "":
			for _, name := range files {
				if equalFold(name, ind.HasFile) {
					return ind, true
				}
			}
		case ind.HasGlob != "":
			for _, name := range files {
				if ok, err := filepath.Match(ind.HasGlob, name); err == nil && ok {
					return ind, true
				}
			}
		case ind.CELExpr != "" && i < len(t.compiled) && t.compiled[i] != nil:
			if evalDirCEL(t.compiled[i], files, subdirs) {
				return ind, true
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
