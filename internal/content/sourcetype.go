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
	for scanner.Scan() {
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
	if bodyBuf != nil && bodyBuf.Len() > 0 {
		var funcs, types, imports []string
		switch s.language {
		case "go":
			funcs, types, imports = extractGoSymbols(bodyBuf.Bytes())
		case "python":
			funcs, types, imports = extractPythonSymbols(bodyBuf.Bytes())
		case "java":
			funcs, types, imports = extractJavaSymbols(bodyBuf.Bytes())
		case "csharp":
			funcs, types, imports = extractCSharpSymbols(bodyBuf.Bytes())
		case "php":
			funcs, types, imports = extractPHPSymbols(bodyBuf.Bytes())
		case "perl":
			funcs, types, imports = extractPerlSymbols(bodyBuf.Bytes())
		case "r":
			funcs, types, imports = extractRSymbols(bodyBuf.Bytes())
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
	}
	return attrs, nil
}

// symbolExtractorWired reports whether the per-language symbol
// extractor is registered. Used to avoid allocating a body-capture
// buffer for languages that won't use it.
func symbolExtractorWired(language string) bool {
	switch language {
	case "go", "python", "java", "csharp", "php", "perl", "r":
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
func isSourceTestFile(language, path string) bool {
	base := strings.ToLower(filepathBase(path))
	switch language {
	case "go":
		return strings.HasSuffix(base, "_test.go")
	case "python":
		return strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") ||
			strings.HasSuffix(base, "_test.py")
	case "javascript", "typescript":
		// Order matters — check longest suffixes first to avoid
		// claiming ".test.tsx" only by ".ts" stripping logic. Each
		// supported extension gets a `.test.<ext>` and `.spec.<ext>`
		// variant.
		for _, ext := range []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx"} {
			if strings.HasSuffix(base, ".test"+ext) ||
				strings.HasSuffix(base, ".spec"+ext) {
				return true
			}
		}
		return false
	case "rust":
		// Integration tests live under a `tests/` directory anywhere
		// in the path. We can't tell from the basename alone — match
		// either a leading "tests/" or a "/tests/" segment.
		lp := strings.ToLower(path)
		return strings.HasSuffix(base, ".rs") &&
			(strings.Contains(lp, "/tests/") || strings.HasPrefix(lp, "tests/"))
	case "c", "cpp":
		exts := []string{".c", ".h", ".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"}
		for _, ext := range exts {
			if before, ok := strings.CutSuffix(base, ext); ok {
				stem := before
				if strings.HasPrefix(stem, "test_") ||
					strings.HasSuffix(stem, "_test") ||
					strings.HasSuffix(stem, "_tests") {
					return true
				}
			}
		}
		return false
	case "java":
		return strings.HasSuffix(base, "test.java") ||
			strings.HasSuffix(base, "tests.java") ||
			strings.HasSuffix(base, "it.java")
	case "ruby":
		return strings.HasSuffix(base, "_spec.rb") ||
			strings.HasSuffix(base, "_test.rb")
	case "swift":
		return strings.HasSuffix(base, "tests.swift") ||
			strings.HasSuffix(base, "test.swift")
	case "kotlin":
		return strings.HasSuffix(base, "test.kt") ||
			strings.HasSuffix(base, "tests.kt")
	case "scala":
		return strings.HasSuffix(base, "spec.scala") ||
			strings.HasSuffix(base, "test.scala")
	case "shell":
		return strings.HasSuffix(base, "_test.sh") ||
			(strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".sh"))
	case "elixir":
		return strings.HasSuffix(base, "_test.exs")
	case "csharp":
		return strings.HasSuffix(base, "test.cs") ||
			strings.HasSuffix(base, "tests.cs")
	case "php":
		return strings.HasSuffix(base, "test.php")
	case "perl":
		// In Test::More / Test::Simple the .t extension IS the test
		// convention — there is no library / test naming distinction.
		return strings.HasSuffix(base, ".t")
	case "r":
		// testthat convention: test-foo.R or test_foo.R (both styles
		// in the wild). lowercase basename already applied above.
		return (strings.HasPrefix(base, "test-") || strings.HasPrefix(base, "test_")) &&
			(strings.HasSuffix(base, ".r"))
	case "vb":
		return strings.HasSuffix(base, "test.vb") ||
			strings.HasSuffix(base, "tests.vb")
	case "matlab":
		return strings.HasSuffix(base, "test.m") ||
			strings.HasSuffix(base, "tests.m")
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
