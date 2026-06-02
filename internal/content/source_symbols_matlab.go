package content

import (
	"bufio"
	"bytes"
	"regexp"
)

var (
	// function name(args)
	// function out = name(args)
	// function [a, b] = name(args)
	// function out=name(args)        (whitespace-tolerant)
	// The optional `(?:[\[\]\w,\s]+=\s*)?` group consumes any
	// return-value declaration (single name, bracketed list, with
	// or without surrounding whitespace) up to the `=` sign.
	matlabFuncRe = regexp.MustCompile(`^\s*function\s+(?:[\[\]\w,\s]+=\s*)?([A-Za-z_]\w*)\s*\(`)

	// classdef Foo
	// classdef Foo < Bar
	// classdef Foo < Bar & Baz
	// classdef (Abstract) Foo
	// classdef (Abstract, Sealed) Foo < Bar
	matlabClassRe = regexp.MustCompile(`^\s*classdef\s+(?:\([^)]+\)\s+)?([A-Za-z_]\w*)\b`)

	// import foo.bar.baz
	// import foo.bar.*
	// The dotted name including a trailing `.*` is captured verbatim.
	// Identifier-segment-based pattern (not `[\w.]+`) so the trailing
	// `.*` wildcard isn't swallowed by a greedy dot-inclusive match.
	matlabImportRe = regexp.MustCompile(`^\s*import\s+([A-Za-z_]\w*(?:\.\w+)*(?:\.\*)?)`)
)

// extractMATLABSymbols scans MATLAB source line-by-line. Captures:
//   - function declarations in every legal MATLAB shape:
//       function name(args)
//       function out = name(args)
//       function [a, b] = name(args)
//     The captured name is the identifier immediately before the
//     parameter list. Nested functions (inside another function's
//     body) and methods inside `methods ... end` blocks of a
//     classdef are matched identically.
//   - classdef declarations (with optional attributes like
//     `(Abstract)` / `(Sealed, Hidden)`, with or without
//     `< Superclass` inheritance) — emitted as type_names.
//   - import statements (`import foo.bar.Baz` or wildcard
//     `import foo.bar.*`) — the dotted name is captured verbatim,
//     wildcard suffix preserved.
//
// Limitations (documented in source-code.md):
//   - The `local function` MATLAB keyword variant is rare; standard
//     `function` covers the common case.
//   - Multi-line function signatures (parameter list wrapping past
//     line end) match on the line carrying `function ... name(` only.
//   - Anonymous functions `f = @(x) x.^2` have no captured name —
//     correctly not matched.
func extractMATLABSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		if m := matlabClassRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := matlabImportRe.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
			continue
		}
		if m := matlabFuncRe.FindStringSubmatch(line); m != nil {
			functions = append(functions, m[1])
		}
	}
	return
}
