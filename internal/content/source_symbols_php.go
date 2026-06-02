package content

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

var (
	// Type declarations — class / interface / trait / enum (PHP 8.1+).
	// Optional modifiers: final / abstract / readonly (8.2+).
	// Inheritance / implements clauses follow the name and are not
	// captured (the type's own name is enough for `in` lookups).
	phpTypeRe = regexp.MustCompile(`^\s*(?:(?:final|abstract|readonly)\s+)*(?:class|interface|trait|enum)\s+([A-Za-z_]\w*)`)

	// use Foo\Bar;            (regular)
	// use Foo\Bar as B;       (aliased)
	// use function Foo\bar;   (function import)
	// use const Foo\BAR;      (constant import)
	// use Foo\{Bar, Baz};     (grouped) — only the prefix is captured
	//                                     for grouped imports; each
	//                                     leaf inside { } is omitted.
	//                                     Agents querying `"Foo" in imports`
	//                                     match the prefix.
	phpUseRe = regexp.MustCompile(`^\s*use\s+(?:function\s+|const\s+)?([\\\w]+)(?:\s+as\s+\w+)?\s*[;{]`)

	// function [&]name(...)  — top-level or inside class body.
	// Anonymous closures `function (...)` and arrow `fn (...)`
	// expressions don't match because there's no name token between
	// `function` and `(`. Modifiers are optional (interface methods
	// don't have access modifiers). The optional `&` prefix is
	// PHP's reference-return shape.
	phpFuncRe = regexp.MustCompile(`^\s*(?:(?:public|private|protected|static|final|abstract|readonly)\s+)*function\s+&?\s*([A-Za-z_]\w*)\s*\(`)
)

// extractPHPSymbols scans PHP source line-by-line. Captures:
//   - top-level + class-body `function name(...)` declarations.
//     Anonymous closures `function (...) { ... }` and arrow `fn`
//     expressions are skipped (no name).
//   - top-level + nested `class` / `interface` / `trait` / `enum`
//     declarations.
//   - `use Foo\\Bar;`, `use Foo\\Bar as B;`, `use function Foo\\bar;`,
//     `use const Foo\\BAR;`, and grouped `use Foo\\{Bar, Baz};`
//     statements. For grouped imports only the prefix is recorded
//     (each leaf inside the braces is omitted) — agents querying
//     `"Foo" in imports` still match the prefix.
//
// Limitations (documented in source-code.md):
//   - Magic methods (`__construct`, `__get`, etc.) ARE captured —
//     they're regular `function` declarations from the parser's POV
//     and agents asking "where is __construct?" want hits.
//   - `function` keyword inside a string literal is technically
//     matchable, but the leading-whitespace anchor + `function` token
//     position guard against the common cases.
//   - Multi-line function signatures (parameter list wrapping past
//     line end) match on the line containing `function name(` only.
func extractPHPSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if m := phpTypeRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := phpUseRe.FindStringSubmatch(line); m != nil {
			// Grouped imports like `use Foo\Bar\{Baz, Qux};` capture
			// the prefix including its trailing backslash. Trim it
			// so agents querying `"Foo\\Bar" in imports` match.
			imports = append(imports, strings.TrimRight(m[1], "\\"))
			continue
		}
		if m := phpFuncRe.FindStringSubmatch(line); m != nil {
			functions = append(functions, m[1])
		}
	}
	return
}
