package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// makeRankTestTree writes three text files of distinctly different
// sizes so a rank expression has something to sort by.
func makeRankTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "small.txt"), "abc")
	mustWriteFile(t, filepath.Join(root, "medium.txt"), string(make([]byte, 500)))
	mustWriteFile(t, filepath.Join(root, "large.txt"), string(make([]byte, 50000)))
	return root
}

// TestWalk_RankBySize verifies that --rank with a size expression
// orders results largest-first (default desc).
func TestWalk_RankBySize(t *testing.T) {
	root := makeRankTestTree(t)
	reg := content.DefaultRegistry()

	results, err := search.Walk(context.Background(), search.Options{
		Root:              root,
		Expr:              "is_text",
		RankExpr:          "size",
		IncludeAttributes: true,
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Default order is desc — largest first.
	wantPaths := []string{"large.txt", "medium.txt", "small.txt"}
	for i, r := range results {
		gotBase := filepath.Base(r.Path)
		if gotBase != wantPaths[i] {
			t.Errorf("position %d: got %s, want %s", i, gotBase, wantPaths[i])
		}
	}

	// Rank values should be populated and ordered desc.
	if results[0].Rank != 50000 {
		t.Errorf("large.txt rank = %v, want 50000", results[0].Rank)
	}
	if results[1].Rank != 500 {
		t.Errorf("medium.txt rank = %v, want 500", results[1].Rank)
	}
	if results[2].Rank != 3 {
		t.Errorf("small.txt rank = %v, want 3", results[2].Rank)
	}
}

// TestWalk_RankOrderAscFlipsResults verifies that --order asc with
// --rank set flips the default desc behaviour.
func TestWalk_RankOrderAscFlipsResults(t *testing.T) {
	root := makeRankTestTree(t)
	reg := content.DefaultRegistry()

	results, err := search.Walk(context.Background(), search.Options{
		Root:              root,
		Expr:              "is_text",
		RankExpr:          "size",
		Order:             "asc",
		IncludeAttributes: true,
	}, reg)
	if err != nil {
		t.Fatal(err)
	}

	wantPaths := []string{"small.txt", "medium.txt", "large.txt"}
	for i, r := range results {
		gotBase := filepath.Base(r.Path)
		if gotBase != wantPaths[i] {
			t.Errorf("position %d: got %s, want %s", i, gotBase, wantPaths[i])
		}
	}
}

// TestWalk_RankWinsOverSort verifies that when both --rank and
// --sort are set, --rank wins on the SORT KEY (the more expressive
// primitive). Note: --order still wins if explicitly set — passing
// --order asc with --rank gives smallest-first; the default-desc
// behaviour only kicks in when --order is omitted.
func TestWalk_RankWinsOverSort(t *testing.T) {
	root := makeRankTestTree(t)
	reg := content.DefaultRegistry()

	// Both rank by size AND sort by name. Rank should win → results
	// ordered by rank desc (default). Order is left unset so the
	// walker's default-to-desc behaviour applies.
	results, err := search.Walk(context.Background(), search.Options{
		Root:              root,
		Expr:              "is_text",
		Sort:              "name",
		RankExpr:          "size",
		IncludeAttributes: true,
	}, reg)
	if err != nil {
		t.Fatal(err)
	}

	if results[0].Rank == 0 {
		t.Fatal("Rank field not populated — rank expression didn't run")
	}
	if results[0].Rank != 50000 {
		t.Errorf("expected rank ordering: largest first, got %v at position 0", results[0].Rank)
	}

	// Order desc → large, medium, small by file size.
	wantPaths := []string{"large.txt", "medium.txt", "small.txt"}
	for i, r := range results {
		gotBase := filepath.Base(r.Path)
		if gotBase != wantPaths[i] {
			t.Errorf("position %d: got %s, want %s", i, gotBase, wantPaths[i])
		}
	}
}

// TestWalk_RankBooleanCoercion verifies the boolean-as-rank shortcut.
func TestWalk_RankBooleanCoercion(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "a.md"), "# markdown\n")
	mustWriteFile(t, filepath.Join(root, "b.txt"), "plain text")
	reg := content.DefaultRegistry()

	results, err := search.Walk(context.Background(), search.Options{
		Root:              root,
		Expr:              "is_text || is_markdown",
		RankExpr:          "is_markdown",
		IncludeAttributes: true,
	}, reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// Markdown should sort first (rank 1.0), text second (rank 0.0).
	if filepath.Base(results[0].Path) != "a.md" {
		t.Errorf("position 0 = %s, want a.md", filepath.Base(results[0].Path))
	}
	if results[0].Rank != 1.0 {
		t.Errorf("a.md rank = %v, want 1.0", results[0].Rank)
	}
	if results[1].Rank != 0.0 {
		t.Errorf("b.txt rank = %v, want 0.0", results[1].Rank)
	}
}

// TestWalk_RankCompileErrorSurfacesAtWalkEntry verifies that a
// malformed rank expression fails at Walk entry (not silently per-
// file), so the user gets a clear error.
func TestWalk_RankCompileErrorSurfacesAtWalkEntry(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "x.txt"), "hi")
	reg := content.DefaultRegistry()

	_, err := search.Walk(context.Background(), search.Options{
		Root:     root,
		Expr:     "true",
		RankExpr: "size +", // syntax error
	}, reg)
	if err == nil {
		t.Fatal("expected compile error, got nil")
	}
}

// mustWriteFile / mustMkdir helpers are shared with the other
// search_test files via walker_features_test.go.
