package bm25

import (
	"math"
	"testing"
)

func TestTokenize(t *testing.T) {
	got := Tokenize("The Quick, brown FOX! 123_456")
	want := []string{"the", "quick", "brown", "fox", "123", "456"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStatsFor_QueryScoped(t *testing.T) {
	s := StatsFor("alpha beta alpha gamma alpha", []string{"alpha", "delta"})
	if s.DocLen != 5 {
		t.Errorf("DocLen = %d, want 5", s.DocLen)
	}
	if s.TermFreqs["alpha"] != 3 {
		t.Errorf("tf[alpha] = %d, want 3", s.TermFreqs["alpha"])
	}
	if _, ok := s.TermFreqs["beta"]; ok {
		t.Error("tf should only hold query terms, found non-query 'beta'")
	}
	if _, ok := s.TermFreqs["delta"]; ok {
		t.Error("absent query term 'delta' should not be in tf map")
	}
}

// TestScore_RanksTermDenseDocHigher: among 3 docs, the one with more
// occurrences of the query term (length-normalised) scores highest.
func TestScore_RanksTermDenseDocHigher(t *testing.T) {
	q := []string{"transformer"}
	docs := []DocStats{
		StatsFor("the transformer architecture transformer transformer", q), // 3 hits / 5 tokens
		StatsFor("a passing mention of one transformer here somewhere ok", q), // 1 hit / 9 tokens
		StatsFor("no relevant terms at all in this document text here", q),     // 0 hits
	}
	sc := NewScorer(q, docs, DefaultK1, DefaultB)
	s0 := sc.Score(docs[0])
	s1 := sc.Score(docs[1])
	s2 := sc.Score(docs[2])
	if !(s0 > s1 && s1 > s2) {
		t.Errorf("expected s0 > s1 > s2, got %.4f, %.4f, %.4f", s0, s1, s2)
	}
	if s2 != 0 {
		t.Errorf("doc with no query terms should score 0, got %.4f", s2)
	}
}

// TestScore_UbiquitousTermLowIDF: a term in every doc has IDF≈0 so it
// barely contributes — BM25's whole point. Score stays non-negative.
func TestScore_UbiquitousTermLowIDF(t *testing.T) {
	q := []string{"the"}
	docs := []DocStats{
		StatsFor("the the the cat", q),
		StatsFor("the dog the", q),
		StatsFor("the bird the the", q),
	}
	sc := NewScorer(q, docs, DefaultK1, DefaultB)
	for i, d := range docs {
		s := sc.Score(d)
		if s < 0 {
			t.Errorf("doc %d scored negative (%.4f); IDF must stay non-negative", i, s)
		}
		if s > 0.5 {
			t.Errorf("doc %d score %.4f too high for a term in every document", i, s)
		}
	}
}

func TestNewScorer_Empty(t *testing.T) {
	sc := NewScorer([]string{"x"}, nil, DefaultK1, DefaultB)
	if got := sc.Score(DocStats{TermFreqs: map[string]int{"x": 1}, DocLen: 1}); got != 0 {
		t.Errorf("empty corpus must score 0, got %.4f", got)
	}
}

func TestReciprocalRankFusion(t *testing.T) {
	// a is #1 in both rankings, d is last in both; b and c swap middle
	// positions. RRF must rank a highest and d lowest.
	kw := []string{"a", "b", "c", "d"}
	vec := []string{"a", "c", "b", "d"}
	fused := ReciprocalRankFusion(DefaultRRFK, kw, vec)
	if !(fused["a"] > fused["b"] && fused["a"] > fused["c"]) {
		t.Errorf("RRF: a (#1 in both) must rank highest; a=%.6f b=%.6f c=%.6f", fused["a"], fused["b"], fused["c"])
	}
	if !(fused["d"] < fused["b"] && fused["d"] < fused["c"]) {
		t.Errorf("RRF: d (last in both) must rank lowest; d=%.6f b=%.6f c=%.6f", fused["d"], fused["b"], fused["c"])
	}
	// b and c are symmetric (2+3 vs 3+2) → equal fused scores.
	if math.Abs(fused["b"]-fused["c"]) > 1e-9 {
		t.Errorf("RRF: b and c are symmetric, want equal; got b=%.6f c=%.6f", fused["b"], fused["c"])
	}
}

func TestReciprocalRankFusion_MissingItem(t *testing.T) {
	// d appears only in the vector ranking — still gets that list's term.
	fused := ReciprocalRankFusion(DefaultRRFK, []string{"a"}, []string{"d", "a"})
	if fused["d"] == 0 {
		t.Error("item present in one ranking should still score")
	}
	if fused["a"] <= fused["d"] {
		t.Errorf("a (in both lists) should outscore d (one list); a=%.6f d=%.6f", fused["a"], fused["d"])
	}
}
