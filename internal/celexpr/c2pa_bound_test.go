package celexpr_test

import (
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// TestC2PABoundExpressions pins the queries the docs promise, evaluated both
// with the attribute set and with it absent — a CEL variable declared in the
// environment but missing from the activation defaults compiles and then fails
// at evaluation time on every file that lacks it, so both halves must exist.
func TestC2PABoundExpressions(t *testing.T) {
	bound := &celexpr.FileAttributes{
		Name: "signed.jpg", Path: "/a/signed.jpg", Ext: ".jpg", ContentType: "image/jpeg", IsImage: true,
		Extra: map[string]any{"is_c2pa": true, "c2pa_valid": true, "c2pa_bound": "verified"},
	}
	carried := &celexpr.FileAttributes{
		Name: "doc.pdf", Path: "/a/doc.pdf", Ext: ".pdf", ContentType: "application/pdf",
		Extra: map[string]any{"is_c2pa": true, "c2pa_valid": true, "c2pa_bound": "unevaluated"},
	}
	// Verification switched off, so the attribute never populated.
	plain := &celexpr.FileAttributes{Name: "plain.jpg", Path: "/a/plain.jpg", Ext: ".jpg", ContentType: "image/jpeg", IsImage: true}

	cases := []struct {
		expr  string
		attrs *celexpr.FileAttributes
		want  bool
	}{
		{`is_c2pa && c2pa_valid && c2pa_bound == "verified"`, bound, true},
		{`is_c2pa && c2pa_valid && c2pa_bound == "verified"`, carried, false},
		{`c2pa_bound == "unevaluated"`, carried, true},
		{`c2pa_bound == "unevaluated"`, bound, false},
		{`c2pa_bound == ""`, plain, true},
	}
	for _, tc := range cases {
		eval, err := celexpr.New(tc.expr)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.expr, err)
		}
		got, err := eval.Evaluate(tc.attrs)
		if err != nil {
			t.Fatalf("eval %q on %s: %v", tc.expr, tc.attrs.Name, err)
		}
		if got != tc.want {
			t.Errorf("expr %q on %s: got %v, want %v", tc.expr, tc.attrs.Name, got, tc.want)
		}
	}
}
