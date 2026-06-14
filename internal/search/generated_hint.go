package search

import "fmt"

// Generated-code dominance hint (issue #430). Ranked code-analysis output
// (complexity / dead_code / test_gaps / duplicate_functions / unused_exports)
// is easily swamped by machine-generated files (protobuf, oapi-codegen,
// mocks, easyjson, …) — they're large and mechanically complex, so they
// crowd out the hand-written signal. When generated rows dominate a result
// set we append a one-line nudge toward the `!is_generated_code` filter
// rather than silently excluding them (some callers do want generated code).
const (
	// generatedHintFraction: emit the hint once generated rows are at least
	// this share of the results.
	generatedHintFraction = 0.34
	// minGeneratedForHint: ...and there are at least this many, so a couple
	// of generated rows in a tiny result set don't trip it.
	minGeneratedForHint = 3
)

// generatedHint returns the nudge string when generated rows dominate, or ""
// otherwise. genCount is how many of the `total` returned rows live in
// generated files.
func generatedHint(genCount, total int) string {
	if genCount < minGeneratedForHint || total == 0 || float64(genCount) < generatedHintFraction*float64(total) {
		return ""
	}
	return fmt.Sprintf("%d of %d results are generated code — add `&& !is_generated_code` to the expression to exclude them.", genCount, total)
}

// countGenerated counts how many of paths are in the generated set.
func countGenerated(paths []string, generated map[string]bool) int {
	n := 0
	for _, p := range paths {
		if generated[p] {
			n++
		}
	}
	return n
}

// generatedHintFor is the convenience combination: count the generated paths
// in a result and build the hint.
func generatedHintFor(paths []string, generated map[string]bool) string {
	return generatedHint(countGenerated(paths, generated), len(paths))
}
