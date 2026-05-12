package celexpr_test

import (
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// FuzzCELCompile feeds arbitrary strings to celexpr.New() — the CEL
// expression compiler. The expression source comes from untrusted
// CLI args (`file-search-on 'expr'`) and from MCP tool input
// (`{"expr": "..."}`), so the compiler is a real adversarial
// boundary.
//
// Contract: never panic; either return a valid *Evaluator or a
// non-nil error. Compilation errors are expected for nearly all
// inputs the fuzzer generates — that's fine. We just want to make
// sure cel-go's parser doesn't crash on adversarial Unicode,
// imbalanced delimiters, gigantic identifiers, etc.
func FuzzCELCompile(f *testing.F) {
	seeds := []string{
		"",
		"true",
		"false",
		"is_markdown",
		"is_pdf && page_count > 10",
		"size > 1000000",
		"name == \"\"",
		"levenshtein(artist, \"Radiohead\") <= 2",
		"point_in_polygon(gps_lat, gps_lon, [[0.0, 0.0]])",
		// Pathological starting points the fuzzer can grow from.
		"(((((",
		"a.b.c.d.e.f.g.h",
		"\"\\u0000\"",
		"size > size > size",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, expr string) {
		// New either returns (*Evaluator, nil) or (nil, err). It must
		// not panic, even on adversarial Unicode, recursion-bombing
		// parens, or absurdly long identifiers.
		_, _ = celexpr.New(expr)
	})
}
