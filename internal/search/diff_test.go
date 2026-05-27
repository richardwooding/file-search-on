package search_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// twoTrees seeds a pair of trees with a known overlap:
//
//	A: shared.txt=SHARED, onlyA.txt=ONLY-A, drift.txt=A-VERSION
//	B: shared.txt=SHARED, onlyB.txt=ONLY-B, drift.txt=B-VERSION
//
// So by content hash: SHARED is in both; ONLY-A / A-VERSION only in A;
// ONLY-B / B-VERSION only in B. drift.txt shares a relative path but
// not content.
func twoTrees(t *testing.T) (treeA, treeB string) {
	t.Helper()
	treeA = t.TempDir()
	treeB = t.TempDir()
	mustWriteFile(t, filepath.Join(treeA, "shared.txt"), "SHARED CONTENT\n")
	mustWriteFile(t, filepath.Join(treeA, "onlyA.txt"), "ONLY IN A\n")
	mustWriteFile(t, filepath.Join(treeA, "drift.txt"), "A VERSION\n")
	mustWriteFile(t, filepath.Join(treeB, "shared.txt"), "SHARED CONTENT\n")
	mustWriteFile(t, filepath.Join(treeB, "onlyB.txt"), "ONLY IN B\n")
	mustWriteFile(t, filepath.Join(treeB, "drift.txt"), "B VERSION\n")
	return treeA, treeB
}

func basesA(recs []search.DiffRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, filepath.Base(r.PathA))
	}
	return out
}

func basesB(recs []search.DiffRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, filepath.Base(r.PathB))
	}
	return out
}

func contains(ss []string, want string) bool {
	return slices.Contains(ss, want)
}

func TestDiffTrees_AMinusB(t *testing.T) {
	a, b := twoTrees(t)
	res, err := search.DiffTrees(t.Context(), a, b, search.OpAMinusB, search.Options{}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	// onlyA.txt and drift.txt (A-VERSION not present in B by hash).
	if res.Count != 2 {
		t.Fatalf("a-minus-b count=%d want 2; %+v", res.Count, res.Records)
	}
	got := basesA(res.Records)
	if !contains(got, "onlyA.txt") || !contains(got, "drift.txt") {
		t.Errorf("a-minus-b paths = %v, want onlyA.txt + drift.txt", got)
	}
	for _, r := range res.Records {
		if r.Status != search.StatusOnlyInA {
			t.Errorf("status = %q, want only_in_a", r.Status)
		}
		if r.SHA256 == "" {
			t.Errorf("expected sha256 populated for %s", r.PathA)
		}
	}
}

func TestDiffTrees_BMinusA(t *testing.T) {
	a, b := twoTrees(t)
	res, err := search.DiffTrees(t.Context(), a, b, search.OpBMinusA, search.Options{}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	if res.Count != 2 {
		t.Fatalf("b-minus-a count=%d want 2; %+v", res.Count, res.Records)
	}
	got := basesB(res.Records)
	if !contains(got, "onlyB.txt") || !contains(got, "drift.txt") {
		t.Errorf("b-minus-a paths = %v, want onlyB.txt + drift.txt", got)
	}
	for _, r := range res.Records {
		if r.Status != search.StatusOnlyInB {
			t.Errorf("status = %q, want only_in_b", r.Status)
		}
	}
}

func TestDiffTrees_Intersect(t *testing.T) {
	a, b := twoTrees(t)
	res, err := search.DiffTrees(t.Context(), a, b, search.OpIntersect, search.Options{}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("intersect count=%d want 1; %+v", res.Count, res.Records)
	}
	r := res.Records[0]
	if r.Status != search.StatusBoth {
		t.Errorf("status = %q, want both", r.Status)
	}
	if filepath.Base(r.PathA) != "shared.txt" || filepath.Base(r.PathB) != "shared.txt" {
		t.Errorf("intersect record = %s / %s, want shared.txt on both sides", r.PathA, r.PathB)
	}
}

func TestDiffTrees_Union(t *testing.T) {
	a, b := twoTrees(t)
	res, err := search.DiffTrees(t.Context(), a, b, search.OpUnion, search.Options{}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	// 5 distinct content hashes: SHARED, ONLY-A, A-VERSION, ONLY-B, B-VERSION.
	if res.Count != 5 {
		t.Fatalf("union count=%d want 5; %+v", res.Count, res.Records)
	}
	var both, onlyA, onlyB int
	for _, r := range res.Records {
		switch r.Status {
		case search.StatusBoth:
			both++
		case search.StatusOnlyInA:
			onlyA++
		case search.StatusOnlyInB:
			onlyB++
		default:
			t.Errorf("unexpected status %q in union", r.Status)
		}
	}
	if both != 1 || onlyA != 2 || onlyB != 2 {
		t.Errorf("union breakdown both=%d onlyA=%d onlyB=%d, want 1/2/2", both, onlyA, onlyB)
	}
}

func TestDiffTrees_Mismatch(t *testing.T) {
	a, b := twoTrees(t)
	res, err := search.DiffTrees(t.Context(), a, b, search.OpMismatch, search.Options{}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	// Only drift.txt shares a relative path with differing content;
	// shared.txt matches both name AND content (not a mismatch).
	if res.Count != 1 {
		t.Fatalf("mismatch count=%d want 1; %+v", res.Count, res.Records)
	}
	r := res.Records[0]
	if r.Status != search.StatusNameMatch {
		t.Errorf("status = %q, want name_match_content_differs", r.Status)
	}
	if filepath.Base(r.PathA) != "drift.txt" || filepath.Base(r.PathB) != "drift.txt" {
		t.Errorf("mismatch record = %s / %s, want drift.txt on both sides", r.PathA, r.PathB)
	}
}

func TestDiffTrees_ExprFilter(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	// A markdown file unique to A, plus a json file unique to A.
	mustWriteFile(t, filepath.Join(a, "doc.md"), "# unique markdown\n")
	mustWriteFile(t, filepath.Join(a, "data.json"), `{"x":1}`)
	// B is empty-ish (one unrelated file).
	mustWriteFile(t, filepath.Join(b, "other.txt"), "unrelated\n")

	res, err := search.DiffTrees(t.Context(), a, b, search.OpAMinusB,
		search.Options{Expr: "is_markdown"}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	// Only the markdown file should be considered; the json file is
	// filtered out before hashing.
	if res.Count != 1 {
		t.Fatalf("expr-filtered count=%d want 1; %+v", res.Count, res.Records)
	}
	if filepath.Base(res.Records[0].PathA) != "doc.md" {
		t.Errorf("got %s, want doc.md", res.Records[0].PathA)
	}
}

func TestDiffTrees_InvalidOp(t *testing.T) {
	a, b := twoTrees(t)
	_, err := search.DiffTrees(t.Context(), a, b, search.DiffOp("nonsense"), search.Options{}, content.DefaultRegistry())
	if err == nil {
		t.Fatal("expected an error for an invalid diff op")
	}
}

func TestDiffTrees_IndexCacheReuse(t *testing.T) {
	a, b := twoTrees(t)
	idx := index.NewMemory()
	opts := search.Options{Index: idx}

	first, err := search.DiffTrees(t.Context(), a, b, search.OpAMinusB, opts, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("first DiffTrees: %v", err)
	}
	second, err := search.DiffTrees(t.Context(), a, b, search.OpAMinusB, opts, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("second DiffTrees: %v", err)
	}
	if first.Count != second.Count {
		t.Errorf("cached run differs: first=%d second=%d", first.Count, second.Count)
	}
}
