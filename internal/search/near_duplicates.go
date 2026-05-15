package search

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/fingerprint"
	"github.com/richardwooding/file-search-on/internal/index"
)

// NearDuplicateMember is a single file inside a near-duplicate group.
// Similarity is the file's SimHash similarity to the group's
// representative (1.0 for the representative itself).
type NearDuplicateMember struct {
	Path       string  `json:"path"`
	Size       int64   `json:"size"`
	Similarity float64 `json:"similarity"`
}

// NearDuplicateGroup collects files whose body SimHash fingerprints
// are within the threshold Hamming distance of one another (i.e.
// pairwise similarity ≥ threshold). Representative is the largest
// file in the group; Fingerprint is its 64-bit fingerprint expressed
// as a hex string (stable for serialisation and across runs).
type NearDuplicateGroup struct {
	Representative string                `json:"representative"`
	Fingerprint    string                `json:"fingerprint"`
	Count          int                   `json:"count"`
	Members        []NearDuplicateMember `json:"members"`
}

// NearDuplicates is the aggregate result of FindNearDuplicates.
// Mirrors the Duplicates shape from the exact-match counterpart, so
// agents can switch between tools with consistent envelopes.
type NearDuplicates struct {
	TotalFiles         int64                `json:"total_files"`
	FingerPrinted      int64                `json:"fingerprinted"`
	GroupCount         int64                `json:"group_count"`
	Threshold          float64              `json:"threshold"`
	Groups             []NearDuplicateGroup `json:"groups"`
	Cancelled          bool                 `json:"cancelled,omitempty"`
	CancellationReason string               `json:"cancellation_reason,omitempty"`
}

// FindNearDuplicates walks opts.Root / opts.Roots, computes a
// SimHash fingerprint of each candidate's extracted body, and
// returns groups of files whose pairwise similarity meets or exceeds
// opts.SimilarityThreshold (default 0.85).
//
// Body extraction is forced on for every walked file (the tool's
// whole purpose is body comparison), with the existing
// canExtractBody gate: text-shaped types (markdown / text / html /
// csv / json / xml / source/*) AND structured-document types (PDF /
// office / EPUB / email). Binary families (image / audio / video /
// archive / install-package / disk-image / binary) are skipped —
// SimHash on opaque bytes is meaningless.
//
// MinSize prunes files whose on-disk size is below the threshold;
// pair comparison is O(N²) so a tight MinSize is the cheapest way
// to keep the tool responsive on large trees.
//
// Caching: fingerprints land on index.Entry.Fingerprint, validated
// by the same (size, mtime) tuple as the rest of the entry. Repeat
// runs on an unchanged tree skip the body read AND the SimHash
// compute.
//
// Cancelled / CancellationReason mirror the find_duplicates and
// search tools' partial-result fields.
func FindNearDuplicates(ctx context.Context, opts Options, registry *content.Registry) (*NearDuplicates, error) {
	threshold := opts.SimilarityThreshold
	if threshold <= 0 || threshold > 1 {
		threshold = 0.85
	}
	out := &NearDuplicates{Threshold: threshold}

	// Force the same attribute-bearing walk find_duplicates uses, plus
	// body extraction (the whole point). Wipe Sort/Limit/Snippet to
	// keep behaviour predictable when callers pass them through.
	if opts.Expr == "" {
		opts.Expr = "true"
	}
	opts.IncludeAttributes = true
	opts.IncludeBody = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	opts.IncludeSnippet = false

	results, walkErr := Walk(ctx, opts, registry)
	out.TotalFiles = int64(len(results))

	candidates := make([]nearDupCandidate, 0, len(results))
	for _, r := range results {
		if ctx.Err() != nil {
			break
		}
		if opts.MinSize > 0 && r.Size < opts.MinSize {
			continue
		}
		if r.Attrs == nil {
			continue
		}
		body, _ := r.Attrs.Extra["body"].(string)
		if body == "" {
			continue
		}
		fp := fingerprintFromCacheOrCompute(r.Path, r.Size, r.Attrs.ModTime, body, opts.Index)
		if fp == 0 {
			// Empty or near-empty body — no signal.
			continue
		}
		candidates = append(candidates, nearDupCandidate{path: r.Path, size: r.Size, fingerprint: fp})
	}
	out.FingerPrinted = int64(len(candidates))

	if len(candidates) >= 2 {
		out.Groups = groupNearDuplicates(candidates, threshold)
		out.GroupCount = int64(len(out.Groups))
	}

	out.Cancelled, out.CancellationReason = classifyCancellation(walkErr, ctx)
	if walkErr != nil && !out.Cancelled {
		return out, walkErr
	}
	return out, nil
}

// fingerprintFromCacheOrCompute returns the SimHash of body. When
// idx is non-nil and an entry validates by (size, mtime), the cached
// Fingerprint is returned unless it's zero. On a cache miss or
// zero-fingerprint hit, computes fresh and writes back, merging
// with the existing cached fields (ContentType, Extra, Hash) so
// downstream consumers (search, find_duplicates) don't lose data
// the near-duplicates pipeline doesn't touch.
//
// Path normalisation matches the BuildAttributesWith cache-key
// convention (filepath.Abs + filepath.Clean) so two callers walking
// the same tree under different roots hit the same entry.
func fingerprintFromCacheOrCompute(path string, size int64, modTime time.Time, body string, idx index.Index) uint64 {
	key := ""
	if idx != nil {
		if abs, err := filepath.Abs(path); err == nil {
			key = filepath.Clean(abs)
		}
	}

	// Cache hit with populated fingerprint → return.
	var cached *index.Entry
	if key != "" {
		if c, ok := idx.Lookup(key, size, modTime); ok {
			cached = c
			if c.Fingerprint != 0 {
				return c.Fingerprint
			}
		}
	}

	fp := fingerprint.Compute(body)
	if fp == 0 {
		return 0
	}

	// Write-back, merging with cached fields when present.
	if key != "" {
		entry := &index.Entry{
			Size:            size,
			ModTimeUnixNano: modTime.UnixNano(),
			Fingerprint:     fp,
		}
		if cached != nil {
			entry.ContentType = cached.ContentType
			entry.Extra = cached.Extra
			entry.Hash = cached.Hash
		}
		_ = idx.Put(key, entry)
	}
	return fp
}

// groupNearDuplicates runs pairwise comparisons via union-find,
// merging candidates whose similarity is at or above threshold.
// Returns groups (size >= 2) sorted by member count desc then by
// representative size desc.
//
// O(N²) — fine for thousands of candidates. For larger trees an
// LSH banding step would prune the pairwise space; out of scope
// for v1.
func groupNearDuplicates(candidates []nearDupCandidate, threshold float64) []NearDuplicateGroup {
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
		rx, ry := find(x), find(y)
		if rx != ry {
			parent[rx] = ry
		}
	}

	// Pairwise compare. For each pair (i, j) with i < j, union when
	// their fingerprints are within the threshold's Hamming distance.
	for i := range candidates {
		fi := candidates[i].fingerprint
		for j := i + 1; j < len(candidates); j++ {
			fj := candidates[j].fingerprint
			if fingerprint.Similarity(fi, fj) >= threshold {
				union(i, j)
			}
		}
	}

	// Bucket by root.
	buckets := map[int][]int{}
	for i := range candidates {
		root := find(i)
		buckets[root] = append(buckets[root], i)
	}

	// Build groups (size >= 2). Representative is the largest file
	// in each bucket.
	groups := make([]NearDuplicateGroup, 0, len(buckets))
	for _, idxs := range buckets {
		if len(idxs) < 2 {
			continue
		}
		// Find representative — largest by size, tie-break by path.
		repIdx := idxs[0]
		for _, idx := range idxs[1:] {
			if candidates[idx].size > candidates[repIdx].size ||
				(candidates[idx].size == candidates[repIdx].size && candidates[idx].path < candidates[repIdx].path) {
				repIdx = idx
			}
		}
		repFP := candidates[repIdx].fingerprint
		members := make([]NearDuplicateMember, 0, len(idxs))
		for _, idx := range idxs {
			members = append(members, NearDuplicateMember{
				Path:       candidates[idx].path,
				Size:       candidates[idx].size,
				Similarity: fingerprint.Similarity(repFP, candidates[idx].fingerprint),
			})
		}
		// Stable member order: similarity desc, then path asc.
		sort.Slice(members, func(i, j int) bool {
			if members[i].Similarity != members[j].Similarity {
				return members[i].Similarity > members[j].Similarity
			}
			return members[i].Path < members[j].Path
		})
		groups = append(groups, NearDuplicateGroup{
			Representative: candidates[repIdx].path,
			Fingerprint:    hex64(repFP),
			Count:          len(members),
			Members:        members,
		})
	}

	// Sort groups: bigger groups first, then by representative size,
	// then by representative path for determinism.
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		if groups[i].Members[0].Size != groups[j].Members[0].Size {
			return groups[i].Members[0].Size > groups[j].Members[0].Size
		}
		return groups[i].Representative < groups[j].Representative
	})
	return groups
}

// nearDupCandidate is the per-file record used inside
// groupNearDuplicates.
type nearDupCandidate struct {
	path        string
	size        int64
	fingerprint uint64
}

// hex64 formats a 64-bit fingerprint as "0x" + 16 lowercase hex
// digits. Stable across runs, easy to grep in logs / JSON output.
func hex64(v uint64) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 18)
	out[0] = '0'
	out[1] = 'x'
	for i := range 16 {
		out[17-i] = hex[v&0xF]
		v >>= 4
	}
	return string(out)
}

// classifyCancellation interprets a Walk error / ctx state into the
// (Cancelled, Reason) pair every walking tool surfaces.
func classifyCancellation(walkErr error, ctx context.Context) (bool, string) {
	if walkErr != nil {
		switch {
		case errors.Is(walkErr, context.Canceled):
			return true, "client_cancel"
		case errors.Is(walkErr, context.DeadlineExceeded):
			return true, "timeout"
		}
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return true, "timeout"
		}
		return true, "client_cancel"
	}
	return false, ""
}
