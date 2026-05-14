package search_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/search"
)

func TestValidateFields_KnownNames(t *testing.T) {
	if err := search.ValidateFields([]string{"title", "author", "taken_at", "is_image"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateFields_UnknownName(t *testing.T) {
	err := search.ValidateFields([]string{"title", "not_a_real_field"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not_a_real_field") {
		t.Errorf("error %q does not mention the offending name", err.Error())
	}
}

func TestValidateFields_EmptyOK(t *testing.T) {
	if err := search.ValidateFields(nil); err != nil {
		t.Errorf("nil fields should be valid: %v", err)
	}
	if err := search.ValidateFields([]string{}); err != nil {
		t.Errorf("empty fields should be valid: %v", err)
	}
}

func TestProjectMatch_KeepsAlwaysOn(t *testing.T) {
	m := search.Match{
		Path:        "/a.md",
		ContentType: "markdown",
		Size:        42,
		Title:       "Hello",
		WordCount:   100,
	}
	out := search.ProjectMatch(m, []string{"word_count"})
	if out.Path != "/a.md" || out.ContentType != "markdown" || out.Size != 42 {
		t.Errorf("always-on fields zeroed: %+v", out)
	}
	if out.Title != "" {
		t.Errorf("Title=%q should be zeroed (not in projection)", out.Title)
	}
	if out.WordCount != 100 {
		t.Errorf("WordCount=%d should be preserved (in projection)", out.WordCount)
	}
}

func TestProjectMatch_EmptyAllowKeepsEverything(t *testing.T) {
	m := search.Match{
		Path:        "/a.md",
		ContentType: "markdown",
		Title:       "Hello",
		WordCount:   100,
	}
	out := search.ProjectMatch(m, nil)
	if out.Title != "Hello" || out.WordCount != 100 {
		t.Errorf("nil allow should preserve everything: %+v", out)
	}
}

func TestProjectMatch_JSONOmitsZeroed(t *testing.T) {
	m := search.Match{
		Path:        "/a.md",
		ContentType: "markdown",
		Size:        42,
		Title:       "Hello",
		Author:      "Jane",
		WordCount:   100,
		IsMarkdown:  true,
	}
	out := search.ProjectMatch(m, []string{"title"})
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	// always-on
	for _, want := range []string{`"path":"/a.md"`, `"content_type":"markdown"`, `"size":42`, `"title":"Hello"`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON %q missing %q", s, want)
		}
	}
	// projected away
	for _, banned := range []string{`"author"`, `"word_count"`, `"is_markdown"`} {
		if strings.Contains(s, banned) {
			t.Errorf("JSON %q should not contain %q (omitempty + zeroed)", s, banned)
		}
	}
}

func TestProjectMatches_BulkAndIdempotent(t *testing.T) {
	ms := []search.Match{
		{Path: "/a", ContentType: "x", Title: "A", WordCount: 1},
		{Path: "/b", ContentType: "x", Title: "B", WordCount: 2},
	}
	search.ProjectMatches(ms, []string{"title"})
	if ms[0].WordCount != 0 || ms[1].WordCount != 0 {
		t.Errorf("WordCount should be zeroed: %+v", ms)
	}
	if ms[0].Title != "A" || ms[1].Title != "B" {
		t.Errorf("Title should be preserved: %+v", ms)
	}
	// Second pass is a no-op.
	search.ProjectMatches(ms, []string{"title"})
	if ms[0].Title != "A" {
		t.Errorf("Title lost on second projection: %+v", ms)
	}
}

func TestMatchFieldNames_IncludesCommonKeys(t *testing.T) {
	names := search.MatchFieldNames()
	want := []string{"path", "content_type", "size", "title", "author", "is_image", "taken_at", "duration"}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("MatchFieldNames missing %q", w)
		}
	}
}

// Sanity: unknown-field error wraps as expected for callers using errors.Is /
// errors.As patterns. (Not strictly required, but cheap to verify.)
func TestValidateFields_NotErrIs(t *testing.T) {
	err := search.ValidateFields([]string{"nope"})
	// Custom errors aren't sentinel — Is should return false against
	// stdlib EOF etc; this is mostly a smoke test that the err is a
	// real error value and not a panic.
	if errors.Is(err, nil) {
		t.Fatal("err should not match nil")
	}
}
