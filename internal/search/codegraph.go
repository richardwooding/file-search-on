package search

import (
	"context"
	"errors"
	"maps"
	"regexp"
	"sort"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// Importer is one source file that imports a queried module.
type Importer struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
}

// SymbolDef is one file that defines a queried symbol. Kind is
// "function" or "type" — the two symbol classes the per-language
// extractors populate (functions / type_names).
type SymbolDef struct {
	Path     string `json:"path"`
	Language string `json:"language,omitempty"`
	Kind     string `json:"kind"`
	// Symbol is the symbol name. Set by DeadCode (where the caller doesn't
	// supply the name); empty for FindDefinition (caller already knows it).
	Symbol string `json:"symbol,omitempty"`
}

// ModuleFanIn is a module ranked by how many files import it.
type ModuleFanIn struct {
	Module string `json:"module"`
	Count  int    `json:"count"`
}

// FileFanOut is a file ranked by how many modules it imports.
type FileFanOut struct {
	Path    string `json:"path"`
	Imports int    `json:"imports"`
}

// DuplicateSymbol is a symbol name+kind defined in more than one file —
// surfaces name collisions, overloads across files, and copy-paste
// definitions.
type DuplicateSymbol struct {
	Symbol string   `json:"symbol"`
	Kind   string   `json:"kind"`
	Paths  []string `json:"paths"`
}

// LanguageCount is the file count for one language in the walked set.
type LanguageCount struct {
	Language string `json:"language"`
	Files    int64  `json:"files"`
}

// CodeGraphOverview is the project-wide summary returned by Overview.
type CodeGraphOverview struct {
	TotalFiles      int64             `json:"total_files"`
	DistinctModules int64             `json:"distinct_modules"`
	DistinctSymbols int64             `json:"distinct_symbols"`
	Languages       []LanguageCount   `json:"languages"`
	ImportHubs      []ModuleFanIn     `json:"import_hubs"`
	HighFanOut      []FileFanOut      `json:"high_fan_out"`
	DuplicateDefs   []DuplicateSymbol `json:"duplicate_definitions,omitempty"`
}

// symbolEntry is one (path, kind) definition of a symbol name.
type symbolEntry struct {
	path     string
	language string
	kind     string
}

// CodeGraph is the in-memory cross-file index built from the per-file
// imports / functions / type_names lists the source extractors already
// produce. It is keyed by import string and by symbol name so the
// query methods (ImportedBy / FindDefinition / Overview) are O(1) /
// O(matches) lookups rather than re-walks.
//
// Cancelled / CancellationReason mirror the search / stats / duplicates
// tools' partial-result fields: a timed-out build still returns a usable
// graph over the files seen before the deadline.
type CodeGraph struct {
	TotalFiles         int64
	Cancelled          bool
	CancellationReason string

	// importedBy: module import string -> set of importing file paths
	// (path -> language, so a file importing a module twice counts once).
	importedBy map[string]map[string]string
	// definedIn: symbol name -> every (path, kind) that defines it.
	definedIn map[string][]symbolEntry
	// referencedBy: callee name -> set of referencing file paths
	// (path -> language). The call-site half of the graph (#363).
	referencedBy map[string]map[string]string
	// callsByName: caller function name -> set of callee names it calls,
	// unioned across every file (name-based). Powers Calls (#368).
	callsByName map[string]map[string]bool
	// fanOut: file path -> language + number of modules it imports.
	fanOut map[string]fileFanOut
	// languages: language -> file count.
	languages map[string]int64
	// testFiles: set of file paths the detector flagged is_test_file.
	// Powers TestGaps (#394) — a function is "tested" when referenced
	// from one of these.
	testFiles map[string]bool
}

// refExtractionLangs is the set of languages for which references
// (call sites) are extracted — Go (go/ast) + the tree-sitter languages.
// DeadCode only considers definitions in these languages: a symbol in a
// language with no reference extraction would always look "dead".
var refExtractionLangs = map[string]bool{
	"go": true, "rust": true, "typescript": true, "javascript": true,
	"ruby": true, "swift": true, "kotlin": true, "c": true, "cpp": true,
}

type fileFanOut struct {
	language string
	imports  int
}

// BuildCodeGraph walks opts.Root / opts.Roots and aggregates the
// per-file symbol lists into a cross-file CodeGraph. It forces
// IncludeAttributes (the symbol lists live on Result.Attrs.Extra) and
// wipes Sort / Limit / Snippet / Body so an inherited Options can't drop
// matches or pay for body reads. Modelled on FindDuplicates.
//
// Callers scope the walk via opts.Expr — typically "is_source" (the
// MCP tools and CLI default to it). Non-source files carry no symbol
// lists and contribute nothing but a TotalFiles increment.
func BuildCodeGraph(ctx context.Context, opts Options, registry *content.Registry) (*CodeGraph, error) {
	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	opts.IncludeSnippet = false
	opts.IncludeBody = false

	results, walkErr := Walk(ctx, opts, registry)

	g := &CodeGraph{
		importedBy:   map[string]map[string]string{},
		definedIn:    map[string][]symbolEntry{},
		referencedBy: map[string]map[string]string{},
		callsByName:  map[string]map[string]bool{},
		fanOut:       map[string]fileFanOut{},
		languages:    map[string]int64{},
		testFiles:    map[string]bool{},
	}
	g.TotalFiles = int64(len(results))

	for _, r := range results {
		if r.Attrs == nil {
			continue
		}
		lang, _ := r.Attrs.Extra["language"].(string)
		if lang != "" {
			g.languages[lang]++
		}
		if t, _ := r.Attrs.Extra["is_test_file"].(bool); t {
			g.testFiles[r.Path] = true
		}

		imports, _ := r.Attrs.Extra["imports"].([]string)
		g.fanOut[r.Path] = fileFanOut{language: lang, imports: len(imports)}
		for _, imp := range imports {
			set := g.importedBy[imp]
			if set == nil {
				set = map[string]string{}
				g.importedBy[imp] = set
			}
			set[r.Path] = lang
		}

		if funcs, ok := r.Attrs.Extra["functions"].([]string); ok {
			for _, fn := range funcs {
				g.definedIn[fn] = append(g.definedIn[fn], symbolEntry{path: r.Path, language: lang, kind: "function"})
			}
		}
		if types, ok := r.Attrs.Extra["type_names"].([]string); ok {
			for _, t := range types {
				g.definedIn[t] = append(g.definedIn[t], symbolEntry{path: r.Path, language: lang, kind: "type"})
			}
		}
		if refs, ok := r.Attrs.Extra["references"].([]string); ok {
			for _, ref := range refs {
				set := g.referencedBy[ref]
				if set == nil {
					set = map[string]string{}
					g.referencedBy[ref] = set
				}
				set[r.Path] = lang
			}
		}
		if edges, ok := r.Attrs.Extra["call_edges"].([]string); ok {
			for _, e := range edges {
				caller, callee, found := strings.Cut(e, "\x00")
				if !found || caller == "" || callee == "" {
					continue
				}
				set := g.callsByName[caller]
				if set == nil {
					set = map[string]bool{}
					g.callsByName[caller] = set
				}
				set[callee] = true
			}
		}
	}

	if walkErr != nil {
		switch {
		case errors.Is(walkErr, context.Canceled):
			g.Cancelled = true
			g.CancellationReason = "client_cancel"
			return g, nil
		case errors.Is(walkErr, context.DeadlineExceeded):
			g.Cancelled = true
			g.CancellationReason = "timeout"
			return g, nil
		}
		return g, walkErr
	}
	if ctx.Err() != nil {
		g.Cancelled = true
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			g.CancellationReason = "timeout"
		} else {
			g.CancellationReason = "client_cancel"
		}
	}
	return g, nil
}

// ImportedBy returns every file that imports a module matching the
// query. mode selects the match semantics:
//
//	"" / "exact" — module string matches exactly
//	"prefix"     — module string is a prefix of the import (e.g. a
//	               package path that owns several sub-imports)
//	"regex"      — module is an RE2 pattern matched against each import
//
// A file importing several matched modules is returned once. Results
// are sorted by path.
func (g *CodeGraph) ImportedBy(module, mode string) ([]Importer, error) {
	merged := map[string]string{} // path -> language

	collect := func(set map[string]string) {
		maps.Copy(merged, set)
	}

	switch mode {
	case "", "exact":
		collect(g.importedBy[module])
	case "prefix":
		for imp, set := range g.importedBy {
			if strings.HasPrefix(imp, module) {
				collect(set)
			}
		}
	case "regex":
		re, err := regexp.Compile(module)
		if err != nil {
			return nil, err
		}
		for imp, set := range g.importedBy {
			if re.MatchString(imp) {
				collect(set)
			}
		}
	default:
		return nil, errors.New(`mode must be one of "exact", "prefix", "regex"`)
	}

	out := make([]Importer, 0, len(merged))
	for p, lang := range merged {
		out = append(out, Importer{Path: p, Language: lang})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// FindDefinition returns every file that defines a symbol with the
// given exact name. kind, when non-empty, filters to "function" or
// "type". Results are sorted by path. Only the languages with symbol
// extraction (Go + Python / Java / C# / PHP / Perl / R / MATLAB /
// Scala) populate functions / type_names, so symbols defined only in
// other languages won't appear.
func (g *CodeGraph) FindDefinition(symbol, kind string) []SymbolDef {
	entries := g.definedIn[symbol]
	out := make([]SymbolDef, 0, len(entries))
	// Dedupe by (path, kind): a single file can define the same name more
	// than once (e.g. methods named String on two Go types both surface as
	// the bare "String"), and we want one row per file per kind.
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if kind != "" && e.kind != kind {
			continue
		}
		key := e.kind + "\x00" + e.path
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, SymbolDef{Path: e.path, Language: e.language, Kind: e.kind})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// WhoCalls returns every file that references (calls) the given exact
// symbol name, sorted by path. Name-based: a call `pkg.Foo()` is keyed
// by "Foo". Reference extraction covers Go + the tree-sitter languages;
// callers in other languages won't appear.
func (g *CodeGraph) WhoCalls(name string) []Importer {
	set := g.referencedBy[name]
	out := make([]Importer, 0, len(set))
	for p, lang := range set {
		out = append(out, Importer{Path: p, Language: lang})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Calls returns the distinct callee names that any function named `name`
// invokes, sorted. Name-based and unioned across every file that defines
// a function with that name. Covers Go + the tree-sitter languages whose
// grammar exposes function spans; same heuristic caveats as WhoCalls.
func (g *CodeGraph) Calls(name string) []string {
	set := g.callsByName[name]
	out := make([]string, 0, len(set))
	for callee := range set {
		out = append(out, callee)
	}
	sort.Strings(out)
	return out
}

// ImpactNode is one symbol in a transitive-impact closure: the dependent
// function's name, the BFS depth at which it was reached (1 = a direct
// caller), and the file(s) that define it.
type ImpactNode struct {
	Symbol string   `json:"symbol"`
	Depth  int      `json:"depth"`
	Paths  []string `json:"paths,omitempty"`
}

// Impact returns the transitive closure of functions that (directly or
// indirectly) call symbol — the blast radius of changing it (issue #396).
// BFS over the reverse of the per-function call graph (callsByName), so a
// caller appears once, at its shortest depth. maxDepth caps the hops
// (<= 0 = unbounded); cycles are handled by the visited set.
//
// Name-based, same caveats as WhoCalls / Calls: same-name collisions,
// interface / reflection dispatch, and table-driven indirection can over- or
// under-count. The complementary IMPORT-level transitive closure ("what
// transitively imports this file") needs file→package resolution the graph
// doesn't carry yet (see issue #393) and is out of scope here.
func (g *CodeGraph) Impact(symbol string, maxDepth int) []ImpactNode {
	// Reverse the forward call edges: callee -> set of direct callers.
	callers := map[string]map[string]bool{}
	for caller, callees := range g.callsByName {
		for callee := range callees {
			if callers[callee] == nil {
				callers[callee] = map[string]bool{}
			}
			callers[callee][caller] = true
		}
	}

	visited := map[string]int{} // dependent symbol -> shortest depth
	frontier := keysOf(callers[symbol])
	for depth := 1; len(frontier) > 0 && (maxDepth <= 0 || depth <= maxDepth); depth++ {
		var next []string
		for _, name := range frontier {
			if name == symbol {
				continue // a self-recursive function isn't its own dependent
			}
			if _, seen := visited[name]; seen {
				continue
			}
			visited[name] = depth
			for c := range callers[name] {
				if _, seen := visited[c]; !seen && c != symbol {
					next = append(next, c)
				}
			}
		}
		frontier = next
	}

	out := make([]ImpactNode, 0, len(visited))
	for name, depth := range visited {
		out = append(out, ImpactNode{Symbol: name, Depth: depth, Paths: definingPaths(g.definedIn[name])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Depth != out[j].Depth {
			return out[i].Depth < out[j].Depth
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

// CallPathStep is one node on a call path: the function name and the file(s)
// that define it.
type CallPathStep struct {
	Symbol string   `json:"symbol"`
	Paths  []string `json:"paths,omitempty"`
}

// CallPath returns the shortest call path from `from` to `to` — the route by
// which `from` (in)directly calls `to` — as an ordered list of steps
// (from … to). Returns nil when `to` is unreachable from `from`. BFS over the
// forward per-function call graph (callsByName); among equal-length paths the
// callee-sorted traversal makes the choice deterministic. maxDepth caps the
// hops (<= 0 = unbounded). Answers "how does A reach B?" — the route, where
// Impact gives the whole closure.
//
// Name-based, same caveats as Impact / Calls (same-name collisions, interface
// / reflection dispatch).
func (g *CodeGraph) CallPath(from, to string, maxDepth int) []CallPathStep {
	if from == to {
		return []CallPathStep{{Symbol: from, Paths: definingPaths(g.definedIn[from])}}
	}

	parent := map[string]string{from: ""}
	depth := map[string]int{from: 0}
	queue := []string{from}
	found := false
	for len(queue) > 0 && !found {
		cur := queue[0]
		queue = queue[1:]
		if maxDepth > 0 && depth[cur] >= maxDepth {
			continue
		}
		callees := keysOf(g.callsByName[cur])
		sort.Strings(callees) // deterministic among equal-length paths
		for _, c := range callees {
			if _, seen := parent[c]; seen {
				continue
			}
			parent[c] = cur
			depth[c] = depth[cur] + 1
			if c == to {
				found = true
				break
			}
			queue = append(queue, c)
		}
	}
	if !found {
		return nil
	}

	// Reconstruct from -> to via parent pointers, then reverse.
	var rev []string
	for n := to; n != ""; n = parent[n] {
		rev = append(rev, n)
		if n == from {
			break
		}
	}
	steps := make([]CallPathStep, 0, len(rev))
	for i := len(rev) - 1; i >= 0; i-- {
		steps = append(steps, CallPathStep{Symbol: rev[i], Paths: definingPaths(g.definedIn[rev[i]])})
	}
	return steps
}

// keysOf returns the keys of a string-set as a slice (unordered).
func keysOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// definingPaths returns the sorted distinct file paths that define a symbol.
func definingPaths(entries []symbolEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if !seen[e.path] {
			seen[e.path] = true
			out = append(out, e.path)
		}
	}
	sort.Strings(out)
	return out
}

// DeadCode returns defined functions / types whose name never appears as
// a reference anywhere in the walked set — candidate dead code. Restricted
// to definitions in languages with reference extraction (refExtractionLangs),
// since a definition in a language we don't scan for calls would always
// look unreferenced.
//
// HEURISTIC, name-based: exported/public API used only by external callers,
// dynamic dispatch, reflection, and same-name collisions all produce false
// positives. Callers must present results as candidates, never authoritative.
func (g *CodeGraph) DeadCode() []SymbolDef {
	var out []SymbolDef
	seen := map[string]bool{}
	for name, entries := range g.definedIn {
		if _, referenced := g.referencedBy[name]; referenced {
			continue
		}
		for _, e := range entries {
			if !refExtractionLangs[e.language] {
				continue
			}
			if isReflectionDispatchedEntry(e.kind, name, e.path, e.language) {
				continue
			}
			key := e.kind + "\x00" + name + "\x00" + e.path
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, SymbolDef{Path: e.path, Language: e.language, Kind: e.kind, Symbol: name})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Symbol != out[j].Symbol {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// isReflectionDispatchedEntry reports whether a definition is an entry point
// invoked by the runtime / a framework rather than by a static call — so it
// never appears in the call-reference graph and would always be a dead-code
// false positive (issue #385). Well-known cases:
//
//   - Go package-init / program entry points (`init`, `main`): run by the Go
//     runtime, never statically called — reporting them is pure noise (they
//     dominated dead_code output on real Go trees, e.g. 57 `init`s here).
//   - Go test-runner entry points (TestXxx / BenchmarkXxx / FuzzXxx /
//     ExampleXxx in a *_test.go file): run by `go test`, never called.
//     Unused test *helpers* are still reported — only the runner entry
//     points are excluded.
//   - CLI command types (the kong / cobra `…Cmd` convention): dispatched off
//     struct-tag reflection, so they're referenced only as field types, which
//     the call-reference graph doesn't track.
func isReflectionDispatchedEntry(kind, name, path, language string) bool {
	if language == "go" && kind == "function" && (name == "init" || name == "main") {
		return true
	}
	if language == "go" && strings.HasSuffix(path, "_test.go") && isGoTestEntry(kind, name) {
		return true
	}
	if kind != "function" && kind != "method" && strings.HasSuffix(name, "Cmd") {
		return true
	}
	return false
}

// isGoTestEntry reports whether name is a Go test-runner entry point — a
// function named Test/Benchmark/Fuzz/Example optionally followed by a
// non-lowercase rune (the convention `go test` matches). "Tester" is a normal
// function, not an entry point.
func isGoTestEntry(kind, name string) bool {
	if kind != "function" {
		return false
	}
	for _, p := range [...]string{"Test", "Benchmark", "Fuzz", "Example"} {
		rest, ok := strings.CutPrefix(name, p)
		if !ok {
			continue
		}
		if rest == "" {
			return true
		}
		if r := rest[0]; r < 'a' || r > 'z' {
			return true
		}
	}
	return false
}

// TestGap is one production source file with functions that no test
// references (issue #394).
type TestGap struct {
	Path              string   `json:"path"`
	Language          string   `json:"language"`
	FunctionCount     int      `json:"function_count"`
	UntestedCount     int      `json:"untested_count"`
	UntestedFunctions []string `json:"untested_functions"`
	FullyUntested     bool     `json:"fully_untested"`
}

// TestGaps returns production source files whose functions are never
// referenced from a test file — candidate untested code (issue #394).
// Restricted to languages with reference extraction (refExtractionLangs);
// functions defined in test files are themselves excluded.
//
// HEURISTIC, name-based and DIRECT-reference only: a function exercised only
// transitively (test → A → B, with B never named in a test) reads as
// untested, and same-name collisions / reflection / table-driven dispatch
// can mislead either way. Present as candidates — pair with a real coverage
// profile for precision. Mirrors DeadCode's machinery (defined-but-not-
// referenced), filtered to "not referenced *from a test*".
func (g *CodeGraph) TestGaps() []TestGap {
	type fileInfo struct {
		lang  string
		funcs []string
	}
	byFile := map[string]*fileInfo{}
	for name, entries := range g.definedIn {
		for _, e := range entries {
			if e.kind != "function" || !refExtractionLangs[e.language] || g.testFiles[e.path] {
				continue
			}
			fi := byFile[e.path]
			if fi == nil {
				fi = &fileInfo{lang: e.language}
				byFile[e.path] = fi
			}
			fi.funcs = append(fi.funcs, name)
		}
	}

	out := make([]TestGap, 0, len(byFile))
	for path, fi := range byFile {
		var untested []string
		for _, name := range fi.funcs {
			if !g.referencedFromTest(name) {
				untested = append(untested, name)
			}
		}
		if len(untested) == 0 {
			continue
		}
		sort.Strings(untested)
		out = append(out, TestGap{
			Path:              path,
			Language:          fi.lang,
			FunctionCount:     len(fi.funcs),
			UntestedCount:     len(untested),
			UntestedFunctions: untested,
			FullyUntested:     len(untested) == len(fi.funcs),
		})
	}

	// Fully-untested files first (the scary gaps), then by untested count
	// desc, then path for determinism.
	sort.Slice(out, func(i, j int) bool {
		if out[i].FullyUntested != out[j].FullyUntested {
			return out[i].FullyUntested
		}
		if out[i].UntestedCount != out[j].UntestedCount {
			return out[i].UntestedCount > out[j].UntestedCount
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// referencedFromTest reports whether any file referencing name is a test file.
func (g *CodeGraph) referencedFromTest(name string) bool {
	for path := range g.referencedBy[name] {
		if g.testFiles[path] {
			return true
		}
	}
	return false
}

// Overview computes the project-wide summary: import hubs (modules with
// the highest fan-in), files with the highest fan-out, symbols defined
// in more than one file, and a language breakdown. top caps each ranked
// list (<= 0 defaults to 20).
func (g *CodeGraph) Overview(top int) CodeGraphOverview {
	if top <= 0 {
		top = 20
	}
	ov := CodeGraphOverview{
		TotalFiles:      g.TotalFiles,
		DistinctModules: int64(len(g.importedBy)),
		DistinctSymbols: int64(len(g.definedIn)),
	}

	// Languages, sorted by file count desc then name.
	for lang, n := range g.languages {
		ov.Languages = append(ov.Languages, LanguageCount{Language: lang, Files: n})
	}
	sort.Slice(ov.Languages, func(i, j int) bool {
		if ov.Languages[i].Files != ov.Languages[j].Files {
			return ov.Languages[i].Files > ov.Languages[j].Files
		}
		return ov.Languages[i].Language < ov.Languages[j].Language
	})

	// Import hubs — modules by importer count.
	hubs := make([]ModuleFanIn, 0, len(g.importedBy))
	for mod, set := range g.importedBy {
		hubs = append(hubs, ModuleFanIn{Module: mod, Count: len(set)})
	}
	sort.Slice(hubs, func(i, j int) bool {
		if hubs[i].Count != hubs[j].Count {
			return hubs[i].Count > hubs[j].Count
		}
		return hubs[i].Module < hubs[j].Module
	})
	ov.ImportHubs = capFanIn(hubs, top)

	// High fan-out — files importing the most modules.
	fanOut := make([]FileFanOut, 0, len(g.fanOut))
	for path, info := range g.fanOut {
		if info.imports == 0 {
			continue
		}
		fanOut = append(fanOut, FileFanOut{Path: path, Imports: info.imports})
	}
	sort.Slice(fanOut, func(i, j int) bool {
		if fanOut[i].Imports != fanOut[j].Imports {
			return fanOut[i].Imports > fanOut[j].Imports
		}
		return fanOut[i].Path < fanOut[j].Path
	})
	ov.HighFanOut = capFanOut(fanOut, top)

	// Duplicate definitions — same (name, kind) in >1 distinct file.
	ov.DuplicateDefs = duplicateDefs(g.definedIn)
	if len(ov.DuplicateDefs) > top {
		ov.DuplicateDefs = ov.DuplicateDefs[:top]
	}

	return ov
}

func capFanIn(s []ModuleFanIn, top int) []ModuleFanIn {
	if len(s) > top {
		return s[:top]
	}
	return s
}

func capFanOut(s []FileFanOut, top int) []FileFanOut {
	if len(s) > top {
		return s[:top]
	}
	return s
}

// duplicateDefs groups the definition index by (name, kind) and returns
// every group spanning more than one distinct file, sorted by file
// count desc then name. Pure — unit-tested directly.
func duplicateDefs(definedIn map[string][]symbolEntry) []DuplicateSymbol {
	var out []DuplicateSymbol
	for name, entries := range definedIn {
		byKind := map[string]map[string]bool{} // kind -> set of paths
		for _, e := range entries {
			set := byKind[e.kind]
			if set == nil {
				set = map[string]bool{}
				byKind[e.kind] = set
			}
			set[e.path] = true
		}
		for kind, paths := range byKind {
			if len(paths) < 2 {
				continue
			}
			ps := make([]string, 0, len(paths))
			for p := range paths {
				ps = append(ps, p)
			}
			sort.Strings(ps)
			out = append(out, DuplicateSymbol{Symbol: name, Kind: kind, Paths: ps})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Paths) != len(out[j].Paths) {
			return len(out[i].Paths) > len(out[j].Paths)
		}
		if out[i].Symbol != out[j].Symbol {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
