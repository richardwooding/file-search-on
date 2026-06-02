package search_test

import (
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestBodyMatchSuggestion_ExtractsLiteral(t *testing.T) {
	cases := []struct {
		name     string
		expr     string
		wantSub  string // substring the hint must contain
		wantHint bool
	}{
		{
			"contains with literal",
			`is_source && body.contains("panic")`,
			`pattern="panic"`,
			true,
		},
		{
			"matches with literal",
			`is_markdown && body.matches("\\bAPI\\b")`,
			`pattern=`, // the escape sequence makes exact-substring brittle; just confirm pattern= is present
			true,
		},
		{
			"contains nested in a bigger expr",
			`is_source && language == "go" && body.contains("TODO") && loc > 100`,
			`pattern="TODO"`,
			true,
		},
		{
			"startsWith — generic fallback",
			`is_source && body.startsWith("// SPDX-")`,
			`find_matches`,
			true,
		},
		{
			"endsWith — generic fallback",
			`is_text && body.endsWith("EOF")`,
			`find_matches`,
			true,
		},
		{
			"no body method",
			`is_source && language == "go" && loc > 100`,
			"",
			false,
		},
		{
			"empty expr",
			"",
			"",
			false,
		},
		{
			"body.length is NOT body content (no hint)",
			// We're matching the method-call pattern, not the .length attr.
			// This expression is contrived; we just check we don't false-positive.
			`size(body) > 1000`,
			"",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := search.BodyMatchSuggestion(tc.expr)
			if tc.wantHint && got == "" {
				t.Errorf("expected a hint, got empty")
				return
			}
			if !tc.wantHint && got != "" {
				t.Errorf("expected no hint, got %q", got)
				return
			}
			if tc.wantHint && !strings.Contains(got, tc.wantSub) {
				t.Errorf("hint %q missing %q", got, tc.wantSub)
			}
		})
	}
}

// TestBodyMatchSuggestion_EscapedQuoteStillFires asserts the hint
// is at least emitted when the literal contains escaped quotes — the
// extracted pattern may be malformed (the simple regex stops at the
// first unescaped " it sees, which is the escape's closing quote)
// but the agent still gets the wrong-tool nudge. Documented
// best-effort behaviour.
func TestBodyMatchSuggestion_EscapedQuoteStillFires(t *testing.T) {
	expr := `body.contains("say \"hi\"")`
	got := search.BodyMatchSuggestion(expr)
	if got == "" {
		t.Fatal("expected a hint (even if pattern extraction is degraded)")
	}
	if !strings.Contains(got, "find_matches") {
		t.Errorf("hint should mention find_matches; got %q", got)
	}
}
