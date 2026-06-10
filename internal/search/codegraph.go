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
	// fanOut: file path -> language + number of modules it imports.
	fanOut map[string]fileFanOut
	// languages: language -> file count.
	languages map[string]int64
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
		importedBy: map[string]map[string]string{},
		definedIn:  map[string][]symbolEntry{},
		fanOut:     map[string]fileFanOut{},
		languages:  map[string]int64{},
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
