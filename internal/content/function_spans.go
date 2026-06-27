package content

import (
	"strings"

	tssymbols "github.com/richardwooding/treesitter-symbols"
)

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
// chunking. Go uses the stdlib go/ast; every other wired language uses
// treesitter-symbols (#540).
func FunctionSpans(contentTypeName string, src []byte) []FunctionSpan {
	language, ok := strings.CutPrefix(contentTypeName, "source/")
	if !ok || !symbolExtractorWired(language) {
		return nil
	}
	if language == "go" {
		return goFunctionSpans(src)
	}
	sym, err := tssymbols.Extract(language, src)
	if err != nil || len(sym.FunctionSpans) == 0 {
		return nil
	}
	out := make([]FunctionSpan, 0, len(sym.FunctionSpans))
	for _, fs := range sym.FunctionSpans {
		out = append(out, FunctionSpan{Name: fs.Name, StartLine: fs.StartLine, EndLine: fs.EndLine})
	}
	return out
}
