package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSearchTool_CursorPagination pages through a result set two at a
// time and asserts the union of pages covers every file exactly once,
// in a stable sorted order, with next_cursor clearing on the last page
// (issue #336).
func TestSearchTool_CursorPagination(t *testing.T) {
	dir := t.TempDir()
	names := []string{"a.md", "b.md", "c.md", "d.md", "e.md"}
	for _, n := range names {
		mustWrite(t, filepath.Join(dir, n), "# "+n+"\n\nbody\n")
	}

	ctx, cs := newSession(t)

	var got []string
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 100 {
			t.Fatal("pagination did not terminate")
		}
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name: "search",
			Arguments: SearchInput{
				Expr:   "is_markdown",
				Dir:    dir,
				SortBy: "name",
				Order:  "asc",
				Limit:  2,
				Cursor: cursor,
			},
		})
		if err != nil {
			t.Fatalf("page %d CallTool: %v", pages, err)
		}
		if res.GetError() != nil {
			t.Fatalf("page %d tool error: %v", pages, res.GetError())
		}
		var out SearchOutput
		mustDecodeStructured(t, res, &out)

		if out.Count > 2 {
			t.Fatalf("page %d returned %d matches, want <= limit 2", pages, out.Count)
		}
		for _, m := range out.Matches {
			got = append(got, filepath.Base(m.Path))
		}
		if out.NextCursor == "" {
			break
		}
		cursor = out.NextCursor
	}

	if pages != 3 { // 2 + 2 + 1
		t.Errorf("pages = %d, want 3", pages)
	}
	want := []string{"a.md", "b.md", "c.md", "d.md", "e.md"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("paged result = %v, want %v (every file once, in name order)", got, want)
	}
}

// TestSearchTool_CursorSortMismatchErrors confirms reusing a cursor with
// a different sort_by is rejected rather than silently returning garbage.
func TestSearchTool_CursorSortMismatchErrors(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.md", "b.md", "c.md"} {
		mustWrite(t, filepath.Join(dir, n), "# "+n+"\n\nbody\n")
	}
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir, SortBy: "name", Order: "asc", Limit: 1},
	})
	if err != nil {
		t.Fatalf("page1 CallTool: %v", err)
	}
	var out SearchOutput
	mustDecodeStructured(t, res, &out)
	if out.NextCursor == "" {
		t.Fatal("expected a next_cursor after page 1")
	}

	// Reuse the name-sorted cursor against a size sort → must error.
	res2, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: SearchInput{Expr: "is_markdown", Dir: dir, SortBy: "size", Order: "asc", Limit: 1, Cursor: out.NextCursor},
	})
	if err != nil {
		t.Fatalf("page2 CallTool transport: %v", err)
	}
	if !res2.IsError {
		t.Error("expected IsError=true when the cursor's sort differs from the call's sort_by")
	}
}

// TestFindMatchesTool_CursorPagination pages the (path, line)-ordered
// line-match list and asserts full, ordered, non-overlapping coverage.
func TestFindMatchesTool_CursorPagination(t *testing.T) {
	dir := t.TempDir()
	// 6 hits total: two files, three matching lines each.
	mustWrite(t, filepath.Join(dir, "a.txt"), "TODO one\nx\nTODO two\ny\nTODO three\n")
	mustWrite(t, filepath.Join(dir, "b.txt"), "TODO four\nz\nTODO five\nw\nTODO six\n")

	ctx, cs := newSession(t)

	var hits []string
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 50 {
			t.Fatal("pagination did not terminate")
		}
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "find_matches",
			Arguments: FindMatchesInput{Pattern: "TODO", Dir: dir, Limit: 2, Cursor: cursor},
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		var out FindMatchesOutput
		mustDecodeStructured(t, res, &out)
		if out.Count != 6 {
			t.Errorf("page %d: count=%d want 6 (total found, not page size)", pages, out.Count)
		}
		if len(out.Matches) > 2 {
			t.Fatalf("page %d returned %d matches, want <= 2", pages, len(out.Matches))
		}
		for _, m := range out.Matches {
			hits = append(hits, fmt.Sprintf("%s:%d", filepath.Base(m.Path), m.Line))
		}
		if out.NextCursor == "" {
			break
		}
		cursor = out.NextCursor
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
	want := []string{"a.txt:1", "a.txt:3", "a.txt:5", "b.txt:1", "b.txt:3", "b.txt:5"}
	if fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Errorf("paged hits = %v, want %v", hits, want)
	}
}

// TestStatsTool_CursorPagination pages the histogram buckets (count
// desc, name asc) and asserts each bucket appears once across pages.
func TestStatsTool_CursorPagination(t *testing.T) {
	dir := t.TempDir()
	// Distinct extensions with differing counts to exercise count-desc.
	mustWrite(t, filepath.Join(dir, "a.md"), "x")
	mustWrite(t, filepath.Join(dir, "b.md"), "x")
	mustWrite(t, filepath.Join(dir, "c.md"), "x")
	mustWrite(t, filepath.Join(dir, "a.json"), "{}")
	mustWrite(t, filepath.Join(dir, "b.json"), "{}")
	mustWrite(t, filepath.Join(dir, "a.txt"), "x")

	ctx, cs := newSession(t)

	var names []string
	cursor := ""
	pages := 0
	for {
		pages++
		if pages > 50 {
			t.Fatal("pagination did not terminate")
		}
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "stats",
			Arguments: StatsInput{Dir: dir, GroupBy: "ext", Limit: 1, Cursor: cursor},
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		var out StatsOutput
		mustDecodeStructured(t, res, &out)
		if out.TotalCount != 6 {
			t.Errorf("page %d: total_count=%d want 6 (whole tree, not page)", pages, out.TotalCount)
		}
		for _, b := range out.Groups {
			names = append(names, b.Name)
		}
		if out.NextCursor == "" {
			break
		}
		cursor = out.NextCursor
	}
	// .md (3) > .json (2) > .txt (1) — count desc.
	want := []string{".md", ".json", ".txt"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Errorf("paged buckets = %v, want %v", names, want)
	}
}

// TestFindNearDuplicatesTool_CursorPagination pages near-duplicate
// clusters and asserts every cluster is visited once.
func TestFindNearDuplicatesTool_CursorPagination(t *testing.T) {
	dir := t.TempDir()
	// Two distinct clusters of near-identical prose.
	base1 := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 60)
	base2 := strings.Repeat("Pack my box with five dozen liquor jugs today. ", 60)
	mustWrite(t, filepath.Join(dir, "c1a.md"), base1)
	mustWrite(t, filepath.Join(dir, "c1b.md"), strings.Replace(base1, "lazy", "sleepy", 1))
	mustWrite(t, filepath.Join(dir, "c2a.md"), base2)
	mustWrite(t, filepath.Join(dir, "c2b.md"), strings.Replace(base2, "today", "tonight", 1))

	ctx, cs := newSession(t)

	seen := map[string]bool{}
	cursor := ""
	pages := 0
	var total int64
	for {
		pages++
		if pages > 50 {
			t.Fatal("pagination did not terminate")
		}
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{
			Name:      "find_near_duplicates",
			Arguments: FindNearDuplicatesInput{Dir: dir, Threshold: 0.8, GroupLimit: 1, Cursor: cursor},
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		var out FindNearDuplicatesOutput
		mustDecodeStructured(t, res, &out)
		total = out.GroupCount
		if len(out.Groups) > 1 {
			t.Fatalf("page %d returned %d groups, want <= group_limit 1", pages, len(out.Groups))
		}
		for _, g := range out.Groups {
			if seen[g.Representative] {
				t.Errorf("group %s returned on more than one page", g.Representative)
			}
			seen[g.Representative] = true
		}
		if out.NextCursor == "" {
			break
		}
		cursor = out.NextCursor
	}
	if int64(len(seen)) != total || total == 0 {
		t.Errorf("paged %d unique groups, group_count=%d (want equal, non-zero)", len(seen), total)
	}
}

