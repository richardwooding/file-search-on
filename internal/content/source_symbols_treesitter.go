package content

import (
	"fmt"
	"strconv"
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
	// Migrated from regex extractors (#365).
	"python": "x.py",
	"java":   "x.java",
	"csharp": "x.cs",
	"php":    "x.php",
	"perl":   "x.pl",
	"r":      "x.r",
	"matlab": "x.m",
	"scala":  "x.scala",
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
	// #365 migrations. Bundled tags cover class+function/method for
	// java / csharp / scala / matlab / python; these supplements add the
	// type kinds tags miss. php / perl / r have empty or call-only tags,
	// so their full def set lives here.
	"java": `(interface_declaration (identifier) @type)
(enum_declaration (identifier) @type)
(record_declaration (identifier) @type)`,
	"csharp": `(struct_declaration (identifier) @type)
(interface_declaration (identifier) @type)
(enum_declaration (identifier) @type)
(record_declaration (identifier) @type)`,
	"scala": `(object_definition (identifier) @type)
(trait_definition (identifier) @type)
(enum_definition (identifier) @type)`,
	"matlab": `(class_definition (identifier) @type)`,
	"php": `(class_declaration (name) @type)
(interface_declaration (name) @type)
(trait_declaration (name) @type)
(enum_declaration (name) @type)
(function_definition (name) @function)
(method_declaration (name) @function)`,
	"perl": `(subroutine_declaration_statement (bareword) @function)
(package_statement (package) @type)`,
	"r": `(binary_operator (identifier) @function (function_definition))`,
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
	// #365 migrations.
	"python": `(import_statement (dotted_name) @import)
(import_from_statement module_name: (dotted_name) @import)`,
	"java":   `(import_declaration (scoped_identifier) @import)`,
	"csharp": `(using_directive (qualified_name) @import)
(using_directive (identifier) @import)`,
	"php":    `(namespace_use_clause (qualified_name) @import)`,
	"perl":   `(use_statement (package) @import)`,
	"r":      `((call function: (identifier) @_f arguments: (arguments (argument (identifier) @import))) (#match? @_f "^(library|require|requireNamespace)$"))`,
	"scala":  `(import_declaration) @import`,
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
	// #365 migrations — call-site callees.
	"python": `(call function: (identifier) @reference)
(call function: (attribute attribute: (identifier) @reference))`,
	"java":   `(method_invocation name: (identifier) @reference)`,
	"csharp": `(invocation_expression function: (identifier) @reference)
(invocation_expression function: (member_access_expression name: (identifier) @reference))`,
	"php":    `(function_call_expression (name) @reference)
(member_call_expression name: (name) @reference)
(scoped_call_expression name: (name) @reference)`,
	"perl":   `(ambiguous_function_call_expression (function) @reference)`,
	"r":      `(call function: (identifier) @reference)`,
	"scala":  `(call_expression (identifier) @reference)`,
	"matlab":  `(function_call (identifier) @reference)`,
}

// tsFuncSpanQuery captures a function definition's full node as @func.def
// plus its name as @func.name, for languages whose bundled tags query
// doesn't expose a function span (ruby/swift/kotlin — their tags emit
// only definition.class). Used for per-function call attribution (#368)
// via span-containment. The bundled-tags languages get spans from the
// tags query's @definition.function capture instead.
var tsFuncSpanQuery = map[string]string{
	"ruby": `(method name: (identifier) @func.name) @func.def
(singleton_method name: (identifier) @func.name) @func.def`,
	"swift":  `(function_declaration (simple_identifier) @func.name) @func.def`,
	"kotlin": `(function_declaration (simple_identifier) @func.name) @func.def`,
}

// tsDecisionQuery captures cyclomatic-complexity decision points as
// @decision (issue #364): branch/loop/case nodes + short-circuit
// operators. Counted per enclosing function span; complexity = 1 + count.
// Node names vary per grammar; iterated via tests.
var tsDecisionQuery = map[string]string{
	"rust": `[(if_expression) (while_expression) (for_expression) (loop_expression) (match_arm) (binary_expression "&&") (binary_expression "||")] @decision`,
	"typescript": `[(if_statement) (while_statement) (for_statement) (for_in_statement) (do_statement) (switch_case) (catch_clause) (ternary_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"javascript": `[(if_statement) (while_statement) (for_statement) (for_in_statement) (do_statement) (switch_case) (catch_clause) (ternary_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"ruby":   `[(if) (elsif) (unless) (while) (until) (for) (when) (rescue) (conditional) (binary "&&") (binary "||")] @decision`,
	"swift":  `[(if_statement) (guard_statement) (while_statement) (for_statement) (switch_entry) (catch_block) (ternary_expression) (conjunction_expression) (disjunction_expression)] @decision`,
	"kotlin": `[(if_expression) (while_statement) (do_while_statement) (for_statement) (when_entry) (catch_block) (conjunction_expression) (disjunction_expression)] @decision`,
	"c":      `[(if_statement) (while_statement) (for_statement) (do_statement) (case_statement) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"cpp":    `[(if_statement) (while_statement) (for_statement) (do_statement) (case_statement) (catch_clause) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	// #365 migrations.
	"python": `[(if_statement) (elif_clause) (for_statement) (while_statement) (except_clause) (conditional_expression) (boolean_operator)] @decision`,
	"java":   `[(if_statement) (for_statement) (enhanced_for_statement) (while_statement) (do_statement) (switch_label) (catch_clause) (ternary_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"csharp": `[(if_statement) (for_statement) (for_each_statement) (while_statement) (do_statement) (switch_section) (catch_clause) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"php":    `[(if_statement) (else_if_clause) (for_statement) (foreach_statement) (while_statement) (do_statement) (case_statement) (catch_clause) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
	"perl":   `[(conditional_statement) (postfix_conditional_expression)] @decision`,
	"r":      `[(if_statement) (for_statement) (while_statement)] @decision`,
	"matlab": `[(if_statement) (elseif_clause) (for_statement) (while_statement) (case_clause) (catch_clause)] @decision`,
	"scala":  `[(if_expression) (for_expression) (while_expression) (case_clause) (catch_clause)] @decision`,
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
	refQuery      *ts.Query // @reference call-site callees; nil when none
	spanQuery     *ts.Query // @func.def + @func.name spans; nil when none
	decisionQuery *ts.Query // @decision complexity points; nil when none
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
	tl := &tsLang{pool: ts.NewParserPool(lang)}
	// Bundled tags query is optional — some grammars (PHP, Perl) ship an
	// empty one, in which case definitions come entirely from tsDefQuery.
	if tagsSrc := grammars.ResolveTagsQuery(*entry); tagsSrc != "" {
		if tagsQ, err := ts.NewQuery(tagsSrc, lang); err == nil {
			tl.tagsQuery = tagsQ
		}
	}
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
	if q := tsFuncSpanQuery[language]; q != "" {
		if spanQ, err := ts.NewQuery(q, lang); err == nil {
			tl.spanQuery = spanQ
		}
	}
	if q := tsDecisionQuery[language]; q != "" {
		if decQ, err := ts.NewQuery(q, lang); err == nil {
			tl.decisionQuery = decQ
		}
	}
	return tl
}

// extractTreeSitterSymbols parses src with the language's grammar and
// returns the function / type / import names. Matches the signature of
// the hand-rolled extractXxxSymbols functions. Returns all-nil when the
// language isn't tree-sitter-backed.
func extractTreeSitterSymbols(language string, src []byte) (functions, types, imports, references, callEdges, complexityRows []string) {
	tl := tsLangFor(language)
	if tl == nil {
		return nil, nil, nil, nil, nil, nil
	}
	tree, err := tl.pool.Parse(src)
	if err != nil || tree == nil {
		return nil, nil, nil, nil, nil, nil
	}

	// funcSpans (named function definitions + byte/line span) are shared
	// by call attribution (#368) and complexity (#364).
	functions, types, funcSpans := tsCollectDefs(tl, tree, src)
	imports = tsCollectImports(tl, tree, src)
	references, callEdges = tsCollectReferences(tl, tree, src, funcSpans)
	complexityRows = tsComplexityRows(tl, tree, funcSpans)

	return dedupeStrings(functions), dedupeStrings(types), dedupeStrings(imports),
		dedupeStrings(references), dedupeStrings(callEdges), complexityRows
}

// tsFunctionSpans returns the 1-based inclusive line span of every named
// function / method definition the grammar surfaces (issue #366). Reuses the
// same tsCollectDefs path as symbol extraction; nil when the language isn't
// tree-sitter-backed or nothing parses.
func tsFunctionSpans(language string, src []byte) []FunctionSpan {
	tl := tsLangFor(language)
	if tl == nil {
		return nil
	}
	tree, err := tl.pool.Parse(src)
	if err != nil || tree == nil {
		return nil
	}
	_, _, funcSpans := tsCollectDefs(tl, tree, src)
	if len(funcSpans) == 0 {
		return nil
	}
	out := make([]FunctionSpan, 0, len(funcSpans))
	for _, s := range funcSpans {
		out = append(out, FunctionSpan{
			Name:      s.name,
			StartLine: int(s.startLine),
			EndLine:   int(s.endLine),
		})
	}
	return out
}

// newFuncSpan builds a tsFuncSpan from a definition node (byte + 1-based
// line span).
func newFuncSpan(name string, n *ts.Node) tsFuncSpan {
	return tsFuncSpan{
		name: name, start: n.StartByte(), end: n.EndByte(),
		startLine: n.StartPoint().Row + 1, endLine: n.EndPoint().Row + 1,
	}
}

// tsCollectDefs gathers function / type names from the bundled tags query
// plus the supplemental def + span queries, and the function spans.
func tsCollectDefs(tl *tsLang, tree *ts.Tree, src []byte) (functions, types []string, funcSpans []tsFuncSpan) {
	for _, m := range tagsMatches(tl, tree) {
		var name, kind string
		var defNode *ts.Node
		for _, c := range m.Captures {
			switch {
			case c.Name == "name":
				name = c.Text(src)
			case strings.HasPrefix(c.Name, "definition."):
				kind = c.Name[len("definition."):]
				defNode = c.Node
			}
		}
		if name == "" {
			continue
		}
		switch kind {
		case "function", "method", "macro", "constructor":
			functions = append(functions, name)
			if defNode != nil {
				funcSpans = append(funcSpans, newFuncSpan(name, defNode))
			}
		case "class", "struct", "interface", "enum", "trait", "type", "module", "union", "protocol", "namespace":
			types = append(types, name)
		}
	}

	// Supplemental def query for languages with weak/empty bundled tags
	// (php / perl / ruby / swift / kotlin): captures @function / @type.
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

	// Supplemental function-span query for grammars whose tags query
	// carries no function span (ruby / swift / kotlin).
	if tl.spanQuery != nil {
		for _, m := range tl.spanQuery.Execute(tree) {
			var name string
			var defNode *ts.Node
			for _, c := range m.Captures {
				switch c.Name {
				case "func.name":
					name = c.Text(src)
				case "func.def":
					defNode = c.Node
				}
			}
			if name != "" && defNode != nil {
				funcSpans = append(funcSpans, newFuncSpan(name, defNode))
			}
		}
	}
	return functions, types, funcSpans
}

// tsCollectImports gathers import paths via the per-language import query.
func tsCollectImports(tl *tsLang, tree *ts.Tree, src []byte) (imports []string) {
	if tl.importQuery == nil {
		return nil
	}
	for _, m := range tl.importQuery.Execute(tree) {
		for _, c := range m.Captures {
			if c.Name != "import" {
				continue
			}
			p := strings.Trim(c.Text(src), "\"'`<>")
			// Some grammars (Scala) have no single import-path node, so we
			// capture the whole declaration; strip the keyword.
			if p = strings.TrimSpace(strings.TrimPrefix(p, "import ")); p != "" {
				imports = append(imports, p)
			}
		}
	}
	return imports
}

// tsCollectReferences gathers call-site callee names (the flat references
// list) and attributes each to the innermost enclosing function span as a
// "caller\x00callee" edge.
func tsCollectReferences(tl *tsLang, tree *ts.Tree, src []byte, funcSpans []tsFuncSpan) (references, callEdges []string) {
	if tl.refQuery == nil {
		return nil, nil
	}
	for _, m := range tl.refQuery.Execute(tree) {
		for _, c := range m.Captures {
			if c.Name != "reference" {
				continue
			}
			r := c.Text(src)
			if r == "" {
				continue
			}
			references = append(references, r)
			if caller := innermostFuncSpan(funcSpans, c.Node.StartByte()); caller != "" {
				callEdges = append(callEdges, caller+"\x00"+r)
			}
		}
	}
	return references, callEdges
}

// tsComplexityRows computes per-function cyclomatic complexity (1 +
// decision points contained in the innermost enclosing span) as
// "name\x00complexity\x00startLine\x00endLine" rows (#364).
func tsComplexityRows(tl *tsLang, tree *ts.Tree, funcSpans []tsFuncSpan) (rows []string) {
	if tl.decisionQuery == nil || len(funcSpans) == 0 {
		return nil
	}
	decisionCount := make([]int, len(funcSpans))
	for _, m := range tl.decisionQuery.Execute(tree) {
		for _, c := range m.Captures {
			if c.Name != "decision" {
				continue
			}
			if i := innermostFuncSpanIndex(funcSpans, c.Node.StartByte()); i >= 0 {
				decisionCount[i]++
			}
		}
	}
	for i, s := range funcSpans {
		rows = append(rows, fmt.Sprintf("%s\x00%d\x00%d\x00%d", s.name, 1+decisionCount[i], s.startLine, s.endLine))
	}
	return rows
}

// tagsMatches runs the bundled tags query, or returns nil when the
// grammar ships no tags query (PHP / Perl) — defs then come from defQuery.
func tagsMatches(tl *tsLang, tree *ts.Tree) []ts.QueryMatch {
	if tl.tagsQuery == nil {
		return nil
	}
	return tl.tagsQuery.Execute(tree)
}

// maxComplexityOf returns the highest complexity across the per-function
// rows ("func\x00complexity\x00startLine\x00endLine"), as the int64 the
// CEL `max_complexity` attribute carries. 0 when no rows.
func maxComplexityOf(rows []string) int64 {
	var max int64
	for _, r := range rows {
		parts := strings.SplitN(r, "\x00", 4)
		if len(parts) < 2 {
			continue
		}
		if cx, err := strconv.ParseInt(parts[1], 10, 64); err == nil && cx > max {
			max = cx
		}
	}
	return max
}

// tsFuncSpan is a named function definition's byte span + 1-based line span.
type tsFuncSpan struct {
	name               string
	start, end         uint32
	startLine, endLine uint32
}

// innermostFuncSpan returns the name of the smallest function span that
// contains pos, or "" if none does (call outside any captured function).
func innermostFuncSpan(spans []tsFuncSpan, pos uint32) string {
	if i := innermostFuncSpanIndex(spans, pos); i >= 0 {
		return spans[i].name
	}
	return ""
}

// innermostFuncSpanIndex returns the index of the smallest function span
// containing pos, or -1 if none does.
func innermostFuncSpanIndex(spans []tsFuncSpan, pos uint32) int {
	best := -1
	bestSize := ^uint32(0)
	for i, s := range spans {
		if pos < s.start || pos >= s.end {
			continue
		}
		if size := s.end - s.start; size < bestSize {
			bestSize = size
			best = i
		}
	}
	return best
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
