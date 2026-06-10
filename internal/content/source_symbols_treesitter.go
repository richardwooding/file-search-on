package content

import (
	"strings"
	"sync"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Tree-sitter-backed symbol extraction for languages without a
// hand-rolled extractor. Definitions (functions / type_names) come from
// the grammar's bundled tags query; imports come from a small
// per-language query (the library's built-in import extractor only
// covers go / java / python, none of which route here).
//
// The grammars are embedded at build time via the `grammar_subset` +
// `grammar_subset_<lang>` build tags (see CLAUDE.md / .goreleaser.yaml);
// a build without those tags embeds all ~206 grammars (~+22 MB).

// tsDetectFile maps our canonical `language` string to a representative
// filename so grammars.DetectLanguage resolves the right LangEntry.
var tsDetectFile = map[string]string{
	"rust":       "x.rs",
	"typescript": "x.ts",
	"javascript": "x.js",
	"ruby":       "x.rb",
	"swift":      "x.swift",
	"kotlin":     "x.kt",
	"c":          "x.c",
	"cpp":        "x.cpp",
}

// tsDefQuery supplements the grammar's bundled tags query for languages
// whose tags query is incomplete: Ruby's captures only `require`,
// Swift's/Kotlin's capture only top-level classes. Captures @function /
// @type; run in addition to the tags query (results deduped).
var tsDefQuery = map[string]string{
	"ruby": `(method name: (identifier) @function)
(singleton_method name: (identifier) @function)
(class name: (constant) @type)
(module name: (constant) @type)`,
	"swift": `(function_declaration (simple_identifier) @function)
(protocol_function_declaration (simple_identifier) @function)
(class_declaration (type_identifier) @type)
(protocol_declaration (type_identifier) @type)`,
	"kotlin": `(function_declaration (simple_identifier) @function)
(class_declaration (type_identifier) @type)
(object_declaration (type_identifier) @type)`,
}

// tsImportQuery is the per-language tree-sitter query capturing the
// import path as @import. Empty/missing → imports left unpopulated.
var tsImportQuery = map[string]string{
	"rust":       `(use_declaration argument: (_) @import)`,
	"typescript": `(import_statement source: (string) @import)`,
	"javascript": `(import_statement source: (string) @import)`,
	"ruby":       `((call method: (identifier) @_m arguments: (argument_list (string) @import)) (#match? @_m "^require"))`,
	"swift":      `(import_declaration (identifier) @import)`,
	"kotlin":     `(import_header (identifier) @import)`,
	"c":          `(preproc_include path: (_) @import)`,
	"cpp":        `(preproc_include path: (_) @import)`,
}

// tsRefQuery is the per-language query capturing call-site callee names
// as @reference. Powers who_calls / dead_code (issue #363). Bare names
// only (name-based resolution); method/field calls capture the method
// name. Empty/missing → references left unpopulated.
var tsRefQuery = map[string]string{
	"rust": `(call_expression function: (identifier) @reference)
(call_expression function: (scoped_identifier name: (identifier) @reference))
(call_expression function: (field_expression field: (field_identifier) @reference))
(macro_invocation macro: (identifier) @reference)`,
	"typescript": `(call_expression function: (identifier) @reference)
(call_expression function: (member_expression property: (property_identifier) @reference))`,
	"javascript": `(call_expression function: (identifier) @reference)
(call_expression function: (member_expression property: (property_identifier) @reference))`,
	"ruby": `(call method: (identifier) @reference)`,
	"swift": `(call_expression (simple_identifier) @reference)
(call_expression (navigation_expression suffix: (navigation_suffix (simple_identifier) @reference)))`,
	"kotlin": `(call_expression (simple_identifier) @reference)
(call_expression (navigation_expression (navigation_suffix (simple_identifier) @reference)))`,
	"c":   `(call_expression function: (identifier) @reference)`,
	"cpp": `(call_expression function: (identifier) @reference)
(call_expression function: (field_expression field: (field_identifier) @reference))`,
}

// tsLang holds the concurrent-safe machinery for one language: a
// ParserPool (safe for concurrent Parse) plus compiled Query objects
// (safe for concurrent Execute after construction). Built once per
// language on first use.
type tsLang struct {
	pool        *ts.ParserPool
	tagsQuery   *ts.Query
	defQuery    *ts.Query // supplemental @function/@type; nil when none
	importQuery *ts.Query // nil when none configured or compile failed
	refQuery    *ts.Query // @reference call-site callees; nil when none
}

var (
	tsMu    sync.Mutex
	tsCache = map[string]*tsLang{} // language -> *tsLang; nil value = unsupported
)

// tsLangFor lazily builds (and caches) the tree-sitter machinery for a
// language. Returns nil when the language isn't tree-sitter-backed or
// its grammar/tags query is unavailable.
func tsLangFor(language string) *tsLang {
	tsMu.Lock()
	defer tsMu.Unlock()
	if tl, ok := tsCache[language]; ok {
		return tl
	}
	tl := buildTSLang(language)
	tsCache[language] = tl
	return tl
}

func buildTSLang(language string) *tsLang {
	sample, ok := tsDetectFile[language]
	if !ok {
		return nil
	}
	entry := grammars.DetectLanguage(sample)
	if entry == nil {
		return nil
	}
	lang := entry.Language()
	if lang == nil {
		return nil
	}
	tagsSrc := grammars.ResolveTagsQuery(*entry)
	if tagsSrc == "" {
		return nil
	}
	tagsQ, err := ts.NewQuery(tagsSrc, lang)
	if err != nil {
		return nil
	}
	tl := &tsLang{pool: ts.NewParserPool(lang), tagsQuery: tagsQ}
	if q := tsDefQuery[language]; q != "" {
		if defQ, err := ts.NewQuery(q, lang); err == nil {
			tl.defQuery = defQ
		}
	}
	if q := tsImportQuery[language]; q != "" {
		if impQ, err := ts.NewQuery(q, lang); err == nil {
			tl.importQuery = impQ
		}
	}
	if q := tsRefQuery[language]; q != "" {
		if refQ, err := ts.NewQuery(q, lang); err == nil {
			tl.refQuery = refQ
		}
	}
	return tl
}

// extractTreeSitterSymbols parses src with the language's grammar and
// returns the function / type / import names. Matches the signature of
// the hand-rolled extractXxxSymbols functions. Returns all-nil when the
// language isn't tree-sitter-backed.
func extractTreeSitterSymbols(language string, src []byte) (functions, types, imports, references []string) {
	tl := tsLangFor(language)
	if tl == nil {
		return nil, nil, nil, nil
	}
	tree, err := tl.pool.Parse(src)
	if err != nil || tree == nil {
		return nil, nil, nil, nil
	}

	for _, m := range tl.tagsQuery.Execute(tree) {
		var name, kind string
		for _, c := range m.Captures {
			switch {
			case c.Name == "name":
				name = c.Text(src)
			case strings.HasPrefix(c.Name, "definition."):
				kind = c.Name[len("definition."):]
			}
		}
		if name == "" {
			continue
		}
		switch kind {
		case "function", "method", "macro", "constructor":
			functions = append(functions, name)
		case "class", "struct", "interface", "enum", "trait", "type", "module", "union", "protocol", "namespace":
			types = append(types, name)
		}
	}

	// Supplemental definition query for languages with weak bundled tags
	// (ruby / swift / kotlin): captures @function / @type directly.
	if tl.defQuery != nil {
		for _, m := range tl.defQuery.Execute(tree) {
			for _, c := range m.Captures {
				switch c.Name {
				case "function":
					functions = append(functions, c.Text(src))
				case "type":
					types = append(types, c.Text(src))
				}
			}
		}
	}

	if tl.importQuery != nil {
		for _, m := range tl.importQuery.Execute(tree) {
			for _, c := range m.Captures {
				if c.Name == "import" {
					if p := strings.Trim(c.Text(src), "\"'`<>"); p != "" {
						imports = append(imports, p)
					}
				}
			}
		}
	}

	if tl.refQuery != nil {
		for _, m := range tl.refQuery.Execute(tree) {
			for _, c := range m.Captures {
				if c.Name == "reference" {
					if r := c.Text(src); r != "" {
						references = append(references, r)
					}
				}
			}
		}
	}

	return dedupeStrings(functions), dedupeStrings(types), dedupeStrings(imports), dedupeStrings(references)
}

// dedupeStrings returns s with duplicates removed, preserving first-seen
// order. Returns nil for empty input so the caller's len() guards skip
// the attribute.
func dedupeStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(s))
	out := s[:0]
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
