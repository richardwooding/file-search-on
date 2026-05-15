// Package fingerprint computes 64-bit Charikar SimHash fingerprints
// suitable for near-duplicate detection across text documents.
//
// SimHash is a locality-sensitive hash: documents whose tokens
// substantially overlap produce fingerprints whose XOR has few set
// bits (small Hamming distance). Two thresholds matter in practice:
//
//	distance <= 3   ≈ 95% similarity  (typo / whitespace edits)
//	distance <= 9   ≈ 85% similarity  (minor edits / template fills)
//	distance <= 16  ≈ 75% similarity  (significant overlap, different docs)
//
// We tokenise by Unicode letters/digits (lowercased), drop tokens
// of length < 2, hash each via FNV-1a-64, and apply Charikar's
// per-bit accumulator: +1 for every token whose hash has bit i set,
// -1 otherwise. The final bit i of the fingerprint is 1 iff the
// running sum at position i is positive. This implementation is
// pure stdlib — no third-party libs.
//
// Use this package via Compute on the extracted body text of each
// candidate, then pairwise compare via Distance or Similarity.
package fingerprint

import (
	"hash/fnv"
	"math/bits"
	"strings"
	"unicode"
)

// minTokenLen filters out single-character tokens (the / a / I).
// Two-character tokens still carry signal ("go", "ml", "ai") but
// single chars are pure noise.
const minTokenLen = 2

// Compute returns the 64-bit SimHash fingerprint of text. Empty
// input (or input with fewer than 2 distinct tokens) returns 0,
// which is a legitimate fingerprint — callers can distinguish "no
// content to fingerprint" via a separate len(body) check.
func Compute(text string) uint64 {
	if text == "" {
		return 0
	}
	var sums [64]int
	tokenCount := 0
	for _, tok := range tokenize(text) {
		h := fnv64(tok)
		for i := range 64 {
			if h&(1<<i) != 0 {
				sums[i]++
			} else {
				sums[i]--
			}
		}
		tokenCount++
	}
	if tokenCount == 0 {
		return 0
	}
	var fp uint64
	for i := range 64 {
		if sums[i] > 0 {
			fp |= 1 << i
		}
	}
	return fp
}

// Distance returns the Hamming distance between two SimHash
// fingerprints — the number of bit positions where they differ.
// Range is 0 (identical) to 64 (maximally different).
func Distance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// Similarity returns the SimHash similarity score in [0, 1]:
//
//	1.0  → fingerprints are identical
//	0.5  → uncorrelated (random fingerprints average here)
//	0.0  → fingerprints differ in every bit
//
// Threshold 0.85 (the issue's default) corresponds to Hamming
// distance ≤ 9.
func Similarity(a, b uint64) float64 {
	return 1.0 - float64(Distance(a, b))/64.0
}

// tokenize splits text into lowercase word-shaped tokens (Unicode
// letters and digits, len >= minTokenLen). Punctuation and
// whitespace separate tokens; numeric-only tokens are kept (they
// carry signal for source code, version strings, etc.).
func tokenize(text string) []string {
	out := make([]string, 0, len(text)/8)
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= minTokenLen {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}

func fnv64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
