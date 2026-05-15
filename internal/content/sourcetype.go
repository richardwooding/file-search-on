package content

import (
	"bufio"
	"context"
	"io/fs"
	"strings"
)

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

	var total, loc, blank, comment int64
	inBlock := false
	for scanner.Scan() {
		total++
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
	return attrs, nil
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
}
