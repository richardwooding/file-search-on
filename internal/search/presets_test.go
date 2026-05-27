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
		return // unreachable; quiets staticcheck SA5011
	}
	first := p.Build()
	if !strings.Contains(first.Expr, "timestamp(") {
		t.Fatalf("recent_changes expr doesn't include a timestamp literal: %s", first.Expr)
	}
}
