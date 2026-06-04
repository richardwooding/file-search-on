// Package fingerprint computes 64-bit Charikar SimHash fingerprints
// suitable for near-duplicate detection across text documents.
//
// SimHash is a locality-sensitive hash: documents whose tokens
// substantially overlap produce fingerprints whose XOR has few set
// bits (small Hamming distance). Similarity == 1 - distance/64, so:
//
//	distance <= 3   ≈ 95% similarity  (near-identical: whitespace / a word or two)
//	distance <= 9   ≈ 85% similarity  (minor edits / template fills — the default cut)
//	distance ~ 28+  ≈ 55% similarity  (unrelated prose, near the random baseline)
//
// (Shingling registers a localised edit as a few changed shingles
// rather than one changed token, so small edits sit slightly higher up
// the distance scale than a single-token hash would put them.)
//
// We tokenise by Unicode letters/digits (lowercased), drop tokens of
// length < 2, then group them into overlapping k-word SHINGLES
// (k = shingleSize). Each shingle is hashed via FNV-1a-64 and fed to
// Charikar's per-bit accumulator: +1 for every shingle whose hash has
// bit i set, -1 otherwise. The final bit i of the fingerprint is 1 iff
// the running sum at position i is positive. This implementation is
// pure stdlib — no third-party libs.
//
// Shingles rather than single words because single-token SimHash over
// natural-language prose is dominated by the high-frequency stopword
// distribution (the / and / of / to …), which is near-universal across
// all English text — so two unrelated novels score ~90% similar and
// cluster spuriously (issue #310). A document's k-word phrasing is far
// more distinctive: unrelated prose drops to ~0.55 (near the random
// baseline) while genuine near-duplicates stay high.
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

// shingleSize is the number of consecutive words per shingle. 3 gives
// the best separation between unrelated prose (~0.55) and genuine
// near-duplicates (≥0.75) on real-world text; smaller k leaves too much
// stopword overlap, larger k is more brittle to minor edits. Texts with
// fewer than shingleSize tokens fall back to single-token features so
// short strings and code snippets still fingerprint.
const shingleSize = 3

// Compute returns the 64-bit SimHash fingerprint of text. Empty input
// (or input with no usable tokens) returns 0, which is a legitimate
// fingerprint — callers can distinguish "no content to fingerprint" via
// a separate len(body) check.
func Compute(text string) uint64 {
	if text == "" {
		return 0
	}
	features := shingles(tokenize(text), shingleSize)
	if len(features) == 0 {
		return 0
	}
	var sums [64]int
	for _, feat := range features {
		h := fnv64(feat)
		for i := range 64 {
			if h&(1<<i) != 0 {
				sums[i]++
			} else {
				sums[i]--
			}
		}
	}
	var fp uint64
	for i := range 64 {
		if sums[i] > 0 {
			fp |= 1 << i
		}
	}
	return fp
}

// shingles groups tokens into overlapping k-word features joined by a
// space. With fewer than k tokens it returns the tokens themselves
// (single-word features) so short inputs still produce a fingerprint.
func shingles(tokens []string, k int) []string {
	if len(tokens) == 0 {
		return nil
	}
	if len(tokens) < k {
		return tokens
	}
	out := make([]string, 0, len(tokens)-k+1)
	for i := 0; i+k <= len(tokens); i++ {
		out = append(out, strings.Join(tokens[i:i+k], " "))
	}
	return out
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
