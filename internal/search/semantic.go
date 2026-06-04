package search

import "fmt"

// SimilarityThresholdExpr builds the CEL clause that folds a semantic
// similarity threshold into a search expression.
//
// The literal is wrapped in double(...) on purpose. The `similarity` CEL
// variable is a double, and cel-go has no `double >= int` overload, so a
// whole-number threshold like 0.0 or 1.0 — which fmt's %v/%g render as
// the int literal "0" / "1" — would compile to `similarity >= 0` and
// fail with "no matching overload for '_>=_' applied to '(double, int)'"
// (issue #307). double(%v) yields double(0) / double(0.5) / double(1),
// all valid doubles, while keeping full float precision via %v.
func SimilarityThresholdExpr(threshold float64) string {
	return fmt.Sprintf("similarity >= double(%v)", threshold)
}
