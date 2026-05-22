package search_test

import (
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestSuggestions_BumpTimeoutFiresOnTimeout(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: "is_pdf"}, nil, 60.0, "timeout")
	if !containsSubstring(got, "timeout at 60.0s") {
		t.Errorf("missing bump-timeout suggestion, got %v", got)
	}
	// Doubled should appear as a Go duration string.
	if !containsSubstring(got, "2m1s") && !containsSubstring(got, "2m") {
		t.Errorf("doubled timeout not in suggestion, got %v", got)
	}
}

func TestSuggestions_BumpTimeoutSilentOnClientCancel(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: "is_pdf"}, nil, 30.0, "client_cancel")
	if containsSubstring(got, "bump") || containsSubstring(got, "timeout") {
		t.Errorf("client_cancel shouldn't produce a bump-timeout suggestion, got %v", got)
	}
}

func TestSuggestions_HotDirectoryFiresOnSharedPrefix(t *testing.T) {
	matches := []search.Match{
		{Path: "/Users/me/Code/file-search-on/a.go"},
		{Path: "/Users/me/Code/file-search-on/sub/b.go"},
		{Path: "/Users/me/Code/file-search-on/c.go"},
	}
	got := search.SuggestionsForSearch(search.Options{Expr: "is_source"}, matches, 30.0, "timeout")
	if !containsSubstring(got, "/Users/me/Code/file-search-on") {
		t.Errorf("missing hot-dir suggestion, got %v", got)
	}
}

func TestSuggestions_HotDirectorySkipsSingleMatch(t *testing.T) {
	matches := []search.Match{{Path: "/Users/me/x.go"}}
	got := search.SuggestionsForSearch(search.Options{Expr: "is_source"}, matches, 30.0, "timeout")
	for _, s := range got {
		if strings.Contains(s, "All 1 matches") {
			t.Errorf("hot-dir fired on a single match: %v", got)
		}
	}
}

func TestSuggestions_IncludeBodyWarning(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: "true", IncludeBody: true}, nil, 30.0, "timeout")
	if !containsSubstring(got, "--body") {
		t.Errorf("missing include-body warning, got %v", got)
	}
}

func TestSuggestions_MissingPrunes(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: "is_source"}, nil, 30.0, "timeout")
	if !containsSubstring(got, "node_modules") {
		t.Errorf("missing prunes hint not present, got %v", got)
	}
}

func TestSuggestions_PrunesSilentWhenAlreadyExcluding(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: "is_source", Excludes: []string{"node_modules"}}, nil, 30.0, "timeout")
	for _, s := range got {
		if strings.Contains(s, "node_modules") {
			t.Errorf("prunes hint fired despite --exclude already set: %v", got)
		}
	}
}

func TestSuggestions_LaxFilterFiresOnEmpty(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: ""}, nil, 30.0, "timeout")
	if !containsSubstring(got, "CEL filter is empty") {
		t.Errorf("lax-filter hint missing, got %v", got)
	}
}

func TestSuggestions_LaxFilterFiresOnTrue(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: "true"}, nil, 30.0, "timeout")
	if !containsSubstring(got, "CEL filter is empty") {
		t.Errorf("lax-filter hint missing for 'true', got %v", got)
	}
}

func TestSuggestions_LaxFilterSilentOnTypePredicate(t *testing.T) {
	got := search.SuggestionsForSearch(search.Options{Expr: "is_pdf"}, nil, 30.0, "timeout")
	for _, s := range got {
		if strings.Contains(s, "CEL filter is empty") {
			t.Errorf("lax-filter hint fired on real expression: %v", got)
		}
	}
}

func TestSuggestions_NoneOnSuccessfulCompletion(t *testing.T) {
	// reason == "" means a clean walk; only the bump-timeout suggestion
	// gates on reason. The other heuristics fire regardless. We don't
	// call SuggestionsForSearch on successful completion (callers gate
	// on cancelled=true), but the contract is "no bump-timeout".
	got := search.SuggestionsForSearch(search.Options{Expr: "is_pdf"}, nil, 30.0, "")
	for _, s := range got {
		if strings.Contains(s, "Walk hit the timeout") {
			t.Errorf("bump-timeout fired on non-timeout reason: %v", got)
		}
	}
}

func TestSuggestions_StatsVariantOmitsHotDir(t *testing.T) {
	got := search.SuggestionsForStats(search.Options{Expr: "true", IncludeBody: true}, 30.0, "timeout")
	for _, s := range got {
		if strings.Contains(s, "narrow") {
			t.Errorf("stats variant shouldn't surface hot-dir suggestion: %v", got)
		}
	}
	// But bump-timeout and body-warning should fire.
	if !containsSubstring(got, "Walk hit the timeout") {
		t.Errorf("stats bump-timeout missing: %v", got)
	}
	if !containsSubstring(got, "--body") {
		t.Errorf("stats body-warning missing: %v", got)
	}
}

func containsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
