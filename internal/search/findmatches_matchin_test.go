package search_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// Per-file Go fixture exercising every match-in interesting case. The
// TODO landmark appears five times:
//
//  1. on a line-comment line                     → comments
//  2. inside a string literal                    → code (whole line is code)
//  3. as a trailing comment after code            → code (rule: line-start classifier)
//  4. inside an open block comment, line 2 of 3  → comments (block-continuation)
//  5. inside an identifier (FooTODOBar)           → code
//
// Each case lands on a distinct line so we can assert by line number.
const fixtureGo = `package main

// TODO: this is a real comment-line match
const s = "TODO inside string"
var x = 1 // TODO trailing
/*
  TODO inside block
*/
var FooTODOBar = 2
`

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func runFindMatches(t *testing.T, dir, pattern, matchIn string) []int {
	t.Helper()
	res, err := search.FindMatches(context.Background(), search.Options{
		Root:    dir,
		Expr:    `is_source && language == "go"`,
		Pattern: pattern,
		Workers: 1,
		MatchIn: matchIn,
	}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	lines := make([]int, len(res.Matches))
	for i, m := range res.Matches {
		lines[i] = m.Line
	}
	sort.Ints(lines)
	return lines
}

func TestFindMatches_MatchInAny_ReturnsEveryHit(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fixture.go", fixtureGo)
	got := runFindMatches(t, dir, "TODO", "any")
	want := []int{3, 4, 5, 7, 9}
	if !slices.Equal(got, want) {
		t.Errorf("match_in=any lines = %v, want %v", got, want)
	}
}

func TestFindMatches_MatchInComments_DropsCodeNoise(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fixture.go", fixtureGo)
	got := runFindMatches(t, dir, "TODO", "comments")
	// Lines 3 (line comment) + 7 (block-comment continuation) survive.
	// Lines 4 (string literal), 5 (trailing comment after code), 9
	// (identifier) all classify as code and drop.
	want := []int{3, 7}
	if !slices.Equal(got, want) {
		t.Errorf("match_in=comments lines = %v, want %v", got, want)
	}
}

func TestFindMatches_MatchInCode_DropsCommentLines(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fixture.go", fixtureGo)
	got := runFindMatches(t, dir, "TODO", "code")
	// Inverse of comments: lines 4 / 5 / 9 are code-classified.
	want := []int{4, 5, 9}
	if !slices.Equal(got, want) {
		t.Errorf("match_in=code lines = %v, want %v", got, want)
	}
}

func TestFindMatches_MatchInUnknown_Errors(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "fixture.go", fixtureGo)
	_, err := search.FindMatches(context.Background(), search.Options{
		Root:    dir,
		Pattern: "TODO",
		Workers: 1,
		MatchIn: "trailing-trivia",
	}, contentpkg.DefaultRegistry())
	if err == nil {
		t.Fatal("unknown match_in should error")
	}
}

// TestFindMatches_MatchInComments_NoOpOnMarkdown confirms the
// contract: non-source files (markdown, JSON, plain text) have no
// language syntax registered, so match_in is a no-op for them. A
// markdown file with TODO in body text still matches under
// match_in=comments — because we'd otherwise silently drop matches
// that the user almost certainly wants.
func TestFindMatches_MatchInComments_NoOpOnMarkdown(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "notes.md", "# Notes\n\n- TODO: write this\n")
	res, err := search.FindMatches(context.Background(), search.Options{
		Root:    dir,
		Pattern: "TODO",
		Workers: 1,
		MatchIn: "comments",
	}, contentpkg.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	if res.Count != 1 {
		t.Errorf("markdown with TODO should still match under match_in=comments; got %d", res.Count)
	}
}
