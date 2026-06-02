package search

import (
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// TestSortAndLimit_GitCommitCount is the regression guard for the
// gap that surfaced during the v0.74.1 dogfood: sort_by="git_commit_count"
// silently no-op'd because the extraScalar fallback only sees the
// Extra map, but git_commit_count is a typed FileAttributes field.
func TestSortAndLimit_GitCommitCount(t *testing.T) {
	results := []Result{
		{Path: "a", Attrs: &celexpr.FileAttributes{GitCommitCount: 3}},
		{Path: "b", Attrs: &celexpr.FileAttributes{GitCommitCount: 17}},
		{Path: "c", Attrs: &celexpr.FileAttributes{GitCommitCount: 8}},
	}
	sorted := SortAndLimit(results, Options{Sort: "git_commit_count", Order: "desc"})
	wantOrder := []string{"b", "c", "a"} // 17 > 8 > 3
	for i, want := range wantOrder {
		if sorted[i].Path != want {
			t.Errorf("position %d: got %q, want %q (full order: %+v)", i, sorted[i].Path, want, paths(sorted))
		}
	}
}

func TestSortAndLimit_GitLastCommitTime(t *testing.T) {
	t1 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	results := []Result{
		{Path: "a", Attrs: &celexpr.FileAttributes{GitLastCommitTime: t1}},
		{Path: "b", Attrs: &celexpr.FileAttributes{GitLastCommitTime: t2}},
		{Path: "c", Attrs: &celexpr.FileAttributes{GitLastCommitTime: t3}},
	}
	sorted := SortAndLimit(results, Options{Sort: "git_last_commit_time", Order: "desc"})
	wantOrder := []string{"b", "a", "c"} // jun > may > apr
	for i, want := range wantOrder {
		if sorted[i].Path != want {
			t.Errorf("position %d: got %q, want %q (full order: %+v)", i, sorted[i].Path, want, paths(sorted))
		}
	}
}

func TestSortAndLimit_GitFirstSeen(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	results := []Result{
		{Path: "older", Attrs: &celexpr.FileAttributes{GitFirstSeen: t1}},
		{Path: "newer", Attrs: &celexpr.FileAttributes{GitFirstSeen: t2}},
	}
	asc := SortAndLimit(results, Options{Sort: "git_first_seen", Order: "asc"})
	if asc[0].Path != "older" || asc[1].Path != "newer" {
		t.Errorf("asc order = %v, want [older newer]", paths(asc))
	}
}

func paths(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Path
	}
	return out
}
