// Package bm25 implements Okapi BM25 keyword relevance scoring plus
// reciprocal-rank fusion, the keyword half of hybrid keyword+semantic
// search (issue #335).
//
// BM25 ranks a document d against a query Q as:
//
//	score(d, Q) = Σ_{t∈Q} IDF(t) · ( f(t,d)·(k1+1) ) / ( f(t,d) + k1·(1 − b + b·|d|/avgdl) )
//
// where f(t,d) is the term frequency of t in d, |d| is the document
// length in tokens, avgdl is the mean document length over the corpus,
// and IDF(t) = ln( 1 + (N − n(t) + 0.5)/(n(t) + 0.5) ) with N documents
// and n(t) documents containing t. The IDF form is the BM25+ variant
// that stays non-negative, so a term appearing in every document scores
// ~0 rather than going negative.
//
// The corpus here is the candidate set being ranked (issue #335's
// "candidate set" decision): IDF and avgdl are computed over exactly the
// files that survived the CEL pre-filter, so relevance is relative to
// the result set the caller is sorting.
package bm25

import (
	"math"
	"strings"
	"unicode"
)

// Default Okapi BM25 free parameters. k1 controls term-frequency
// saturation (how quickly extra occurrences stop helping); b controls
// length normalisation (0 = none, 1 = full). These are the standard
// textbook defaults and work well across mixed corpora.
const (
	DefaultK1 = 1.5
	DefaultB  = 0.75
)

// Tokenize splits text into lowercase word-shaped tokens — maximal runs
// of Unicode letters and digits, everything else a separator. The same
// tokenizer is applied to both queries and documents so they share a
// vocabulary. Mirrors internal/fingerprint's tokenizer so keyword and
// near-duplicate views agree on word boundaries.
func Tokenize(text string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
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

// DocStats is the per-document data BM25 needs: the term frequency of
// each QUERY term in the document, plus the document's total token
// length. It's query-scoped (TermFreqs only holds query terms) so it
// stays tiny regardless of body size — the walker captures it during
// body extraction.
type DocStats struct {
	TermFreqs map[string]int
	DocLen    int
}

// StatsFor builds the DocStats for a body given the query terms. It
// tokenizes the body once: counting the total length and tallying only
// the frequencies of terms that appear in queryTerms.
func StatsFor(body string, queryTerms []string) DocStats {
	want := make(map[string]struct{}, len(queryTerms))
	for _, t := range queryTerms {
		want[t] = struct{}{}
	}
	tf := make(map[string]int, len(want))
	n := 0
	for _, tok := range Tokenize(body) {
		n++
		if _, ok := want[tok]; ok {
			tf[tok]++
		}
	}
	return DocStats{TermFreqs: tf, DocLen: n}
}

// Scorer holds the corpus statistics (IDF per query term, average doc
// length) needed to score any document in the corpus. Build it once per
// candidate set with NewScorer, then call Score per document.
type Scorer struct {
	idf    map[string]float64
	avgdl  float64
	k1, b  float64
	hasDoc bool
}

// NewScorer computes IDF (over the candidate set) and average document
// length from the per-document stats of every candidate. queryTerms is
// the de-duplicated, tokenized query. k1/b are the BM25 parameters
// (pass DefaultK1/DefaultB unless tuning).
func NewScorer(queryTerms []string, docs []DocStats, k1, b float64) *Scorer {
	n := len(docs)
	df := make(map[string]int, len(queryTerms))
	totalLen := 0
	for _, d := range docs {
		totalLen += d.DocLen
		for t := range d.TermFreqs {
			if d.TermFreqs[t] > 0 {
				df[t]++
			}
		}
	}
	idf := make(map[string]float64, len(queryTerms))
	for _, t := range uniq(queryTerms) {
		nt := df[t]
		// BM25+ non-negative IDF: ln(1 + (N − n + 0.5)/(n + 0.5)).
		idf[t] = math.Log(1 + (float64(n)-float64(nt)+0.5)/(float64(nt)+0.5))
	}
	avgdl := 0.0
	if n > 0 {
		avgdl = float64(totalLen) / float64(n)
	}
	if k1 <= 0 {
		k1 = DefaultK1
	}
	if b < 0 || b > 1 {
		b = DefaultB
	}
	return &Scorer{idf: idf, avgdl: avgdl, k1: k1, b: b, hasDoc: n > 0}
}

// Score returns the BM25 relevance of one document. A document with no
// query-term hits scores 0.
func (s *Scorer) Score(d DocStats) float64 {
	if !s.hasDoc || s.avgdl == 0 {
		return 0
	}
	lenNorm := s.k1 * (1 - s.b + s.b*float64(d.DocLen)/s.avgdl)
	var score float64
	for term, f := range d.TermFreqs {
		if f <= 0 {
			continue
		}
		idf, ok := s.idf[term]
		if !ok {
			continue
		}
		ff := float64(f)
		score += idf * (ff * (s.k1 + 1)) / (ff + lenNorm)
	}
	return score
}

// ReciprocalRankFusion fuses two rankings of the same item set into a
// single score per item, the standard RRF formula:
//
//	RRF(item) = Σ_ranking 1 / (k + rank(item))
//
// rank is 1-based position in each input ranking. k (typically 60)
// damps the contribution of low ranks so neither list dominates. Each
// input is a slice of item IDs in ranked order (best first); the return
// maps item ID → fused score. Items missing from a ranking simply don't
// contribute that list's term.
func ReciprocalRankFusion(k float64, rankings ...[]string) map[string]float64 {
	if k <= 0 {
		k = 60
	}
	fused := make(map[string]float64)
	for _, ranking := range rankings {
		for i, id := range ranking {
			fused[id] += 1.0 / (k + float64(i+1))
		}
	}
	return fused
}

// DefaultRRFK is the conventional reciprocal-rank-fusion damping
// constant.
const DefaultRRFK = 60

func uniq(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0:0]
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
