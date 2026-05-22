package celexpr

import (
	"strings"
	"testing"
)

// TestRank_DoubleLiteral exercises the simplest path — a CEL
// expression that's just a literal double.
func TestRank_DoubleLiteral(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.NewRank("1.5")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Eval(&FileAttributes{})
	if err != nil {
		t.Fatal(err)
	}
	if got != 1.5 {
		t.Errorf("got %v, want 1.5", got)
	}
}

// TestRank_ArithmeticOverAttributes exercises the typical use case
// — combining file attributes into a numeric score.
func TestRank_ArithmeticOverAttributes(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.NewRank("size / 1024")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Eval(&FileAttributes{Size: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if got != 4.0 {
		t.Errorf("got %v, want 4.0 (4096 / 1024)", got)
	}
}

// TestRank_BooleanCoercion exercises the shortcut where a bare
// predicate becomes the rank — true → 1.0, false → 0.0.
func TestRank_BooleanCoercion(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.NewRank("is_pdf")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Eval(&FileAttributes{IsPDF: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != 1.0 {
		t.Errorf("true → got %v, want 1.0", got)
	}
	got, err = r.Eval(&FileAttributes{IsPDF: false})
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.0 {
		t.Errorf("false → got %v, want 0.0", got)
	}
}

// TestRank_IntCoercion confirms int-returning expressions
// (e.g. `size`) are coerced to float64.
func TestRank_IntCoercion(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.NewRank("size")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Eval(&FileAttributes{Size: 12345})
	if err != nil {
		t.Fatal(err)
	}
	if got != 12345.0 {
		t.Errorf("got %v, want 12345.0", got)
	}
}

// TestRank_SimilarityComposition is the headline use case from
// issue #168 — semantic similarity blended with another scalar.
func TestRank_SimilarityComposition(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.NewRank("similarity * 0.7 + (is_pdf ? 0.3 : 0.0)")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Eval(&FileAttributes{Similarity: 0.8, IsPDF: true})
	if err != nil {
		t.Fatal(err)
	}
	want := 0.8*0.7 + 0.3 // 0.86
	// Float comparison: allow a small epsilon for the * 0.7
	// multiplication.
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("got %v, want %v (diff %v)", got, want, diff)
	}
}

// TestRank_CompileError surfaces malformed CEL expressions at
// compile time, not at first eval.
func TestRank_CompileError(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.NewRank("size +") // syntax error
	if err == nil {
		t.Fatal("expected compile error, got nil")
	}
	if !strings.Contains(err.Error(), "compiling rank expression") {
		t.Errorf("error doesn't mention rank expression: %v", err)
	}
}

// TestRank_UnsupportedReturnType — strings can compile but evaluate
// to types.String. Eval should return a clear error.
func TestRank_UnsupportedReturnType(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.NewRank(`"hello"`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Eval(&FileAttributes{})
	if err == nil {
		t.Fatal("expected eval error for string return type, got nil")
	}
	if !strings.Contains(err.Error(), "want double, int, or bool") {
		t.Errorf("error doesn't mention expected return types: %v", err)
	}
}

// TestRank_NegativeValues confirms negative ints flow through
// cleanly — useful for "rank by smaller-is-better" patterns where
// the user wants oldest-first via descending sort on -mtime.
func TestRank_NegativeValues(t *testing.T) {
	e, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	// CEL doesn't auto-coerce double × int, so use unary minus on
	// the int directly. This is the idiomatic way to flip sign in
	// a rank expression.
	r, err := e.NewRank("-size")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Eval(&FileAttributes{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	if got != -100.0 {
		t.Errorf("got %v, want -100.0", got)
	}
}
