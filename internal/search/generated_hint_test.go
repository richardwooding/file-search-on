package search

import "testing"

func TestGeneratedHint(t *testing.T) {
	cases := []struct {
		name      string
		gen, tot  int
		wantEmpty bool
	}{
		{"dominated", 8, 10, false},          // 80% generated, ≥3 → hint
		{"exactly threshold", 4, 10, false},  // 40% ≥ 34% → hint
		{"below fraction", 2, 10, true},      // 20% < 34% → no hint
		{"too few absolute", 2, 2, true},     // 100% but only 2 (< minGeneratedForHint) → no hint
		{"none generated", 0, 12, true},      // → no hint
		{"empty result", 0, 0, true},         // → no hint
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := generatedHint(tc.gen, tc.tot)
			if (got == "") != tc.wantEmpty {
				t.Errorf("generatedHint(%d,%d) = %q; wantEmpty=%v", tc.gen, tc.tot, got, tc.wantEmpty)
			}
		})
	}
}

func TestCountGenerated(t *testing.T) {
	gen := map[string]bool{"/a.go": true, "/b.go": true}
	got := countGenerated([]string{"/a.go", "/b.go", "/c.go", "/a.go"}, gen)
	if got != 3 { // a, b, and a again
		t.Errorf("countGenerated = %d, want 3", got)
	}
}
