package fingerprint

import (
	"strings"
	"testing"
)

// TestCompute_Identical verifies that the same text always produces
// the same fingerprint — the canonical "deterministic hash"
// property.
func TestCompute_Identical(t *testing.T) {
	body := "The quick brown fox jumps over the lazy dog."
	a := Compute(body)
	b := Compute(body)
	if a != b {
		t.Errorf("Compute is non-deterministic: %d != %d", a, b)
	}
	if a == 0 {
		t.Errorf("Compute(%q) = 0, want non-zero", body)
	}
}

// TestCompute_Empty verifies the zero-input contract.
func TestCompute_Empty(t *testing.T) {
	if fp := Compute(""); fp != 0 {
		t.Errorf("Compute(\"\") = %d, want 0", fp)
	}
	if fp := Compute(";.!"); fp != 0 {
		t.Errorf("Compute punctuation-only = %d, want 0 (no tokens)", fp)
	}
}

// TestSimilarity_NearDuplicates verifies that small edits produce
// fingerprints with high similarity (the locality-sensitive
// property — the whole point of SimHash).
func TestSimilarity_NearDuplicates(t *testing.T) {
	// A paragraph of varied prose (most shingles distinct), as a real
	// document is — not pathologically repetitive. A single-word edit
	// touches only the few shingles spanning that word, so the
	// fingerprints stay near-identical.
	original := "The expedition reached the northern ridge at dawn, where the surveyor " +
		"mapped a hidden valley threaded by a slow winding river. Provisions ran low " +
		"and morale was uneven, yet the guide insisted the pass ahead would shelter them " +
		"from the coming storm during the long descent toward the distant rocky coast."
	edited := strings.Replace(original, "surveyor", "cartographer", 1)
	a := Compute(original)
	b := Compute(edited)
	sim := Similarity(a, b)
	if sim < 0.85 {
		t.Errorf("similarity = %.3f, want >= 0.85 for single-word edit in varied prose", sim)
	}
}

// TestSimilarity_Unrelated verifies that completely different text
// produces fingerprints with similarity near 0.5 (the random
// baseline — uncorrelated documents share roughly half their bits
// by chance).
func TestSimilarity_Unrelated(t *testing.T) {
	a := Compute("The Go programming language was developed at Google and announced publicly in November 2009.")
	b := Compute("Photosynthesis converts light energy into chemical energy stored in glucose molecules.")
	sim := Similarity(a, b)
	if sim > 0.7 {
		t.Errorf("similarity = %.3f, want <= 0.7 for unrelated docs", sim)
	}
	if sim < 0.3 {
		t.Errorf("similarity = %.3f, want >= 0.3 (random baseline ~0.5)", sim)
	}
}

// TestSimilarity_SameWordsDifferentPhrasing is the regression for issue
// #310. Single-token SimHash is a bag-of-words hash: two documents with
// the same word multiset but different phrasing produce (near-)identical
// fingerprints, so unrelated prose that merely shares English's
// near-universal word distribution clusters spuriously. The shingled
// fingerprint keys on k-word sequences, so different phrasings of the
// same vocabulary are correctly distinguished.
//
// Both documents below use the EXACT same multiset of words (docB is a
// rotation of docA's word stream), so a bag-of-words hash scores them
// ~1.0; the shingled hash must score them well under the 0.85 default.
func TestSimilarity_SameWordsDifferentPhrasing(t *testing.T) {
	words := strings.Fields("alpha bravo charlie delta echo foxtrot golf hotel india juliet " +
		"kilo lima mike november oscar papa quebec romeo sierra tango uniform victor whiskey xray")
	reversed := make([]string, len(words))
	for i, w := range words {
		reversed[len(words)-1-i] = w
	}
	// Same word multiset, different adjacencies (forward vs reversed),
	// bulked up so the fingerprint is stable. A bag-of-words hash can't
	// tell these apart; a shingled hash must.
	a := strings.Repeat(strings.Join(words, " ")+" ", 30)
	b := strings.Repeat(strings.Join(reversed, " ")+" ", 30)
	sim := Similarity(Compute(a), Compute(b))
	if sim >= 0.85 {
		t.Errorf("similarity = %.3f — same words in different order must NOT be near-duplicates; a bag-of-words hash would score ~1.0 (#310)", sim)
	}
}

// TestDistance_Bounds verifies Hamming distance stays in [0, 64].
func TestDistance_Bounds(t *testing.T) {
	cases := []struct {
		a, b uint64
		want int
	}{
		{0, 0, 0},
		{1, 0, 1},
		{0xFFFFFFFFFFFFFFFF, 0, 64},
		{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0},
		{0xF0F0F0F0F0F0F0F0, 0x0F0F0F0F0F0F0F0F, 64},
	}
	for _, c := range cases {
		if got := Distance(c.a, c.b); got != c.want {
			t.Errorf("Distance(%x, %x) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestSimilarity_Bounds verifies similarity stays in [0, 1].
func TestSimilarity_Bounds(t *testing.T) {
	cases := []struct {
		a, b uint64
		want float64
	}{
		{0, 0, 1.0},                                  // identical
		{0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 1.0}, // identical
		{0xFFFFFFFFFFFFFFFF, 0, 0.0},                 // opposite
	}
	for _, c := range cases {
		if got := Similarity(c.a, c.b); got != c.want {
			t.Errorf("Similarity(%x, %x) = %f, want %f", c.a, c.b, got, c.want)
		}
	}
}

// TestTokenize_Coverage exercises the tokenizer's edge cases.
func TestTokenize_Coverage(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"Hello, World!", []string{"hello", "world"}},
		{"a b cd", []string{"cd"}}, // single chars filtered
		{"foo123 bar456", []string{"foo123", "bar456"}}, // alnum kept
		{"", []string{}},
		{"...", []string{}},
		{"Über naïve résumé", []string{"über", "naïve", "résumé"}}, // Unicode letters
	}
	for _, c := range cases {
		got := tokenize(c.in)
		if !sliceEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
