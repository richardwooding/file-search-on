package content

import (
	"bufio"
	"bytes"
	"regexp"
)

var (
	// foo <- function(...)  or  foo = function(...)
	// Captures the LHS identifier. R doesn't have a dedicated
	// function-declaration keyword — functions are values bound to
	// names via assignment. The right-hand side must begin with
	// `function` (not `function(` since whitespace is allowed).
	rFuncRe = regexp.MustCompile(`^\s*([A-Za-z_.][\w.]*)\s*(?:<-|=)\s*function\s*\(`)

	// library(foo) / library("foo") / library('foo') /
	// require(foo) / require("foo") / require('foo') /
	// requireNamespace("foo")
	rImportRe = regexp.MustCompile(`^\s*(?:library|require|requireNamespace)\s*\(\s*['"]?([A-Za-z_.][\w.]*)['"]?\s*[,)]`)

	// setClass("Foo", ...)  setGeneric("foo", ...)  setRefClass("Foo")
	// First-arg quoted name (single or double quotes).
	rSetClassRe = regexp.MustCompile(`\bset(?:Class|Generic|RefClass)\s*\(\s*['"]([A-Za-z_.][\w.]*)['"]`)

	// Foo <- R6Class("Foo", ...)  or  Foo <- R6::R6Class("Foo", ...)
	// Captures the quoted class-name argument (which is canonical;
	// the LHS name may differ but rarely does in practice). Both
	// the bare `R6Class` and namespaced `R6::R6Class` forms work.
	rR6Re = regexp.MustCompile(`(?:R6::)?R6Class\s*\(\s*['"]([A-Za-z_.][\w.]*)['"]`)
)

// extractRSymbols scans R source line-by-line. Captures:
//   - top-level + nested function assignments (`foo <- function(...)`
//     or `foo = function(...)`). Both standard `<-` and `=` assignment
//     forms are matched; CRAN style is `<-` but `=` is valid syntax.
//   - S4 class / generic / reference-class declarations via
//     setClass("Foo", ...) / setGeneric("foo", ...) / setRefClass —
//     emitted as type_names (with the quoted name being canonical).
//   - R6 / R5 reference classes via the `R6Class("Foo", ...)` and
//     namespaced `R6::R6Class("Foo", ...)` forms — emitted as
//     type_names.
//   - library() / require() / requireNamespace() calls — bareword
//     OR quoted package name captured.
//
// Limitations (documented in source-code.md):
//   - String literals containing assignment-shaped text could in
//     principle false-match, but the leading-whitespace anchor +
//     identifier-must-start-with-letter guard keeps the common
//     cases safe.
//   - Multi-line function signatures (parameter list wrapping past
//     line end) match on the line with `<- function(` only.
//   - The `<<-` super-assignment operator is treated the same as
//     `<-` because the regex matches `<-` substring — agents asking
//     "where is `foo` defined?" want both shapes anyway.
func extractRSymbols(src []byte) (functions, types, imports []string) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		line := scanner.Text()
		// R6 first so we don't emit BOTH a function (from the
		// `Foo <-` LHS) and a type_name. The R6 line shape is
		// `Foo <- R6Class("Foo", ...)` — the function regex would
		// not match because the RHS doesn't start with `function`,
		// but checking R6 first keeps the logic explicit.
		if m := rR6Re.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		// setClass / setGeneric / setRefClass — anywhere on the line.
		if m := rSetClassRe.FindStringSubmatch(line); m != nil {
			types = append(types, m[1])
			continue
		}
		if m := rFuncRe.FindStringSubmatch(line); m != nil {
			functions = append(functions, m[1])
			continue
		}
		if m := rImportRe.FindStringSubmatch(line); m != nil {
			imports = append(imports, m[1])
		}
	}
	return
}
