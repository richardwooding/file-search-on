package content

import "strings"

// FunctionSpan is a top-level function / method definition's name and 1-based
// inclusive line range. Used for function-level semantic chunking (issue
// #366): each span becomes one embedding chunk so a semantic hit can report
// the matching symbol, not just the file.
type FunctionSpan struct {
	Name      string
	StartLine int
	EndLine   int
}

// FunctionSpans returns the function / method spans for a source content type
// (e.g. "source/go", "source/rust"). Returns nil when the type isn't a wired
// source language or nothing parses — callers fall back to byte-window
// chunking. Reuses the same extractors as symbol extraction: the stdlib
// go/ast for Go, tree-sitter for every other wired language.
func FunctionSpans(contentTypeName string, src []byte) []FunctionSpan {
	language, ok := strings.CutPrefix(contentTypeName, "source/")
	if !ok || !symbolExtractorWired(language) {
		return nil
	}
	if language == "go" {
		return goFunctionSpans(src)
	}
	return tsFunctionSpans(language, src)
}
