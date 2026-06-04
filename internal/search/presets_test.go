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

// TestPresets_NewSet confirms the 6 v0.74.x-era presets are
// registered, compile, and have the shape the docs advertise — git-
// aware ones use git_* sort keys (which the auto-enable picks up
// through celexpr.NeedsGit on opts.Sort).
func TestPresets_NewSet(t *testing.T) {
	wantPresets := map[string]struct {
		wantSort  string
		wantLimit int
		mustHave  string // substring that must appear in Expr
	}{
		"recent_commits":  {wantSort: "git_last_commit_time", wantLimit: 50, mustHave: "git_last_commit_time"},
		"hot_files":       {wantSort: "git_commit_count", wantLimit: 20, mustHave: "git_commit_count"},
		"prod_code":       {wantSort: "loc", wantLimit: 100, mustHave: "!is_generated_code"},
		"untracked_code":  {wantSort: "size", wantLimit: 50, mustHave: "!is_git_tracked"},
		"generated_code":  {wantSort: "size", wantLimit: 50, mustHave: "is_generated_code"},
		"test_files":      {wantSort: "loc", wantLimit: 50, mustHave: "is_test_file"},
	}
	for name, want := range wantPresets {
		t.Run(name, func(t *testing.T) {
			p := search.PresetByName(name)
			if p == nil {
				t.Fatalf("preset %q not registered", name)
			}
			opts := p.Build()
			if opts.Sort != want.wantSort {
				t.Errorf("Sort = %q, want %q", opts.Sort, want.wantSort)
			}
			if opts.Limit != want.wantLimit {
				t.Errorf("Limit = %d, want %d", opts.Limit, want.wantLimit)
			}
			if !strings.Contains(opts.Expr, want.mustHave) {
				t.Errorf("Expr missing %q substring: %s", want.mustHave, opts.Expr)
			}
			// Every new preset must compile through the same env
			// (existing TestPresets_AllCompile already covers this for
			// the whole catalog; we just sanity-check the new ones too).
			if _, err := celexpr.New(opts.Expr); err != nil {
				t.Errorf("preset %q expr failed to compile: %v", name, err)
			}
		})
	}
}

// TestPresets_NewSet_AutoEnableGit confirms the git-aware presets'
// expr/sort keys trigger celexpr.NeedsGit so callers don't have to
// pass with_git explicitly.
func TestPresets_NewSet_AutoEnableGit(t *testing.T) {
	for _, name := range []string{"recent_commits", "hot_files", "prod_code", "untracked_code"} {
		p := search.PresetByName(name)
		if p == nil {
			t.Fatalf("preset %q not registered", name)
		}
		opts := p.Build()
		if !celexpr.NeedsGit(opts.Expr, opts.Sort, opts.RankExpr) {
			t.Errorf("preset %q should auto-enable with_git via NeedsGit (expr=%q, sort=%q)", name, opts.Expr, opts.Sort)
		}
	}
	// Non-git-aware presets stay false.
	for _, name := range []string{"recent_changes", "large_files", "system_metadata", "test_files", "generated_code"} {
		p := search.PresetByName(name)
		if p == nil {
			t.Fatalf("preset %q not registered", name)
		}
		opts := p.Build()
		if celexpr.NeedsGit(opts.Expr, opts.Sort, opts.RankExpr) {
			t.Errorf("preset %q should NOT auto-enable with_git (expr=%q, sort=%q)", name, opts.Expr, opts.Sort)
		}
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

// TestPresets_EbookSet asserts the EPUB-oriented presets exist, are
// scoped to is_epub, and sort on a key meaningful for ebooks (EPUB has
// no word_count/page_count, so size/name/mod_time only).
func TestPresets_EbookSet(t *testing.T) {
	want := map[string]string{
		"large_ebooks":       "size",
		"recent_ebooks":      "mod_time",
		"untagged_ebooks":    "name",
		"non_english_ebooks": "name",
	}
	for name, sortKey := range want {
		p := search.PresetByName(name)
		if p == nil {
			t.Errorf("preset %q not found", name)
			continue
		}
		opts := p.Build()
		if !strings.Contains(opts.Expr, "is_epub") {
			t.Errorf("preset %q expr %q is not scoped to is_epub", name, opts.Expr)
		}
		if opts.Sort != sortKey {
			t.Errorf("preset %q sort = %q, want %q", name, opts.Sort, sortKey)
		}
		if _, err := celexpr.New(opts.Expr); err != nil {
			t.Errorf("preset %q expr %q failed to compile: %v", name, opts.Expr, err)
		}
	}
}
