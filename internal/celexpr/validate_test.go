package celexpr_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

func TestValidateExpr_HappyPath(t *testing.T) {
	res := celexpr.ValidateExpr(`is_source && language == "go" && loc > 100`)
	if !res.OK {
		t.Fatalf("expected ok=true, got error: %s", res.Error)
	}
	wantVars := []string{"is_source", "language", "loc"}
	for _, v := range wantVars {
		if !slices.Contains(res.ReferencedVariables, v) {
			t.Errorf("ReferencedVariables missing %q; got %v", v, res.ReferencedVariables)
		}
	}
	if len(res.ReferencedFunctions) != 0 {
		t.Errorf("ReferencedFunctions should be empty for this expr; got %v", res.ReferencedFunctions)
	}
}

func TestValidateExpr_TyposReturnError_AndSuggestion(t *testing.T) {
	res := celexpr.ValidateExpr(`is_source && imprts.size() > 0`)
	if res.OK {
		t.Fatal("expected ok=false for typo")
	}
	if res.Error == "" {
		t.Error("expected non-empty Error")
	}
	if !strings.Contains(res.Suggestion, "imports") {
		t.Errorf("expected suggestion to include 'imports'; got %q", res.Suggestion)
	}
}

func TestValidateExpr_ReferencedFunctions(t *testing.T) {
	res := celexpr.ValidateExpr(`is_source && levenshtein(author, "Alice") < 3`)
	if !res.OK {
		t.Fatalf("expected ok=true, got: %s", res.Error)
	}
	if !slices.Contains(res.ReferencedFunctions, "levenshtein") {
		t.Errorf("ReferencedFunctions should include levenshtein; got %v", res.ReferencedFunctions)
	}
	for _, v := range []string{"is_source", "author"} {
		if !slices.Contains(res.ReferencedVariables, v) {
			t.Errorf("ReferencedVariables missing %q; got %v", v, res.ReferencedVariables)
		}
	}
}

func TestValidateExpr_StringLiteralsDontPolluteRefs(t *testing.T) {
	// "size" appears inside a string literal — it should NOT count
	// as a referenced variable even though "size" is a real var.
	res := celexpr.ValidateExpr(`is_source && body.contains("size")`)
	if !res.OK {
		t.Fatalf("expected ok=true, got: %s", res.Error)
	}
	for _, v := range res.ReferencedVariables {
		if v == "size" {
			t.Errorf("size appearing inside string literal should not surface as a ReferencedVariable; got %v", res.ReferencedVariables)
		}
	}
}

func TestValidateExpr_EmptyExprErrors(t *testing.T) {
	res := celexpr.ValidateExpr("")
	// cel-go errors on empty expr; that's fine — we just expect not-ok.
	if res.OK {
		t.Error("expected ok=false for empty expr")
	}
}

func TestValidateExpr_DistantTypoNoSuggestion(t *testing.T) {
	// Distance > 2 from any known name — no suggestion offered.
	res := celexpr.ValidateExpr(`is_source && completelyMadeUpThing > 0`)
	if res.OK {
		t.Fatal("expected ok=false")
	}
	if res.Suggestion != "" {
		t.Errorf("distant typo should NOT trigger a suggestion; got %q", res.Suggestion)
	}
}
