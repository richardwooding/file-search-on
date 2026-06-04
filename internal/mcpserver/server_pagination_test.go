package mcpserver

import (
	"fmt"
	"path/filepath"
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
