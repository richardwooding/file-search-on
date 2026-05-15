package search_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TestFindNearDuplicates_GroupsNearMatches verifies the canonical
// workflow: an original document + a near-duplicate (single-paragraph
// edit) + an unrelated document → exactly one group containing the
// original + near-duplicate, with the unrelated document excluded.
func TestFindNearDuplicates_GroupsNearMatches(t *testing.T) {
	tmp := t.TempDir()
	original := strings.Repeat("The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. ", 50)
	edited := strings.Replace(original, "lazy dog", "sleeping cat", 5)
	unrelated := strings.Repeat("Photosynthesis converts light into chemical energy. Plants use chlorophyll to capture photons. ", 50)

	write := func(name, body string) string {
		p := filepath.Join(tmp, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	origPath := write("doc-a.md", original)
	editPath := write("doc-b.md", edited)
	_ = write("doc-c.md", unrelated)

	res, err := search.FindNearDuplicates(context.Background(), search.Options{
		Root:                tmp,
		Expr:                "is_markdown",
		SimilarityThreshold: 0.85,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}

	if res.TotalFiles != 3 {
		t.Errorf("TotalFiles = %d, want 3", res.TotalFiles)
	}
	if res.FingerPrinted != 3 {
		t.Errorf("FingerPrinted = %d, want 3", res.FingerPrinted)
	}
	if res.GroupCount != 1 {
		t.Fatalf("GroupCount = %d, want 1 (orig+edit group; unrelated excluded)", res.GroupCount)
	}
	g := res.Groups[0]
	if g.Count != 2 {
		t.Errorf("group.Count = %d, want 2", g.Count)
	}
	got := map[string]bool{}
	for _, m := range g.Members {
		got[m.Path] = true
	}
	if !got[origPath] || !got[editPath] {
		t.Errorf("group members %v don't include both original (%q) and edit (%q)",
			got, origPath, editPath)
	}
	// Representative is the largest file — original (length 4350) is
	// larger than edited (length 4365 - 5*4 = 4345 if "lazy dog"→"sleeping cat", which is +4 chars * 5 = +20). Actually
	// "sleeping cat" (12 chars) is +4 longer than "lazy dog" (8 chars),
	// 5 replacements = +20 bytes → edited is LARGER.
	// Don't assert which one is representative; just that one of
	// them is, and Similarity for both is high.
	for _, m := range g.Members {
		if m.Similarity < 0.85 {
			t.Errorf("member %q similarity = %.3f, want >= 0.85", m.Path, m.Similarity)
		}
	}
}

// TestFindNearDuplicates_ThresholdGating verifies the threshold knob
// — at a very high threshold, the same near-duplicate pair drops out
// of the group.
func TestFindNearDuplicates_ThresholdGating(t *testing.T) {
	tmp := t.TempDir()
	original := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 50)
	// Replace ~10% of words; should be well below 0.95 similarity.
	edited := strings.Replace(original, "quick brown", "slow purple", 30)
	if err := os.WriteFile(filepath.Join(tmp, "a.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.md"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// At 0.95 the pair should NOT group.
	res, err := search.FindNearDuplicates(context.Background(), search.Options{
		Root:                tmp,
		Expr:                "is_markdown",
		SimilarityThreshold: 0.95,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if res.GroupCount != 0 {
		t.Errorf("strict threshold: GroupCount = %d, want 0", res.GroupCount)
	}
}

// TestFindNearDuplicates_SkipsBinary verifies that binary content
// types (no body extraction) don't surface as candidates — the tool
// is text-only by design.
func TestFindNearDuplicates_SkipsBinary(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.png"), []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := search.FindNearDuplicates(context.Background(), search.Options{
		Root: tmp,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if res.FingerPrinted != 0 {
		t.Errorf("FingerPrinted = %d, want 0 (PNG has no body)", res.FingerPrinted)
	}
}
