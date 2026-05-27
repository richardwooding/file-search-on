package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// seedDiffTrees writes the same A/B fixture the search-package diff
// tests use and returns the two roots.
func seedDiffTrees(t *testing.T) (treeA, treeB string) {
	t.Helper()
	treeA = t.TempDir()
	treeB = t.TempDir()
	mustWrite(t, filepath.Join(treeA, "shared.txt"), "SHARED CONTENT\n")
	mustWrite(t, filepath.Join(treeA, "onlyA.txt"), "ONLY IN A\n")
	mustWrite(t, filepath.Join(treeA, "drift.txt"), "A VERSION\n")
	mustWrite(t, filepath.Join(treeB, "shared.txt"), "SHARED CONTENT\n")
	mustWrite(t, filepath.Join(treeB, "onlyB.txt"), "ONLY IN B\n")
	mustWrite(t, filepath.Join(treeB, "drift.txt"), "B VERSION\n")
	return treeA, treeB
}

func TestDiffTreesTool_AMinusB(t *testing.T) {
	a, b := seedDiffTrees(t)
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "diff_trees",
		Arguments: DiffTreesInput{TreeA: a, TreeB: b, Op: "a-minus-b"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.GetError() != nil {
		t.Fatalf("tool returned error: %v", res.GetError())
	}

	var out DiffTreesOutput
	mustDecodeStructured(t, res, &out)

	// onlyA.txt + drift.txt (A VERSION absent from B by hash).
	if out.Count != 2 {
		t.Fatalf("a-minus-b count=%d want 2; %+v", out.Count, out.Records)
	}
	for _, r := range out.Records {
		if r.Status != "only_in_a" {
			t.Errorf("status = %q, want only_in_a", r.Status)
		}
		if r.PathA == "" || r.SHA256 == "" {
			t.Errorf("expected path_a + sha256 populated, got %+v", r)
		}
	}
}

func TestDiffTreesTool_DefaultOpAndMismatch(t *testing.T) {
	a, b := seedDiffTrees(t)
	ctx, cs := newSession(t)

	// Omitting op defaults to a-minus-b.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "diff_trees",
		Arguments: DiffTreesInput{TreeA: a, TreeB: b},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var out DiffTreesOutput
	mustDecodeStructured(t, res, &out)
	if out.Op != "a-minus-b" {
		t.Errorf("default op = %q, want a-minus-b", out.Op)
	}

	// mismatch: only drift.txt differs by content at the same rel path.
	res, err = cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "diff_trees",
		Arguments: DiffTreesInput{TreeA: a, TreeB: b, Op: "mismatch"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	var mm DiffTreesOutput
	mustDecodeStructured(t, res, &mm)
	if mm.Count != 1 {
		t.Fatalf("mismatch count=%d want 1; %+v", mm.Count, mm.Records)
	}
	if mm.Records[0].Status != "name_match_content_differs" {
		t.Errorf("status = %q, want name_match_content_differs", mm.Records[0].Status)
	}
	if filepath.Base(mm.Records[0].PathA) != "drift.txt" {
		t.Errorf("got %s, want drift.txt", mm.Records[0].PathA)
	}
}

func TestDiffTreesTool_InvalidOp(t *testing.T) {
	a, b := seedDiffTrees(t)
	ctx, cs := newSession(t)

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "diff_trees",
		Arguments: DiffTreesInput{TreeA: a, TreeB: b, Op: "nonsense"},
	})
	if err != nil {
		t.Fatalf("CallTool transport error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError=true for an invalid op")
	}
}
