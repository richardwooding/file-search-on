package search

import (
	"context"
	"testing"
)

// TestGroupNearDuplicates_Cancellation verifies the O(n^2) grouping loop
// honours context cancellation — a cancelled ctx returns no groups
// instead of grinding through the full pairwise comparison set (the
// ctx-cancellation audit gap).
func TestGroupNearDuplicates_Cancellation(t *testing.T) {
	// Two identical fingerprints would normally union into one group.
	cands := []nearDupCandidate{
		{path: "a", size: 100, fingerprint: 0xABCD},
		{path: "b", size: 100, fingerprint: 0xABCD},
		{path: "c", size: 100, fingerprint: 0xABCD},
	}

	// Sanity: live ctx groups them.
	if got := groupNearDuplicates(context.Background(), cands, 0.85); len(got) != 1 {
		t.Fatalf("live ctx: got %d groups, want 1", len(got))
	}

	// Cancelled ctx: bail with no groups.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := groupNearDuplicates(ctx, cands, 0.85); got != nil {
		t.Errorf("cancelled ctx: got %d groups, want nil (bail on cancel)", len(got))
	}
}
