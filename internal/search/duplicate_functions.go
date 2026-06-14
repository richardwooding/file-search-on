package search

import (
	"context"
	"sort"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/fingerprint"
)

// defaultDupFuncThreshold is the SimHash similarity floor for two
// functions to count as near-duplicates. Code SimHash sits high even for
// unrelated functions (shared idioms — for/if/return/err), so this matches
// the tighter source default FindNearDuplicates uses (0.92 ≈ 5 bits) rather
// than the 0.85 prose default.
const defaultDupFuncThreshold = 0.92

// defaultDupFuncMinLines drops functions shorter than this from
// consideration. Tiny functions (getters, one-line wrappers, stubs) SimHash
// to near-identical patterns and would bury real copy-paste in noise.
const defaultDupFuncMinLines = 5

// DuplicateFunctionMember is one function inside a duplicate cluster.
type DuplicateFunctionMember struct {
	Path       string  `json:"path"`
	Symbol     string  `json:"symbol"`
	StartLine  int     `json:"start_line"`
	EndLine    int     `json:"end_line"`
	Lines      int     `json:"lines"`
	Similarity float64 `json:"similarity"`
}

// DuplicateFunctionGroup is a cluster of near-identical functions. Fingerprint
// is the representative's 64-bit SimHash as hex.
type DuplicateFunctionGroup struct {
	Fingerprint string                    `json:"fingerprint"`
	Count       int                       `json:"count"`
	Members     []DuplicateFunctionMember `json:"members"`
}

// DuplicateFunctions is the aggregate result of FindDuplicateFunctions.
type DuplicateFunctions struct {
	TotalFiles         int64                    `json:"total_files"`
	FunctionsScanned   int64                    `json:"functions_scanned"`
	GroupCount         int64                    `json:"group_count"`
	Threshold          float64                  `json:"threshold"`
	MinLines           int                      `json:"min_lines"`
	Groups             []DuplicateFunctionGroup `json:"groups"`
	Hint               string                   `json:"hint,omitempty"`
	Cancelled          bool                     `json:"cancelled,omitempty"`
	CancellationReason string                   `json:"cancellation_reason,omitempty"`
}

// dupFuncCandidate is the per-function record fed to the grouping pass.
type dupFuncCandidate struct {
	path        string
	symbol      string
	start, end  int
	lines       int
	fingerprint uint64
}

// FindDuplicateFunctions walks opts.Root / opts.Roots, splits each source
// file into its functions via content.FunctionSpans (issue #366), SimHashes
// each function body, and returns clusters of near-identical functions —
// the copy-paste a file-level near-dup scan misses. Granularity is the
// function span, so duplicated logic surfaces even inside otherwise-distinct
// files.
//
// Source-only by default (opts.Expr defaults to "is_source"): FunctionSpans
// returns nil for non-source types, so they contribute nothing. Functions
// shorter than opts.DupFuncMinLines (default 5) are skipped as noise.
//
// The O(N²) grouping (shared with FindNearDuplicates' union-find) runs after
// the cancellable walk and checks ctx once per outer iteration; the
// minimum-lines filter keeps the candidate set well below the file count's
// order of magnitude in practice.
func FindDuplicateFunctions(ctx context.Context, opts Options, registry *content.Registry) (*DuplicateFunctions, error) {
	threshold := opts.SimilarityThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = defaultDupFuncThreshold
	}
	minLines := opts.DupFuncMinLines
	if minLines <= 0 {
		minLines = defaultDupFuncMinLines
	}
	out := &DuplicateFunctions{Threshold: threshold, MinLines: minLines}

	if opts.Expr == "" {
		opts.Expr = "is_source"
	}
	opts.IncludeAttributes = true
	opts.IncludeBody = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	opts.IncludeSnippet = false

	results, walkErr := Walk(ctx, opts, registry)
	out.TotalFiles = int64(len(results))

	var candidates []dupFuncCandidate
	generated := map[string]bool{}
	for _, r := range results {
		if ctx.Err() != nil {
			break
		}
		if r.Attrs == nil {
			continue
		}
		if gen, _ := r.Attrs.Extra["is_generated_code"].(bool); gen {
			generated[r.Path] = true
		}
		body, _ := r.Attrs.Extra["body"].(string)
		if body == "" {
			continue
		}
		spans := content.FunctionSpans(r.ContentType, []byte(body))
		if len(spans) == 0 {
			continue
		}
		lines := strings.Split(body, "\n")
		for _, sp := range spans {
			if sp.StartLine < 1 || sp.EndLine < sp.StartLine || sp.EndLine > len(lines) {
				continue
			}
			n := sp.EndLine - sp.StartLine + 1
			if n < minLines {
				continue
			}
			fp := fingerprint.Compute(strings.Join(lines[sp.StartLine-1:sp.EndLine], "\n"))
			if fp == 0 {
				continue
			}
			candidates = append(candidates, dupFuncCandidate{
				path: r.Path, symbol: sp.Name,
				start: sp.StartLine, end: sp.EndLine, lines: n, fingerprint: fp,
			})
		}
	}
	out.FunctionsScanned = int64(len(candidates))

	if len(candidates) >= 2 {
		out.Groups = groupDuplicateFunctions(ctx, candidates, threshold)
		out.GroupCount = int64(len(out.Groups))
	}
	// Hint over the clustered members — generated files produce many
	// look-alike functions that swamp real copy-paste (#430).
	var memberPaths []string
	for _, g := range out.Groups {
		for _, m := range g.Members {
			memberPaths = append(memberPaths, m.Path)
		}
	}
	out.Hint = generatedHintFor(memberPaths, generated)

	out.Cancelled, out.CancellationReason = classifyCancellation(walkErr, ctx)
	if walkErr != nil && !out.Cancelled {
		return out, walkErr
	}
	return out, nil
}

// groupDuplicateFunctions clusters candidates whose SimHash similarity meets
// threshold, via the same union-find as groupNearDuplicates. Representative
// (and group fingerprint) is the longest function in the cluster. Returns
// clusters of size >= 2, ordered by member count desc, then total duplicated
// lines desc, then representative path/symbol for determinism.
func groupDuplicateFunctions(ctx context.Context, candidates []dupFuncCandidate, threshold float64) []DuplicateFunctionGroup {
	parent := make([]int, len(candidates))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(x, y int) {
		if rx, ry := find(x), find(y); rx != ry {
			parent[rx] = ry
		}
	}

	for i := range candidates {
		if ctx.Err() != nil {
			return nil // cancelled mid-grouping — caller reports cancelled=true
		}
		for j := i + 1; j < len(candidates); j++ {
			if fingerprint.Similarity(candidates[i].fingerprint, candidates[j].fingerprint) >= threshold {
				union(i, j)
			}
		}
	}

	buckets := map[int][]int{}
	for i := range candidates {
		root := find(i)
		buckets[root] = append(buckets[root], i)
	}

	groups := make([]DuplicateFunctionGroup, 0, len(buckets))
	for _, idxs := range buckets {
		if len(idxs) < 2 {
			continue
		}
		repIdx := idxs[0]
		for _, idx := range idxs[1:] {
			if dupFuncLess(candidates[repIdx], candidates[idx]) {
				repIdx = idx
			}
		}
		repFP := candidates[repIdx].fingerprint
		members := make([]DuplicateFunctionMember, 0, len(idxs))
		for _, idx := range idxs {
			c := candidates[idx]
			members = append(members, DuplicateFunctionMember{
				Path: c.path, Symbol: c.symbol, StartLine: c.start, EndLine: c.end, Lines: c.lines,
				Similarity: fingerprint.Similarity(repFP, c.fingerprint),
			})
		}
		sort.Slice(members, func(i, j int) bool {
			if members[i].Similarity != members[j].Similarity {
				return members[i].Similarity > members[j].Similarity
			}
			if members[i].Path != members[j].Path {
				return members[i].Path < members[j].Path
			}
			return members[i].StartLine < members[j].StartLine
		})
		groups = append(groups, DuplicateFunctionGroup{
			Fingerprint: hex64(repFP),
			Count:       len(members),
			Members:     members,
		})
	}

	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		li, lj := groupTotalLines(groups[i]), groupTotalLines(groups[j])
		if li != lj {
			return li > lj
		}
		return groups[i].Members[0].Path < groups[j].Members[0].Path
	})
	return groups
}

// dupFuncLess reports whether a should yield representative status to b — b is
// "bigger": more lines, then lexicographically-earlier path, then symbol.
func dupFuncLess(a, b dupFuncCandidate) bool {
	if a.lines != b.lines {
		return a.lines < b.lines
	}
	if a.path != b.path {
		return a.path > b.path
	}
	return a.symbol > b.symbol
}

func groupTotalLines(g DuplicateFunctionGroup) int {
	total := 0
	for _, m := range g.Members {
		total += m.Lines
	}
	return total
}
