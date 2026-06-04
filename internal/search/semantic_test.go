package search_test

import (
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestSimilarityThresholdExpr_Compiles is the regression for issue #307:
// a whole-number threshold (0.0, 1.0) must produce a CEL expression that
// compiles. Before the double(...) wrapper, %g rendered 0.0 as the int
// literal "0" and `similarity >= 0` failed with cel-go's missing
// double>=int overload.
func TestSimilarityThresholdExpr_Compiles(t *testing.T) {
	for _, th := range []float64{0, 0.0, 1, 1.0, 0.5, 0.55, 0.7, 0.123456789, 1e-7} {
		expr := search.SimilarityThresholdExpr(th)
		if _, err := celexpr.New(expr); err != nil {
			t.Errorf("threshold %v -> %q failed to compile: %v", th, expr, err)
		}
		// Also the AND-combined form the CLI / MCP build.
		combined := "(is_epub) && " + expr
		if _, err := celexpr.New(combined); err != nil {
			t.Errorf("threshold %v combined -> %q failed to compile: %v", th, combined, err)
		}
	}
}
