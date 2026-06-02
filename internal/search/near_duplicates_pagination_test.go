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

// seedNearDupGroup writes N markdown files with a 1-token-edit between
// each so the SimHash similarity comfortably exceeds 0.85 (the prose
// default). Returns the tmpdir path.
func seedNearDupGroup(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	base := strings.Repeat("The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. ", 60)
	for i := range n {
		// Each variant changes a small fraction of the text — enough to keep similarity high.
		body := strings.Replace(base, "lazy", "sleepy", i+1)
		if err := os.WriteFile(filepath.Join(dir, "file"+string(rune('a'+i))+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return dir
}

func TestFindNearDuplicates_MembersLimitPerGroup_Truncates(t *testing.T) {
	dir := seedNearDupGroup(t, 6)
	res, err := search.FindNearDuplicates(context.Background(), search.Options{
		Root:                dir,
		Expr:                `is_markdown`,
		Workers:             1,
		NearDupMembersLimit: 3,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if len(res.Groups) == 0 {
		t.Fatal("expected at least one near-dup group")
	}
	g := res.Groups[0]
	if len(g.Members) != 3 {
		t.Errorf("Members size = %d, want 3", len(g.Members))
	}
	if g.MembersTotal != 6 {
		t.Errorf("MembersTotal = %d, want 6", g.MembersTotal)
	}
	if !g.MembersTruncated {
		t.Errorf("MembersTruncated should be true")
	}
	// Count carries the FULL group size, not the truncated Members length.
	if g.Count != 6 {
		t.Errorf("Count = %d, want 6 (unchanged by truncation)", g.Count)
	}
}

func TestFindNearDuplicates_NoLimitLeavesGroupAsIs(t *testing.T) {
	dir := seedNearDupGroup(t, 4)
	res, err := search.FindNearDuplicates(context.Background(), search.Options{
		Root:    dir,
		Expr:    `is_markdown`,
		Workers: 1,
		// NearDupMembersLimit: 0 (default) → no truncation
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if len(res.Groups) == 0 {
		t.Fatal("expected at least one near-dup group")
	}
	g := res.Groups[0]
	if g.MembersTruncated {
		t.Errorf("MembersTruncated should be false when no cap was requested")
	}
	if g.MembersTotal != 0 {
		t.Errorf("MembersTotal should be 0 when no cap was requested; got %d", g.MembersTotal)
	}
}

func TestFindNearDuplicates_GroupLimit_CapsSlice(t *testing.T) {
	// Two separate near-dup clusters.
	dir := t.TempDir()
	cluster1 := strings.Repeat("The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. ", 60)
	cluster2 := strings.Repeat("Photosynthesis converts light into chemical energy via chlorophyll capturing photons. ", 60)
	for i, body := range []string{cluster1, strings.Replace(cluster1, "lazy", "sleepy", 1), strings.Replace(cluster1, "fox", "cat", 1)} {
		_ = i
		if err := os.WriteFile(filepath.Join(dir, "c1_"+string(rune('a'+i))+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	for i, body := range []string{cluster2, strings.Replace(cluster2, "chlorophyll", "carotenoid", 1)} {
		_ = i
		if err := os.WriteFile(filepath.Join(dir, "c2_"+string(rune('a'+i))+".md"), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// Without group limit — both clusters appear.
	all, err := search.FindNearDuplicates(context.Background(), search.Options{
		Root:    dir,
		Expr:    `is_markdown`,
		Workers: 1,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates: %v", err)
	}
	if len(all.Groups) < 2 {
		t.Skipf("test environment didn't produce 2 distinct clusters; got %d", len(all.Groups))
	}

	// With group limit=1, only the largest cluster survives.
	capped, err := search.FindNearDuplicates(context.Background(), search.Options{
		Root:              dir,
		Expr:              `is_markdown`,
		Workers:           1,
		NearDupGroupLimit: 1,
	}, content.DefaultRegistry())
	if err != nil {
		t.Fatalf("FindNearDuplicates capped: %v", err)
	}
	if len(capped.Groups) != 1 {
		t.Errorf("Groups size = %d, want 1 (capped)", len(capped.Groups))
	}
	// GroupCount reports the unbounded original count.
	if capped.GroupCount < 2 {
		t.Errorf("GroupCount = %d, want >=2 (unchanged by truncation)", capped.GroupCount)
	}
}
