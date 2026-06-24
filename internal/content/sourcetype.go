package content

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"strings"
)

// symbolCaptureCap bounds how much of a source file's body the
// symbol extractor sees. 1 MiB covers > 99% of real source files
// (median ~10 KB). Anything past the cap is silently truncated —
// extracted symbol lists may be incomplete on huge generated /
// vendored / amalgamation files. Documented in source-code.md.
const symbolCaptureCap = 1 << 20

// sourceType is one programming-language registration. The same Go
// type backs every language; the per-language behaviour lives in the
// config fields. blockOpen / blockClose may be empty for languages
// without block-comment syntax (e.g. Python, Shell, Clojure).
type sourceType struct {
	name        string   // canonical content-type name, e.g. "source/go"
	language    string   // canonical CEL `language` value, e.g. "go"
	exts        []string // extensions registered for this language
	lineComment string   // "" for languages with no line comment (OCaml)
	blockOpen   string   // "" for languages with no block comment
	blockClose  string   // ""
}

func (s *sourceType) Name() string         { return s.name }
func (s *sourceType) Extensions() []string { return s.exts }
func (s *sourceType) MagicBytes() [][]byte { return nil }

// Attributes scans the file line-by-line and classifies each line as
// blank, comment, or code based on this language's comment syntax.
// Returns line_count (total physical lines), loc (non-blank,
// non-comment), comment_loc, blank_loc, and language. Mixed lines
// (code with a trailing comment, or block-comment opening mid-line)
// are classified by what the line BEGINS with after stripping leading
// whitespace — matches the convention used by `cloc` and `tokei`.
func (s *sourceType) Attributes(ctx context.Context, fsys fs.FS, p string) (Attributes, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), MaxLineBytes())

	// Capture the raw body up to symbolCaptureCap so the per-language
	// symbol extractor can run in the same pass as the line counter.
	// Only allocate the buffer for languages whose extractor is wired
	// (Go / Python / Java) — saves bytes for every other source file.
	var bodyBuf *bytes.Buffer
	if symbolExtractorWired(s.language) {
		bodyBuf = bytes.NewBuffer(make([]byte, 0, 16*1024))
	}

	var total, loc, blank, comment int64
	inBlock := false
	// is_generated_code detection (#276): check first
	// generatedCodeLineBudget lines against the per-language marker
	// patterns. Once a hit lands, stop checking — markers don't
	// appear past the file header. Languages without a registered
	// convention leave generatedMarkers[s.language] empty and the
	// inner Contains loop short-circuits cheaply.
	var generatedDetected bool
	for scanner.Scan() {
		// Per-iteration cancellation guard — a pathological source
		// file (huge generated code, multi-MB single-line minified
		// blob) shouldn't stall the walker after the walk was
		// already cancelled. Mirrors the canonical text.go pattern.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		total++
		rawLine := scanner.Bytes()
		if bodyBuf != nil && bodyBuf.Len() < symbolCaptureCap {
			// Re-append the newline that bufio.Scanner stripped — the
			// extractors are line-based but need the line boundaries.
			remaining := symbolCaptureCap - bodyBuf.Len()
			if len(rawLine) >= remaining {
				bodyBuf.Write(rawLine[:remaining])
			} else {
				bodyBuf.Write(rawLine)
				bodyBuf.WriteByte('\n')
			}
		}
		line := strings.TrimSpace(scanner.Text())

		// Generator-marker probe — runs only for the first
		// generatedCodeLineBudget lines OR until a hit. Cheap when the
		// language has no markers (the inner lookup returns immediately).
		if !generatedDetected && total <= generatedCodeLineBudget && line != "" {
			if isGeneratedCodeLine(line, s.language) {
				generatedDetected = true
			}
		}

		if line == "" {
			blank++
			// A blank line inside a block comment is still a block
			// comment line by some tools' counting; we count it as
			// blank to match the dominant convention (cloc / tokei).
			continue
		}

		if inBlock {
			comment++
			if s.blockClose != "" && strings.Contains(line, s.blockClose) {
				inBlock = false
			}
			continue
		}

		if s.blockOpen != "" && strings.HasPrefix(line, s.blockOpen) {
			comment++
			// Single-line block comment: opens and closes on the same line.
			if s.blockClose != "" {
				rest := line[len(s.blockOpen):]
				if !strings.Contains(rest, s.blockClose) {
					inBlock = true
				}
			}
			continue
		}

		if s.lineComment != "" && strings.HasPrefix(line, s.lineComment) {
			comment++
			continue
		}

		loc++
	}

	attrs := Attributes{
		"language":    s.language,
		"line_count":  total,
		"loc":         loc,
		"comment_loc": comment,
		"blank_loc":   blank,
	}
	if isSourceTestFile(s.language, p) {
		attrs["is_test_file"] = true
	}
	if generatedDetected {
		attrs["is_generated_code"] = true
	}
	if bodyBuf != nil && bodyBuf.Len() > 0 {
		var funcs, types, imports, refs, callEdges, complexityRows []string
		switch s.language {
		case "go":
			// Go keeps the stdlib-AST extractor (rigorous, free).
			funcs, types, imports, refs, callEdges, complexityRows = extractGoSymbols(bodyBuf.Bytes())
		default:
			// Every other wired language (Python / Java / C# / PHP / Perl /
			// R / MATLAB / Scala + Rust / TS / JS / Ruby / Swift / Kotlin /
			// C / C++) uses the tree-sitter extractor (#365 migrated the
			// first eight off regex). Returns nil for unwired languages —
			// but symbolExtractorWired gates this block.
			funcs, types, imports, refs, callEdges, complexityRows = extractTreeSitterSymbols(s.language, bodyBuf.Bytes())
		}
		if len(funcs) > 0 {
			attrs["functions"] = funcs
		}
		if len(types) > 0 {
			attrs["type_names"] = types
		}
		if len(imports) > 0 {
			attrs["imports"] = imports
		}
		if len(refs) > 0 {
			attrs["references"] = refs
		}
		if len(callEdges) > 0 {
			// Builder-internal (call graph #368); not a CEL variable.
			attrs["call_edges"] = callEdges
		}
		if len(complexityRows) > 0 {
			// Builder-internal per-function rows (#364) for the complexity
			// tool; max_complexity is the file-level CEL attribute.
			attrs["complexity_rows"] = complexityRows
			attrs["max_complexity"] = maxComplexityOf(complexityRows)
		}
		// method_owners (builder-internal, #445): "method\x00owner" pairs so
		// the code graph can disambiguate same-named methods on different
		// types (find_definition / who_calls / dead_code report the owning
		// type). Go via stdlib AST; the class-based tree-sitter languages
		// via tsMethodOwners (parent-walk to the enclosing type).
		var owners []string
		if s.language == "go" {
			owners = goMethodOwners(bodyBuf.Bytes())
		} else {
			owners = tsMethodOwners(s.language, bodyBuf.Bytes())
		}
		if len(owners) > 0 {
			attrs["method_owners"] = owners
		}
		// package (builder-internal, #467): the file's declared package /
		// namespace — the node unit for package-level coupling (Java today).
		// Empty for Go (package implied by directory) and languages with no
		// package concept; computed only when a package query exists, so
		// non-package languages pay no parse.
		if pkg := declaredPackage(s.language, bodyBuf.Bytes()); pkg != "" {
			attrs["package"] = pkg
		}
		// relative_imports (builder-internal, #467): relative imports with
		// their leading dots preserved, kept separate from `imports` so that
		// attribute stays free of dotted-relative strings. Python today; the
		// coupling adapter resolves them against the file's own package.
		if rel := relativeImports(s.language, bodyBuf.Bytes()); len(rel) > 0 {
			attrs["relative_imports"] = rel
		}
		// exported_symbols (builder-internal, #409): the public subset of
		// defs for keyword-visibility languages, consumed by unused_exports.
		// Positive-keyword languages (Rust pub, TS/JS export, Java/C# public)
		// capture the public defs directly; default-public languages
		// (Kotlin / Scala) capture the NON-public defs and subtract them
		// from funcs+types. Go/Python derive visibility by name in the
		// consumer, so they need no query and no extra parse here.
		switch {
		case tsHasExportedQuery(s.language):
			if exp := tsExportedSymbols(s.language, bodyBuf.Bytes()); len(exp) > 0 {
				attrs["exported_symbols"] = exp
			}
		case tsHasNonExportedQuery(s.language):
			all := make([]string, 0, len(funcs)+len(types))
			all = append(all, funcs...)
			all = append(all, types...)
			if exp := subtractStrings(all, tsNonExportedSymbols(s.language, bodyBuf.Bytes())); len(exp) > 0 {
				attrs["exported_symbols"] = exp
			}
		}
	}
	return attrs, nil
}

// symbolExtractorWired reports whether the per-language symbol
// extractor is registered. Used to avoid allocating a body-capture
// buffer for languages that won't use it.
func symbolExtractorWired(language string) bool {
	switch language {
	// Go uses the stdlib-AST extractor; the rest use tree-sitter
	// (extractTreeSitterSymbols via the switch default). #365 migrated
	// python/java/csharp/php/perl/r/matlab/scala off regex onto tree-sitter.
	case "go",
		"python", "java", "csharp", "php", "perl", "r", "matlab", "scala",
		"rust", "typescript", "javascript", "ruby", "swift", "kotlin", "c", "cpp":
		return true
	}
	return false
}

// isSourceTestFile reports whether a source file's basename matches
// the per-language test convention. Returns false for languages
// without a strong filename convention (Rust uses inline #[cfg(test)]
// modules — basename matching only catches the integration-test files
// under `tests/`).
//
// Conventions covered:
//
//	Go         *_test.go
//	Python     test_*.py, *_test.py
//	JS/TS      *.test.{js,mjs,cjs,jsx,ts,tsx}, *.spec.{...}
//	Rust       integration tests under a `tests/` directory
//	C / C++    test_*.{c,cpp,...}, *_test.{c,cpp,...}, *_tests.{c,cpp,...}
//	Java       *Test.java, *Tests.java, *IT.java (Maven failsafe IT)
//	Ruby       *_spec.rb, *_test.rb
//	Swift      *Tests.swift, *Test.swift
//	Kotlin     *Test.kt, *Tests.kt
//	Scala      *Spec.scala, *Test.scala
//	Shell      *_test.sh, test_*.sh
//	Elixir     *_test.exs
//
// testConvention is a per-language test-filename rule: a basename matches
// if it ends with any suffix, OR (when prefixExt is set) it starts with a
// prefix AND ends with prefixExt. All values are lowercase.
type testConvention struct {
	suffixes  []string
	prefixes  []string
	prefixExt string
}

// jsTestSuffixes is the `.test.<ext>` / `.spec.<ext>` set shared by JS/TS.
var jsTestSuffixes = func() []string {
	var s []string
	for _, ext := range []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx"} {
		s = append(s, ".test"+ext, ".spec"+ext)
	}
	return s
}()

// testFileConventions drives isSourceTestFile for the suffix/prefix-shaped
// languages. Rust (tests/ directory) and C/C++ (stem-based) are handled
// specially in isSourceTestFile. See the doc comment for the source of
// each convention.
var testFileConventions = map[string]testConvention{
	"go":         {suffixes: []string{"_test.go"}},
	"python":     {suffixes: []string{"_test.py"}, prefixes: []string{"test_"}, prefixExt: ".py"},
	"javascript": {suffixes: jsTestSuffixes},
	"typescript": {suffixes: jsTestSuffixes},
	"java":       {suffixes: []string{"test.java", "tests.java", "it.java"}},
	"ruby":       {suffixes: []string{"_spec.rb", "_test.rb"}},
	"swift":      {suffixes: []string{"tests.swift", "test.swift"}},
	"kotlin":     {suffixes: []string{"test.kt", "tests.kt"}},
	"scala":      {suffixes: []string{"spec.scala", "test.scala"}},
	"shell":      {suffixes: []string{"_test.sh"}, prefixes: []string{"test_"}, prefixExt: ".sh"},
	"elixir":     {suffixes: []string{"_test.exs"}},
	"csharp":     {suffixes: []string{"test.cs", "tests.cs"}},
	"php":        {suffixes: []string{"test.php"}},
	"perl":       {suffixes: []string{".t"}}, // Test::More: the .t extension IS the convention
	"r":          {prefixes: []string{"test-", "test_"}, prefixExt: ".r"},
	"vb":         {suffixes: []string{"test.vb", "tests.vb"}},
	"matlab":     {suffixes: []string{"test.m", "tests.m"}},
}

// cTestExts are the C/C++ source extensions whose stem is checked for a
// test prefix/suffix.
var cTestExts = []string{".c", ".h", ".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"}

func isSourceTestFile(language, path string) bool {
	base := strings.ToLower(filepathBase(path))
	switch language {
	case "rust":
		// Integration tests live under a `tests/` directory anywhere in
		// the path — not detectable from the basename alone.
		lp := strings.ToLower(path)
		return strings.HasSuffix(base, ".rs") &&
			(strings.Contains(lp, "/tests/") || strings.HasPrefix(lp, "tests/"))
	case "c", "cpp":
		// Stem-based: test_foo.c / foo_test.c / foo_tests.cpp.
		for _, ext := range cTestExts {
			if stem, ok := strings.CutSuffix(base, ext); ok {
				if strings.HasPrefix(stem, "test_") ||
					strings.HasSuffix(stem, "_test") || strings.HasSuffix(stem, "_tests") {
					return true
				}
			}
		}
		return false
	}
	c, ok := testFileConventions[language]
	if !ok {
		return false
	}
	for _, s := range c.suffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	if c.prefixExt != "" && strings.HasSuffix(base, c.prefixExt) {
		for _, p := range c.prefixes {
			if strings.HasPrefix(base, p) {
				return true
			}
		}
	}
	return false
}

// filepathBase returns the last path component without importing
// filepath into this hot-path file. fs.FS keys always use forward
// slashes, so a simple LastIndexByte works on every OS.
func filepathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// register helper threads the boilerplate.
func registerSource(name, language string, exts []string, lineComment, blockOpen, blockClose string) {
	Register(&sourceType{
		name:        name,
		language:    language,
		exts:        exts,
		lineComment: lineComment,
		blockOpen:   blockOpen,
		blockClose:  blockClose,
	})
}

func init() {
	// C-family: // line comments + /* */ block comments.
	registerSource("source/go", "go", []string{".go"}, "//", "/*", "*/")
	registerSource("source/javascript", "javascript", []string{".js", ".mjs", ".cjs", ".jsx"}, "//", "/*", "*/")
	registerSource("source/typescript", "typescript", []string{".ts", ".tsx"}, "//", "/*", "*/")
	registerSource("source/rust", "rust", []string{".rs"}, "//", "/*", "*/")
	registerSource("source/c", "c", []string{".c", ".h"}, "//", "/*", "*/")
	registerSource("source/cpp", "cpp", []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"}, "//", "/*", "*/")
	registerSource("source/java", "java", []string{".java"}, "//", "/*", "*/")
	registerSource("source/swift", "swift", []string{".swift"}, "//", "/*", "*/")
	registerSource("source/kotlin", "kotlin", []string{".kt", ".kts"}, "//", "/*", "*/")
	registerSource("source/scala", "scala", []string{".scala", ".sc"}, "//", "/*", "*/")
	registerSource("source/zig", "zig", []string{".zig"}, "//", "", "")

	// Hash-comment family (no block syntax in the simple form we model).
	registerSource("source/python", "python", []string{".py", ".pyi"}, "#", "", "")
	registerSource("source/shell", "shell", []string{".sh", ".bash", ".zsh"}, "#", "", "")
	registerSource("source/elixir", "elixir", []string{".ex", ".exs"}, "#", "", "")
	registerSource("source/ruby", "ruby", []string{".rb"}, "#", "=begin", "=end")

	// Less-common syntaxes.
	registerSource("source/lua", "lua", []string{".lua"}, "--", "--[[", "]]")
	registerSource("source/haskell", "haskell", []string{".hs"}, "--", "{-", "-}")
	registerSource("source/ocaml", "ocaml", []string{".ml", ".mli"}, "", "(*", "*)")
	registerSource("source/clojure", "clojure", []string{".clj", ".cljs", ".cljc", ".edn"}, ";", "", "")

	// Tiobe top 20 (May 2026) — additions completing the coverage of
	// the canonical "most-used languages" list. Scratch (#12) is a
	// block-visual environment with binary .sb3 files and doesn't fit
	// the source/* shape; it's tracked as a separate content-type
	// follow-up. Symbol extraction (functions / type_names / imports)
	// stays in scope only for Go / Python / Java; per-language symbol
	// extractors for these new langs are clean follow-up PRs.
	registerSource("source/csharp", "csharp", []string{".cs"}, "//", "/*", "*/")
	registerSource("source/php", "php", []string{".php", ".phtml", ".php3", ".php4", ".php5", ".php7", ".phps"}, "//", "/*", "*/")
	registerSource("source/perl", "perl", []string{".pl", ".pm", ".t"}, "#", "", "")
	registerSource("source/r", "r", []string{".r", ".R"}, "#", "", "")
	registerSource("source/ada", "ada", []string{".adb", ".ads"}, "--", "", "")
	registerSource("source/sql", "sql", []string{".sql"}, "--", "/*", "*/")
	registerSource("source/vb", "vb", []string{".vb", ".bas", ".vbs", ".cls", ".frm"}, "'", "", "")
	registerSource("source/fortran", "fortran", []string{".f", ".f90", ".f95", ".f03", ".f08", ".for", ".ftn", ".fpp"}, "!", "", "")
	// MATLAB claims .m. Objective-C also uses .m but isn't a
	// registered content type today; if it ever is, the two will need
	// a disambiguator (e.g. first-line "function" / "classdef" sniff).
	registerSource("source/matlab", "matlab", []string{".m"}, "%", "%{", "%}")
	// Assembly: many dialects. We pick the common-denominator ";" line
	// comment (NASM / MASM). GAS files using "#" or "/* */" comments
	// fall through and get classified as code — documented limitation.
	registerSource("source/assembly", "assembly", []string{".asm", ".s", ".S", ".nasm"}, ";", "", "")
	// Pascal / Delphi: two block-comment dialects exist ({ ... } and
	// (* ... *)). The state machine supports one pair; we pick the
	// modern Delphi default { ... }. (* ... *) lines fall through as
	// code — documented limitation, future refactor.
	registerSource("source/pascal", "pascal", []string{".pas", ".pp", ".dpr", ".lpr"}, "//", "{", "}")
}
