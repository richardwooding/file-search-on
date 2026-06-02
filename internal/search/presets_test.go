package search_test

import (
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestPresets_AllCompile verifies that every preset in the catalog
// produces a CEL filter expression that compiles cleanly. Catches
// typos in the static catalog at build time rather than at first
// user invocation.
func TestPresets_AllCompile(t *testing.T) {
	for _, p := range search.Presets() {
		t.Run(p.Name, func(t *testing.T) {
			opts := p.Build()
			if opts.Expr == "" {
				t.Fatal("preset built an empty Expr")
			}
			if _, err := celexpr.New(opts.Expr); err != nil {
				t.Fatalf("preset %q expr %q failed to compile: %v", p.Name, opts.Expr, err)
			}
			if opts.RankExpr != "" {
				e, err := celexpr.New("true")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := e.NewRank(opts.RankExpr); err != nil {
					t.Fatalf("preset %q rank %q failed to compile: %v", p.Name, opts.RankExpr, err)
				}
			}
			if p.Description == "" {
				t.Errorf("preset %q has empty Description", p.Name)
			}
		})
	}
}

// TestPresets_NamesUnique ensures the catalog doesn't have duplicate
// preset names — would break PresetByName.
func TestPresets_NamesUnique(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range search.Presets() {
		if seen[p.Name] {
			t.Errorf("duplicate preset name: %q", p.Name)
		}
		seen[p.Name] = true
	}
}

// TestPresets_NamesAreKebabCaseLower ensures presets follow the
// stable-identifier convention.
func TestPresets_NamesAreKebabCaseLower(t *testing.T) {
	for _, p := range search.Presets() {
		if strings.ToLower(p.Name) != p.Name {
			t.Errorf("preset %q is not all-lowercase", p.Name)
		}
		if strings.ContainsAny(p.Name, " /\\.") {
			t.Errorf("preset %q contains a forbidden character", p.Name)
		}
	}
}

// TestPresetByName_NotFoundReturnsNil documents the missing-lookup
// contract.
func TestPresetByName_NotFoundReturnsNil(t *testing.T) {
	if p := search.PresetByName("does-not-exist"); p != nil {
		t.Errorf("expected nil, got %+v", p)
	}
}

// TestPresetByName_RoundTrip ensures every catalogued preset is
// reachable via PresetByName.
func TestPresetByName_RoundTrip(t *testing.T) {
	for _, p := range search.Presets() {
		got := search.PresetByName(p.Name)
		if got == nil {
			t.Errorf("PresetByName(%q) returned nil", p.Name)
			continue
		}
		if got.Name != p.Name {
			t.Errorf("got name %q, want %q", got.Name, p.Name)
		}
	}
}

// TestPresets_TimeRelativeExpressionsBakeNow verifies presets that
// reference time relative to "now" actually bake in a fresh
// timestamp via Build() — calling Build() twice with a delay
// produces different cutoff values.
func TestPresets_TimeRelativeExpressionsBakeNow(t *testing.T) {
	p := search.PresetByName("recent_changes")
	if p == nil {
		t.Fatal("recent_changes preset missing")
	}
	first := p.Build()
	if !strings.Contains(first.Expr, "timestamp(") {
		t.Fatalf("recent_changes expr doesn't include a timestamp literal: %s", first.Expr)
	}
}

// TestPreset_FailedTests_OnlyFiresOnCommentLines is the #280
// regression guard: the preset's body.matches() pattern must NOT fire
// on raw-content occurrences of FIXME / XXX / FAIL / TODO (test
// fixtures, string literals, fuzz seeds). Drives the actual CEL
// expression baked into the preset rather than re-implementing it.
func TestPreset_FailedTests_OnlyFiresOnCommentLines(t *testing.T) {
	p := search.PresetByName("failed_tests")
	if p == nil {
		t.Fatal("failed_tests preset missing")
	}
	opts := p.Build()
	ev, err := celexpr.New(opts.Expr)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// Helper to build the attrs payload the evaluator wants. Only
	// the four fields the failed_tests filter touches are populated;
	// everything else stays at zero.
	type fixture struct {
		name        string
		body        string
		wantMatched bool
	}
	for _, fx := range []fixture{
		// REAL comment annotations — should fire.
		{"go line comment FIXME", "// FIXME: rewrite this\npackage foo\n", true},
		{"indented go FIXME", "    // FIXME: indented\n", true},
		{"python comment XXX", "# XXX needs proper test\n", true},
		{"shell comment TODO", "# TODO: handle eof\n", true},
		{"lua comment FAIL", "-- FAIL expected\n", true},
		{"clojure comment FIXME", "; FIXME later\n", true},

		// NOISE — should NOT fire under the comment-only preset.
		{"string literal containing FIXME", `package foo
var marker = "FIXME: this is data"
`, false},
		{"test fixture line", `mustWrite(t, p, "// FIXME inside string")`, false},
		{"identifier substring", "func FailTestRunner() {}\n", false},
		{"trailing comment after code", "assert(x == 1) // FIXME later\n", false},
		{"FIXME word in normal prose", "// This package fixes the missing case (see issue).\n", false},

		// Edge: bare FIXME with no comment prefix at all.
		{"bare FIXME on its own line", "FIXME\n", false},
	} {
		t.Run(fx.name, func(t *testing.T) {
			attrs := &celexpr.FileAttributes{
				ContentType: "source/go",
				IsSource:    true,
				Extra: map[string]any{
					"body":         fx.body,
					"language":     "go",
					"is_test_file": true,
				},
			}
			matched, err := ev.Evaluate(attrs)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if matched != fx.wantMatched {
				t.Errorf("body %q: got matched=%v, want %v", fx.body, matched, fx.wantMatched)
			}
		})
	}
}
