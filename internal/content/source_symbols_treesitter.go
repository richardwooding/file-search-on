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

// tsParseTimeoutMicros caps a single tree-sitter parse (issue #432). A
// pathological grammar parse (notably Swift) can run for minutes on a small
// file and isn't cancellable; this bounds it so the offending file is
// skipped (no symbols) rather than hanging the walk. 5 s is far above any
// healthy parse (milliseconds) yet well under a per-call timeout.
const tsParseTimeoutMicros = 5_000_000

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
	// ESM import + CommonJS require("x") — the latter covers the large
	// half of the JS/TS ecosystem that never adopted ES modules.
	"typescript": `(import_statement source: (string) @import)
((call_expression function: (identifier) @_f arguments: (arguments (string) @import)) (#eq? @_f "require"))`,
	"javascript": `(import_statement source: (string) @import)
((call_expression function: (identifier) @_f arguments: (arguments (string) @import)) (#eq? @_f "require"))`,
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
	"perl": `(ambiguous_function_call_expression (function) @reference)
(method_call_expression (method) @reference)`,
	"r":      `(call function: (identifier) @reference)`,
	"scala":  `(call_expression (identifier) @reference)`,
	"matlab":  `(function_call (identifier) @reference)`,
}

// tsTypeRefQuery captures type USAGES — a type named as a field type,
// parameter / return type, variable type, or generic argument — as
// @typeref (issue #398). These join the @reference set (call sites) so a
// type used only as a field type counts as referenced, fixing the
// type-level dead_code false-positive class and making who_calls on a type
// name find its users. Each query is scoped to usage-position parent nodes
// so it never captures the definition's own name (which would make every
// type self-referenced). Type usages never become call edges.
//
// Only languages with static type annotations appear here: JavaScript /
// Ruby / Perl / R / MATLAB have none, so they keep call-only references.
// Node names were read off each grammar's parse tree (see the s-expression
// probe in the test suite); a clause naming an unknown node type fails the
// whole query compile, so each clause is verified against the real grammar.
var tsTypeRefQuery = map[string]string{
	"rust": `(field_declaration (type_identifier) @typeref)
(parameter (type_identifier) @typeref)
(generic_type (type_identifier) @typeref)
(type_arguments (type_identifier) @typeref)
(function_item (type_identifier) @typeref)
(let_declaration (type_identifier) @typeref)
(reference_type (type_identifier) @typeref)`,
	"typescript": `(type_annotation (type_identifier) @typeref)
(type_arguments (type_identifier) @typeref)
(new_expression constructor: (identifier) @typeref)`,
	// JavaScript has no type annotations, but a class used only via
	// `new Foo()` would otherwise never appear as a reference and read as
	// dead. Capture the constructor identifier as a type usage (#444).
	"javascript": `(new_expression constructor: (identifier) @typeref)`,
	// Ruby is dynamically typed; "type usage" is a superclass in a class
	// definition (`class Widget < Base`) or a constant receiver
	// (`Helper.go`) — neither captured by the call/ref query, so a
	// referenced-only-as-a-base class read as dead before this (#444).
	"ruby": `(superclass (constant) @typeref)
(call receiver: (constant) @typeref)`,
	"python": `(type (identifier) @typeref)`,
	"java": `(field_declaration (type_identifier) @typeref)
(formal_parameter (type_identifier) @typeref)
(local_variable_declaration (type_identifier) @typeref)
(method_declaration (type_identifier) @typeref)
(type_arguments (type_identifier) @typeref)
(object_creation_expression (type_identifier) @typeref)`,
	// C# is identifier-based (no distinct type node), so only the
	// variable_declaration type position is unambiguous — params / returns
	// share the (identifier) shape with names and aren't safely separable.
	"csharp": `(variable_declaration (identifier) @typeref)`,
	"c": `(field_declaration (struct_specifier (type_identifier) @typeref))
(parameter_declaration (struct_specifier (type_identifier) @typeref))
(declaration (struct_specifier (type_identifier) @typeref))
(field_declaration (type_identifier) @typeref)
(parameter_declaration (type_identifier) @typeref)
(declaration (type_identifier) @typeref)`,
	"cpp": `(field_declaration (type_identifier) @typeref)
(parameter_declaration (type_identifier) @typeref)
(declaration (type_identifier) @typeref)
(function_definition (type_identifier) @typeref)
(template_argument_list (type_descriptor (type_identifier) @typeref))`,
	"kotlin": `(user_type (type_identifier) @typeref)`,
	"swift":  `(user_type (type_identifier) @typeref)`,
	"scala": `(class_parameter (type_identifier) @typeref)
(parameter (type_identifier) @typeref)
(function_definition (type_identifier) @typeref)
(val_definition (type_identifier) @typeref)`,
	"php": `(named_type (name) @typeref)`,
	// Perl / R / MATLAB intentionally have no typeref query: they have no
	// static type-annotation syntax, and their "types" (packages / S4
	// classes / classdefs) are referenced through the ordinary call path
	// already captured by tsRefQuery — so there's no type-only-usage gap
	// to close for dead-code accuracy.
}

// tsExportedQuery captures the names of PUBLIC function/type definitions as
// @exported — the keyword-visibility complement to Go/Python's name-based
// rule, feeding the builder-internal `exported_symbols` attribute that
// `unused_exports` consumes (issue #409 cross-language rollout). Only
// languages whose visibility is a keyword need an entry; name-convention
// languages (Go capitalised, Python `_`-prefixed) derive it without a query.
// Node shapes read off each grammar's parse tree (SExpr probe).
var tsExportedQuery = map[string]string{
	// Rust: a `pub` (any pub*) def carries a visibility_modifier child.
	"rust": `(function_item (visibility_modifier) (identifier) @exported)
(struct_item (visibility_modifier) (type_identifier) @exported)
(enum_item (visibility_modifier) (type_identifier) @exported)
(trait_item (visibility_modifier) (type_identifier) @exported)
(type_item (visibility_modifier) (type_identifier) @exported)`,
	// TypeScript: a public def is wrapped in an export_statement.
	"typescript": `(export_statement (function_declaration (identifier) @exported))
(export_statement (class_declaration (type_identifier) @exported))
(export_statement (interface_declaration (type_identifier) @exported))
(export_statement (type_alias_declaration (type_identifier) @exported))
(export_statement (abstract_class_declaration (type_identifier) @exported))`,
	// JavaScript: same wrapper; class name is a plain identifier (no types).
	"javascript": `(export_statement (function_declaration (identifier) @exported))
(export_statement (class_declaration (identifier) @exported))`,
	// Java: a `public` keyword inside the def's modifiers node (anonymous
	// token match is order-independent). Default (no modifier) is
	// package-private and correctly excluded.
	"java": `(class_declaration (modifiers "public") name: (identifier) @exported)
(interface_declaration (modifiers "public") name: (identifier) @exported)
(enum_declaration (modifiers "public") name: (identifier) @exported)
(record_declaration (modifiers "public") name: (identifier) @exported)
(method_declaration (modifiers "public") name: (identifier) @exported)`,
	// C#: a named (modifier) node equal to "public" (via #eq?). Top-level
	// default is internal, correctly excluded.
	"csharp": `(class_declaration (modifier) @_m name: (identifier) @exported (#eq? @_m "public"))
(interface_declaration (modifier) @_m name: (identifier) @exported (#eq? @_m "public"))
(struct_declaration (modifier) @_m name: (identifier) @exported (#eq? @_m "public"))
(enum_declaration (modifier) @_m name: (identifier) @exported (#eq? @_m "public"))
(record_declaration (modifier) @_m name: (identifier) @exported (#eq? @_m "public"))
(method_declaration (modifier) @_m name: (identifier) @exported (#eq? @_m "public"))`,
}

// tsNonExportedQuery is the inverse of tsExportedQuery for DEFAULT-PUBLIC
// languages (Kotlin / Scala): visibility is public unless a private /
// internal / protected modifier says otherwise, which tree-sitter can't
// express as a positive match. So these capture the NON-public defs (name +
// modifier text @vis); the exported set is then `functions + type_names`
// minus the captured names (see tsNonExportedSymbols + sourcetype). Kotlin
// allows an explicit `public` modifier, so @vis is text-filtered to exclude
// it; Scala has no public keyword (any access_modifier is non-public).
var tsNonExportedQuery = map[string]string{
	"kotlin": `(function_declaration (modifiers (visibility_modifier) @vis) (simple_identifier) @name)
(class_declaration (modifiers (visibility_modifier) @vis) (type_identifier) @name)
(object_declaration (modifiers (visibility_modifier) @vis) (type_identifier) @name)`,
	"scala": `(function_definition (modifiers (access_modifier) @vis) (identifier) @name)
(class_definition (modifiers (access_modifier) @vis) (identifier) @name)
(object_definition (modifiers (access_modifier) @vis) (identifier) @name)
(trait_definition (modifiers (access_modifier) @vis) (identifier) @name)`,
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
	// #365-migrated languages whose function names come from tsDefQuery
	// (name-only) and so carried no span — leaving cyclomatic complexity
	// uncomputed. Mirror each language's def node types as spans so
	// tsComplexityRows can attribute decision points to a function.
	// (C# is absent on purpose: its bundled tags query already emits
	// @definition.method/constructor spans, so adding it here double-counts.)
	"php": `(function_definition (name) @func.name) @func.def
(method_declaration (name) @func.name) @func.def`,
	"perl": `(subroutine_declaration_statement (bareword) @func.name) @func.def`,
	"r":    `(binary_operator (identifier) @func.name (function_definition)) @func.def`,
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
	"csharp": `[(if_statement) (for_statement) (foreach_statement) (while_statement) (do_statement) (switch_section) (catch_clause) (conditional_expression) (binary_expression "&&") (binary_expression "||")] @decision`,
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
	typeRefQuery  *ts.Query // @typeref type usages (#398); nil when none
	exportedQuery *ts.Query // @exported public defs (#409); nil when none
	nonExpQuery   *ts.Query // @name+@vis non-public defs (#409 negation); nil when none
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
	// Bound every parse with a hard time budget (issue #432). The
	// tree-sitter Swift grammar — and potentially any grammar — can parse
	// catastrophically slowly on certain real constructs (e.g. Alamofire's
	// AFError.swift: 3+ minutes for 37 KB, not size-related), and the parse
	// loop isn't cancellable, so one bad file would otherwise hang the whole
	// walk past any --timeout. On expiry Parse returns early; every consumer
	// already treats a nil/short tree as "no symbols", so the file degrades
	// gracefully (the #337 invariant) instead of stalling. Normal files
	// parse in milliseconds, so the cap only ever trips on pathological ones.
	tl := &tsLang{pool: ts.NewParserPool(lang, ts.WithParserPoolTimeoutMicros(tsParseTimeoutMicros))}
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
	if q := tsTypeRefQuery[language]; q != "" {
		if trQ, err := ts.NewQuery(q, lang); err == nil {
			tl.typeRefQuery = trQ
		}
	}
	if q := tsExportedQuery[language]; q != "" {
		if exQ, err := ts.NewQuery(q, lang); err == nil {
			tl.exportedQuery = exQ
		}
	}
	if q := tsNonExportedQuery[language]; q != "" {
		if neQ, err := ts.NewQuery(q, lang); err == nil {
			tl.nonExpQuery = neQ
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
	// Type usages (#398) join `references` — they make a type used only as a
	// field/param/return/generic type count as referenced — but never become
	// call edges (the call graph stays call-only).
	references = append(references, tsCollectTypeRefs(tl, tree, src)...)
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

// tsCollectTypeRefs gathers type-usage names (#398) via the per-language
// type-reference query. These join the flat references list but, unlike
// call sites, are not attributed to a caller (no call edges).
func tsCollectTypeRefs(tl *tsLang, tree *ts.Tree, src []byte) (refs []string) {
	if tl.typeRefQuery == nil {
		return nil
	}
	for _, m := range tl.typeRefQuery.Execute(tree) {
		for _, c := range m.Captures {
			if c.Name != "typeref" {
				continue
			}
			if r := c.Text(src); r != "" {
				refs = append(refs, r)
			}
		}
	}
	return refs
}

// tsHasExportedQuery reports whether language has a keyword-visibility
// export query — i.e. whether tsExportedSymbols can resolve its public
// symbols. Lets callers skip the extra parse for languages without one.
func tsHasExportedQuery(language string) bool {
	return tsExportedQuery[language] != ""
}

// tsExportedSymbols parses src and returns the names of public (exported)
// function/type definitions for keyword-visibility languages (Rust `pub`,
// TS/JS `export`). The builder-internal `exported_symbols` attribute that
// `unused_exports` consumes (#409). Returns nil for languages without an
// export query or when nothing parses. Parses independently of
// extractTreeSitterSymbols — call only for languages where tsHasExportedQuery
// is true, so non-keyword languages pay no extra parse.
func tsExportedSymbols(language string, src []byte) []string {
	tl := tsLangFor(language)
	if tl == nil || tl.exportedQuery == nil {
		return nil
	}
	tree, err := tl.pool.Parse(src)
	if err != nil || tree == nil {
		return nil
	}
	var out []string
	for _, m := range tl.exportedQuery.Execute(tree) {
		for _, c := range m.Captures {
			if c.Name == "exported" {
				if name := c.Text(src); name != "" {
					out = append(out, name)
				}
			}
		}
	}
	return dedupeStrings(out)
}

// tsHasNonExportedQuery reports whether language uses negation-style
// visibility (default-public; Kotlin / Scala) — i.e. its exported set is
// computed as defs minus the non-public names tsNonExportedSymbols returns.
func tsHasNonExportedQuery(language string) bool {
	return tsNonExportedQuery[language] != ""
}

// tsNonExportedSymbols parses src and returns the names of NON-public
// function/type definitions for default-public languages — those marked
// private / internal / protected. The caller subtracts these from the full
// def set to get the exported set (#409). Returns nil for languages without
// a non-export query or when nothing parses.
func tsNonExportedSymbols(language string, src []byte) []string {
	tl := tsLangFor(language)
	if tl == nil || tl.nonExpQuery == nil {
		return nil
	}
	tree, err := tl.pool.Parse(src)
	if err != nil || tree == nil {
		return nil
	}
	var out []string
	for _, m := range tl.nonExpQuery.Execute(tree) {
		var name, vis string
		for _, c := range m.Captures {
			switch c.Name {
			case "name":
				name = c.Text(src)
			case "vis":
				vis = c.Text(src)
			}
		}
		// An explicit `public` (Kotlin) is still exported — only
		// private / internal / protected count as non-public.
		if name != "" && !strings.HasPrefix(strings.TrimSpace(vis), "public") {
			out = append(out, name)
		}
	}
	return dedupeStrings(out)
}

// subtractStrings returns the members of all that are not in remove,
// preserving order. Used to derive the exported set (defs minus non-public)
// for default-public languages.
func subtractStrings(all, remove []string) []string {
	if len(remove) == 0 {
		return all
	}
	rm := make(map[string]struct{}, len(remove))
	for _, r := range remove {
		rm[r] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for _, a := range all {
		if _, ok := rm[a]; !ok {
			out = append(out, a)
		}
	}
	return out
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
