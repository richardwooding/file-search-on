package search

import (
	"sort"

	"github.com/richardwooding/bm25"
	"github.com/richardwooding/file-search-on/internal/celexpr"
)

// FinalizeBM25 is the buffered post-pass that turns the per-file BM25
// carrier data captured during the walk into actual scores and a final
// ranking (issue #335). It must run on the COLLECTED candidate set,
// because Okapi BM25's IDF and average-document-length are computed over
// exactly the post-filter candidates being ranked.
//
// It is a no-op when opts.KeywordQuery is empty. Otherwise it:
//  1. builds a bm25.Scorer (candidate-set IDF + avgdl) from the
//     captured per-file term frequencies and doc lengths,
//  2. writes each result's score to Attrs.BM25 (the `bm25` CEL var),
//  3. sets Result.Rank:
//     - opts.Hybrid → reciprocal-rank fusion of the BM25 ranking and the
//     similarity ranking,
//     - else opts.RankExpr set → the rank CEL expression re-evaluated
//     now that bm25 is populated (this is where `bm25*0.4 +
//     similarity*0.6` is finally computed),
//     - else → the raw bm25 score (pure keyword ranking).
//
// Callers sort by Rank (desc) afterwards — Walk and the MCP search
// handler default Sort to "rank" when a keyword query is in play.
func FinalizeBM25(results []Result, opts Options) error {
	terms := uniqStrings(bm25.Tokenize(opts.KeywordQuery))
	if len(terms) == 0 {
		return nil
	}

	docs := make([]bm25.DocStats, len(results))
	for i, r := range results {
		if r.Attrs != nil {
			docs[i] = bm25.DocStats{TermFreqs: r.Attrs.BM25TermFreqs, DocLen: r.Attrs.BM25DocLen}
		}
	}
	scorer := bm25.NewScorer(terms, docs, bm25.DefaultK1, bm25.DefaultB)
	for i := range results {
		if results[i].Attrs != nil {
			results[i].Attrs.BM25 = scorer.Score(docs[i])
		}
	}

	switch {
	case opts.Hybrid:
		applyRRF(results)
	case opts.RankExpr != "":
		expr := opts.Expr
		if expr == "" {
			expr = "true"
		}
		ev, err := celexpr.New(expr)
		if err != nil {
			return err
		}
		rank, err := ev.NewRank(opts.RankExpr)
		if err != nil {
			return err
		}
		for i := range results {
			if results[i].Attrs == nil {
				continue
			}
			if v, err := rank.Eval(results[i].Attrs); err == nil {
				results[i].Rank = v
			}
		}
	default:
		for i := range results {
			if results[i].Attrs != nil {
				results[i].Rank = results[i].Attrs.BM25
			}
		}
	}
	return nil
}

// applyRRF fuses the BM25-desc and similarity-desc rankings of the same
// candidate set into Result.Rank via reciprocal-rank fusion (issue
// #335). The fusion id is the file path (unique within a result set).
func applyRRF(results []Result) {
	kw := rankedPaths(results, func(r Result) float64 { return bmOf(r) })
	vec := rankedPaths(results, func(r Result) float64 { return simOf(r) })
	fused := bm25.ReciprocalRankFusion(bm25.DefaultRRFK, kw, vec)
	for i := range results {
		results[i].Rank = fused[results[i].Path]
	}
}

// rankedPaths returns the result paths ordered by score desc, path asc
// on ties (a deterministic total order so the RRF positions are stable).
func rankedPaths(results []Result, score func(Result) float64) []string {
	idx := make([]int, len(results))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		sa, sb := score(results[idx[a]]), score(results[idx[b]])
		if sa != sb {
			return sa > sb
		}
		return results[idx[a]].Path < results[idx[b]].Path
	})
	out := make([]string, len(idx))
	for pos, i := range idx {
		out[pos] = results[i].Path
	}
	return out
}

func bmOf(r Result) float64 {
	if r.Attrs != nil {
		return r.Attrs.BM25
	}
	return 0
}

func simOf(r Result) float64 {
	if r.Attrs != nil {
		return r.Attrs.Similarity
	}
	return 0
}

func uniqStrings(in []string) []string {
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
