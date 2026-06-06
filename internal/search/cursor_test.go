package search

import (
	"fmt"
	"testing"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// collectPages walks every page via PaginateResults, returning the
// concatenated paths in page order and the number of pages it took. It
// fails the test on any pagination error or if paging doesn't terminate.
func collectPages(t *testing.T, build func() []Result, sortKey, order string, limit int) ([]string, int) {
	t.Helper()
	var paths []string
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 1000 {
			t.Fatalf("pagination did not terminate after 1000 pages")
		}
		// Rebuild the (unsorted) result set each page to model the
		// stateless re-walk the MCP handler performs.
		page, next, err := PaginateResults(build(), sortKey, order, cursor, limit)
		if err != nil {
			t.Fatalf("PaginateResults page %d: %v", pages, err)
		}
		for _, r := range page {
			paths = append(paths, r.Path)
		}
		if next == "" {
			break
		}
		if len(page) == 0 {
			t.Fatalf("page %d returned a next_cursor but no results (would loop forever)", pages)
		}
		cursor = next
	}
	return paths, pages
}

func resultsBySize() []Result {
	// Deliberately unsorted, with a size tie (b and d both 20) to
	// exercise the path tiebreaker.
	return []Result{
		{Path: "c.txt", Size: 30},
		{Path: "a.txt", Size: 10},
		{Path: "d.txt", Size: 20},
		{Path: "b.txt", Size: 20},
		{Path: "e.txt", Size: 40},
	}
}

func TestPaginate_CoversEveryItemOnceInOrder(t *testing.T) {
	paths, pages := collectPages(t, resultsBySize, "size", "asc", 2)
	want := []string{"a.txt", "b.txt", "d.txt", "c.txt", "e.txt"} // size asc, path tiebreak on the 20s
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("paged order = %v, want %v", paths, want)
	}
	if pages != 3 { // 2 + 2 + 1
		t.Errorf("pages = %d, want 3", pages)
	}
}

func TestPaginate_DescOrder(t *testing.T) {
	paths, _ := collectPages(t, resultsBySize, "size", "desc", 2)
	want := []string{"e.txt", "c.txt", "b.txt", "d.txt", "a.txt"} // size desc, path tiebreak still asc on the 20s
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("paged desc order = %v, want %v", paths, want)
	}
}

func TestPaginate_NoLimitReturnsAllNoCursor(t *testing.T) {
	page, next, err := PaginateResults(resultsBySize(), "size", "asc", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != "" {
		t.Errorf("next_cursor = %q, want empty (no limit → single page)", next)
	}
	if len(page) != 5 {
		t.Errorf("len(page) = %d, want 5", len(page))
	}
}

func TestPaginate_LastPageHasNoCursor(t *testing.T) {
	// limit == len means one full page and no more.
	_, next, err := PaginateResults(resultsBySize(), "size", "asc", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if next != "" {
		t.Errorf("next_cursor = %q, want empty when the page exhausts the set", next)
	}
}

func TestPaginate_EmptySortKeyOrdersByPath(t *testing.T) {
	paths, _ := collectPages(t, func() []Result {
		return []Result{{Path: "z.txt"}, {Path: "a.txt"}, {Path: "m.txt"}}
	}, "", "", 1)
	want := []string{"a.txt", "m.txt", "z.txt"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("empty-sort paged order = %v, want %v (by path)", paths, want)
	}
}

// TestPaginate_RobustToDeletedCursorItem confirms the keyset resume
// finds the first item strictly after the cursor position even when the
// exact file the cursor pointed at is gone on the next walk.
func TestPaginate_RobustToDeletedCursorItem(t *testing.T) {
	// Page 1 of size-asc: a(10), b(20). Cursor anchors on b(20).
	_, next, err := PaginateResults(resultsBySize(), "size", "asc", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if next == "" {
		t.Fatal("expected a next_cursor after page 1")
	}
	// Now b.txt is deleted before page 2's re-walk.
	withoutB := []Result{
		{Path: "c.txt", Size: 30},
		{Path: "a.txt", Size: 10},
		{Path: "d.txt", Size: 20},
		{Path: "e.txt", Size: 40},
	}
	page2, _, err := PaginateResults(withoutB, "size", "asc", next, 2)
	if err != nil {
		t.Fatalf("page 2 with deleted cursor item: %v", err)
	}
	// d(20) sorts after b(20) on the path tiebreak, so it must lead page 2.
	got := []string{}
	for _, r := range page2 {
		got = append(got, r.Path)
	}
	want := []string{"d.txt", "c.txt"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("page 2 = %v, want %v (resume past deleted cursor)", got, want)
	}
}

func TestPaginate_SortMismatchErrors(t *testing.T) {
	_, next, err := PaginateResults(resultsBySize(), "size", "asc", "", 2)
	if err != nil || next == "" {
		t.Fatalf("setup: err=%v next=%q", err, next)
	}
	if _, _, err := PaginateResults(resultsBySize(), "name", "asc", next, 2); err == nil {
		t.Error("expected an error when the cursor's sort key differs from the call's")
	}
	if _, _, err := PaginateResults(resultsBySize(), "size", "desc", next, 2); err == nil {
		t.Error("expected an error when the cursor's order differs from the call's")
	}
}

func TestPaginate_InvalidCursorErrors(t *testing.T) {
	if _, _, err := PaginateResults(resultsBySize(), "size", "asc", "!!!not-base64!!!", 2); err == nil {
		t.Error("expected an error for a malformed cursor token")
	}
}

// TestPaginate_ExtraScalarKey covers a per-family scalar pulled from
// FileAttributes.Extra (e.g. word_count) rather than a typed field.
func TestPaginate_ExtraScalarKey(t *testing.T) {
	build := func() []Result {
		return []Result{
			{Path: "a.md", Attrs: &celexpr.FileAttributes{Extra: map[string]any{"word_count": int64(300)}}},
			{Path: "b.md", Attrs: &celexpr.FileAttributes{Extra: map[string]any{"word_count": int64(100)}}},
			{Path: "c.md", Attrs: &celexpr.FileAttributes{Extra: map[string]any{"word_count": int64(200)}}},
		}
	}
	paths, _ := collectPages(t, build, "word_count", "asc", 1)
	want := []string{"b.md", "c.md", "a.md"} // 100, 200, 300
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("word_count paged order = %v, want %v", paths, want)
	}
}

// TestPaginate_MissingAttributeSortsLast mirrors compareByKey's rule:
// a result with no value for the sort key groups at the end.
func TestPaginate_MissingAttributeSortsLast(t *testing.T) {
	build := func() []Result {
		return []Result{
			{Path: "has.md", Attrs: &celexpr.FileAttributes{Extra: map[string]any{"iso": int64(800)}}},
			{Path: "missing.md", Attrs: &celexpr.FileAttributes{Extra: map[string]any{}}},
		}
	}
	paths, _ := collectPages(t, build, "iso", "asc", 1)
	want := []string{"has.md", "missing.md"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("missing-attr order = %v, want %v", paths, want)
	}
}

// --- PaginateGeneric (group-shaped tools, issue #336) ---

type bucket struct {
	name  string
	count int64
}

func bktKeyFn(b bucket) []any { return []any{b.count, b.name} }

func collectGenericPages(t *testing.T, build func() []bucket, orders []string, limit int) []string {
	t.Helper()
	var names []string
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 1000 {
			t.Fatal("generic pagination did not terminate")
		}
		page, next, err := PaginateGeneric(build(), bktKeyFn, orders, "test", cursor, limit)
		if err != nil {
			t.Fatalf("PaginateGeneric page %d: %v", pages, err)
		}
		for _, b := range page {
			names = append(names, b.name)
		}
		if next == "" {
			break
		}
		if len(page) == 0 {
			t.Fatal("non-empty next cursor with empty page")
		}
		cursor = next
	}
	return names
}

func buckets() []bucket {
	// count desc, name asc tiebreak on the two 5s.
	return []bucket{
		{"go", 5}, {"md", 2}, {"py", 5}, {"txt", 1}, {"json", 3},
	}
}

func TestPaginateGeneric_CountDescNameAsc(t *testing.T) {
	names := collectGenericPages(t, buckets, []string{"desc", "asc"}, 2)
	want := []string{"go", "py", "json", "md", "txt"} // 5,5 (go<py), 3, 2, 1
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Errorf("paged buckets = %v, want %v", names, want)
	}
}

func TestPaginateGeneric_NoLimitNoCursor(t *testing.T) {
	page, next, err := PaginateGeneric(buckets(), bktKeyFn, []string{"desc", "asc"}, "test", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if next != "" || len(page) != 5 {
		t.Errorf("want single full page no cursor; got len=%d next=%q", len(page), next)
	}
}

func TestPaginateGeneric_OrderMismatchErrors(t *testing.T) {
	_, next, err := PaginateGeneric(buckets(), bktKeyFn, []string{"desc", "asc"}, "test", "", 2)
	if err != nil || next == "" {
		t.Fatalf("setup: err=%v next=%q", err, next)
	}
	// Reuse a (desc,asc) cursor against (asc,asc), same scope → reject.
	if _, _, err := PaginateGeneric(buckets(), bktKeyFn, []string{"asc", "asc"}, "test", next, 2); err == nil {
		t.Error("expected an error when the cursor's ordering differs from the call's")
	}
}

// TestPaginateGeneric_ScopeMismatchErrors is the #347 regression: a
// cursor issued for one query dimension (scope) must be rejected when
// reused against a different dimension, rather than silently mis-paging.
func TestPaginateGeneric_ScopeMismatchErrors(t *testing.T) {
	_, next, err := PaginateGeneric(buckets(), bktKeyFn, []string{"desc", "asc"}, "stats:ext", "", 2)
	if err != nil || next == "" {
		t.Fatalf("setup: err=%v next=%q", err, next)
	}
	// Same ordering, DIFFERENT scope → reject.
	if _, _, err := PaginateGeneric(buckets(), bktKeyFn, []string{"desc", "asc"}, "stats:language", next, 2); err == nil {
		t.Error("expected an error when the cursor's scope differs from the call's (#347)")
	}
}

func TestPaginateGeneric_InvalidCursorErrors(t *testing.T) {
	if _, _, err := PaginateGeneric(buckets(), bktKeyFn, []string{"desc", "asc"}, "test", "@@bad@@", 2); err == nil {
		t.Error("expected an error for a malformed generic cursor")
	}
}

func TestPaginate_TimeKeyRoundTrips(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	build := func() []Result {
		return []Result{
			{Path: "new.md", Attrs: &celexpr.FileAttributes{ModTime: t0.Add(48 * time.Hour)}},
			{Path: "old.md", Attrs: &celexpr.FileAttributes{ModTime: t0}},
			{Path: "mid.md", Attrs: &celexpr.FileAttributes{ModTime: t0.Add(24 * time.Hour)}},
		}
	}
	paths, _ := collectPages(t, build, "mod_time", "desc", 1)
	want := []string{"new.md", "mid.md", "old.md"}
	if fmt.Sprint(paths) != fmt.Sprint(want) {
		t.Errorf("mod_time desc paged order = %v, want %v", paths, want)
	}
}
