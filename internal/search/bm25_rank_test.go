package search_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestWalk_KeywordRanking walks a real tree with a keyword query and
// asserts BM25 ranks the term-dense document first and populates the
// bm25 score (issue #335).
func TestWalk_KeywordRanking(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "dense.md"),
		"# transformer\n\n"+strings.Repeat("transformer attention transformer ", 20))
	mustWriteFile(t, filepath.Join(root, "mention.md"),
		"# notes\n\nthis mentions a transformer once and then moves on to other topics entirely\n")
	mustWriteFile(t, filepath.Join(root, "unrelated.md"),
		"# cooking\n\n"+strings.Repeat("recipe ingredients oven bake ", 20))

	results, err := search.Walk(context.Background(), search.Options{
		Root:         root,
		Expr:         "is_markdown",
		KeywordQuery: "transformer attention",
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 markdown results, got %d", len(results))
	}
	if filepath.Base(results[0].Path) != "dense.md" {
		t.Errorf("expected dense.md ranked first, got %s", filepath.Base(results[0].Path))
	}
	if results[0].Attrs == nil || results[0].Attrs.BM25 <= 0 {
		t.Errorf("dense.md should have bm25 > 0, got %+v", results[0].Attrs)
	}
	// The unrelated doc shares no query terms → bm25 0, ranked last.
	last := results[len(results)-1]
	if filepath.Base(last.Path) != "unrelated.md" {
		t.Errorf("expected unrelated.md last, got %s", filepath.Base(last.Path))
	}
	if last.Attrs != nil && last.Attrs.BM25 != 0 {
		t.Errorf("unrelated.md should score bm25 0, got %f", last.Attrs.BM25)
	}
}

// synthResult builds a Result carrying the BM25 carrier data + a
// similarity, as if produced by a walk — lets us exercise FinalizeBM25
// without an embedding server.
func synthResult(path string, alphaTF, docLen int, sim float64) search.Result {
	tf := map[string]int{}
	if alphaTF > 0 {
		tf["alpha"] = alphaTF
	}
	return search.Result{
		Path: path,
		Attrs: &celexpr.FileAttributes{
			BM25TermFreqs: tf,
			BM25DocLen:    docLen,
			Similarity:    sim,
		},
	}
}

// TestFinalizeBM25_HybridRRF: a doc strong in BOTH keyword and semantic
// rankings must beat one weak in both, under reciprocal-rank fusion.
func TestFinalizeBM25_HybridRRF(t *testing.T) {
	results := []search.Result{
		synthResult("both.md", 5, 10, 0.9),
		synthResult("simonly.md", 0, 10, 0.9),
		synthResult("kwonly.md", 5, 10, 0.1),
		synthResult("neither.md", 0, 10, 0.1),
	}
	if err := search.FinalizeBM25(results, search.Options{KeywordQuery: "alpha", Hybrid: true}); err != nil {
		t.Fatalf("FinalizeBM25: %v", err)
	}
	rank := map[string]float64{}
	for _, r := range results {
		rank[filepath.Base(r.Path)] = r.Rank
	}
	if !(rank["both.md"] > rank["simonly.md"] && rank["both.md"] > rank["kwonly.md"]) {
		t.Errorf("hybrid: both.md should win; ranks=%v", rank)
	}
	if !(rank["neither.md"] < rank["simonly.md"] && rank["neither.md"] < rank["kwonly.md"]) {
		t.Errorf("hybrid: neither.md should lose; ranks=%v", rank)
	}
}

// TestFinalizeBM25_RankExpr evaluates a rank expression referencing the
// bm25 CEL variable and confirms it drives the score.
func TestFinalizeBM25_RankExpr(t *testing.T) {
	results := []search.Result{
		synthResult("hi.md", 6, 12, 0),
		synthResult("lo.md", 1, 12, 0),
	}
	if err := search.FinalizeBM25(results, search.Options{KeywordQuery: "alpha", RankExpr: "bm25 * 2.0"}); err != nil {
		t.Fatalf("FinalizeBM25: %v", err)
	}
	var hi, lo search.Result
	for _, r := range results {
		switch filepath.Base(r.Path) {
		case "hi.md":
			hi = r
		case "lo.md":
			lo = r
		}
	}
	if hi.Attrs.BM25 <= lo.Attrs.BM25 {
		t.Errorf("hi.md should have higher bm25; hi=%f lo=%f", hi.Attrs.BM25, lo.Attrs.BM25)
	}
	// rank == bm25 * 2.0
	if hi.Rank <= lo.Rank {
		t.Errorf("rank should track bm25; hi.Rank=%f lo.Rank=%f", hi.Rank, lo.Rank)
	}
	if hi.Rank != hi.Attrs.BM25*2.0 {
		t.Errorf("rank expr 'bm25*2.0' not applied: rank=%f bm25=%f", hi.Rank, hi.Attrs.BM25)
	}
}

// TestFinalizeBM25_Empty is a no-op when no keyword query is set.
func TestFinalizeBM25_NoQuery(t *testing.T) {
	results := []search.Result{synthResult("a.md", 3, 10, 0)}
	if err := search.FinalizeBM25(results, search.Options{}); err != nil {
		t.Fatalf("FinalizeBM25: %v", err)
	}
	if results[0].Attrs.BM25 != 0 || results[0].Rank != 0 {
		t.Errorf("no keyword query should leave bm25/rank at 0; got %+v", results[0])
	}
}
