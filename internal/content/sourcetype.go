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

	return Attributes{
		"language":    s.language,
		"line_count":  total,
		"loc":         loc,
		"comment_loc": comment,
		"blank_loc":   blank,
	}, nil
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
