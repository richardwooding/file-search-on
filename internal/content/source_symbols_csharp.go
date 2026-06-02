package content

import (
	"bufio"
	"bytes"
	"regexp"
)

var (
	// Type declarations — class / struct / interface / record /
	// record class / record struct / enum. The optional
	// "record class" / "record struct" longer forms are matched by
	// the `(?:\s+(?:class|struct))?` group attached to "record".
	// Delegates handled separately (csharpDelegateRe) because the
	// name appears AFTER the return type, not directly after the
	// `delegate` keyword.
	csharpTypeRe = regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|abstract|sealed|partial|static|readonly|ref|unsafe|new|file)\s+)*(?:class|struct|interface|record(?:\s+(?:class|struct))?|enum)\s+([A-Za-z_]\w*)`)

	// `public delegate void Handler(int x);` — the delegate name
	// comes AFTER the return type (here `void`). The return-type
	// portion is lazy-matched so we don't accidentally consume the
	// delegate name.
	csharpDelegateRe = regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|new|unsafe)\s+)*delegate\s+[\w<>\[\],.\s?]+?\s+([A-Za-z_]\w*)\s*[(<]`)

	// using Foo.Bar;
	// using static Foo.Bar.Baz;
	// using Alias = Foo.Bar;
	// using global::Foo.Bar;  (rare; the global:: prefix is dropped)
	// We capture the right-hand identifier (the namespace / type
	// being imported). For aliased imports we capture the alias's
	// RHS — that's what `imports` is documented to record.
	csharpUsingRe = regexp.MustCompile(`^\s*using\s+(?:static\s+)?(?:\w+\s*=\s*)?(?:global::)?([\w.]+)\s*;`)

	// Methods inside class / struct / interface bodies. Modifiers
	// are OPTIONAL (`*` not `+`) so interface methods (which have
	// no explicit access modifier in C#) are captured. Generic
	// method names (`Convert<T, U>`) are supported via the optional
	// `(?:<[^>]+>)?` group between the method name and the `(`.
	// Return type is lazy-matched as `[\w<>\[\],.\s?]+?`. Trailing
	// `where T : constraint` generic constraints are tolerated.
	//
	// Tolerated bodies: `{` (normal method), `;` (abstract /
	// interface method / extern declaration), `=` (expression-
	// bodied member `=>` shorthand — the `=` from `=>` matches).
	//
	// Without the modifier requirement, the keyword guard
	// (isCSharpKeyword) is the primary defence against false
	// positives like `if (cond)` / `for (...)`.
	csharpMethodRe = regexp.MustCompile(`^[ \t]+(?:(?:public|private|protected|internal|static|virtual|override|abstract|sealed|async|extern|partial|new|readonly|unsafe|file|ref|in|out)\s+)*[\w<>\[\],.\s?]+?\s+([A-Za-z_]\w*)\s*(?:<[^>]+>)?\s*\([^)]*\)\s*(?:where\s+[\w:,\s.<>()]+?)?\s*[\{;=]`)
)

// extractCSharpSymbols scans C# source line-by-line. Captures:
//   - top-level + nested class / struct / interface / enum / record
//     / record struct / record class / delegate / @interface (any
//     declaration form recognised by the C# spec) — emitted as
//     type_names.
//   - top-level + nested method declarations (regular, abstract,
//     async, extern, expression-bodied) — emitted as functions.
//   - using directives (regular, static, aliased) — emitted as imports.
//
// Limitations (documented in source-code.md): Java-shape regex over
// C#. Top-level statements (file-scoped methods without a class
// wrapper) require leading whitespace to be matched; bare top-level
// methods written flush-left are NOT captured. Multi-line method
// signatures with parameters wrapping past line end aren't matched.
// Comment-stripping isn't performed, so a `// using …` comment line
// could in principle match if the // is stripped before the regex
// runs — but the regex anchors on `^\s*using` so commented lines
// (which start `//` not `using`) are safe.
//
// File-scoped namespaces (`namespace Foo;` on a single line) don't
// emit anything — the namespace is metadata, not a symbol. Block-
// form namespaces (`namespace Foo { … }`) are similarly ignored.
func extractCSharpSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if m := csharpTypeRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := csharpDelegateRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := csharpUsingRe.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
			continue
		}
		if m := csharpMethodRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			if !isCSharpKeyword(name) {
				functions = append(functions, name)
			}
		}
	}
	return
}

// isCSharpKeyword guards against keyword false-positives in the
// method-extractor regex. Control-flow keywords that take parens (`if
// (cond)`, `for (...)`, etc.) can otherwise look method-shaped.
func isCSharpKeyword(s string) bool {
	switch s {
	case "if", "for", "foreach", "while", "do", "switch", "case",
		"return", "throw", "try", "catch", "finally",
		"using", "lock", "checked", "unchecked", "fixed", "stackalloc",
		"new", "this", "base", "true", "false", "null",
		"is", "as", "typeof", "sizeof", "nameof", "default",
		"await", "yield", "when", "with":
		return true
	}
	return false
}
