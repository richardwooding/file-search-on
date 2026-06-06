package search

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/fingerprint"
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
	// Count is the FULL membership count for this group — unchanged
	// by per-group truncation so agents still see the real cluster
	// size when Members has been capped via NearDupMembersLimit.
	Count   int                   `json:"count"`
	Members []NearDuplicateMember `json:"members"`
	// MembersTotal mirrors Count when Members is unscaled; when the
	// caller asked for NearDupMembersLimit ≥ 1 and the group had
	// more members, MembersTotal carries the pre-truncation count so
	// the caller knows there's more to drill into. Issue #279.
	MembersTotal int `json:"members_total,omitempty"`
	// MembersTruncated fires when Members was capped via the
	// per-group limit input. Distinct from Count > len(Members)
	// alone because zero-limit (default) leaves Members untouched
	// and we want the boolean to be unambiguous on the wire.
	MembersTruncated bool `json:"members_truncated,omitempty"`
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
	explicitlySet := threshold > 0 && threshold <= 1
	if !explicitlySet {
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
	sourceCount := 0
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
		fp := fingerprintFromCacheOrCompute(r.Path, r.Size, r.Attrs.ModTime, body, r.ContentType, opts.Index)
		if fp == 0 {
			// Empty or near-empty body — no signal.
			continue
		}
		if languageFromContentType(r.ContentType) != "" {
			sourceCount++
		}
		candidates = append(candidates, nearDupCandidate{path: r.Path, size: r.Size, fingerprint: fp})
	}
	out.FingerPrinted = int64(len(candidates))

	// Source-dominated trees need a tighter default than the 0.85
	// originally chosen for prose / markdown. Go idiomatic tokens
	// (func / err / nil / ctx / return) push EVERY file's SimHash
	// into a similar bit pattern even after preprocessForFingerprint
	// strips imports + license headers; at 0.85 ≈ 9 bits the false-
	// positive rate is high enough to bury real near-dups. 0.92 ≈ 5
	// bits cuts the noise while still catching template-copies and
	// regenerated boilerplate within the same project. Threshold the
	// caller supplied explicitly always wins. Issue #274.
	if !explicitlySet && len(candidates) > 0 && sourceCount*2 > len(candidates) {
		threshold = 0.92
		out.Threshold = threshold
	}

	if len(candidates) >= 2 {
		out.Groups = groupNearDuplicates(ctx, candidates, threshold)
		out.GroupCount = int64(len(out.Groups))
		applyNearDupCaps(&out.Groups, opts.NearDupMembersLimit, opts.NearDupGroupLimit)
	}

	out.Cancelled, out.CancellationReason = classifyCancellation(walkErr, ctx)
	if walkErr != nil && !out.Cancelled {
		return out, walkErr
	}
	return out, nil
}

// fingerprintFromCacheOrCompute returns the boilerplate-stripped,
// shingled SimHash of body. When idx is non-nil and an entry validates
// by (size, mtime), the cached FingerprintV3 is returned unless it's
// zero. On miss / zero-V3 hit, body is run through
// preprocessForFingerprint (per the file's contentType) then fed to
// fingerprint.Compute, and the result is written back to
// FingerprintV3. The legacy Fingerprint (V1) and single-token
// FingerprintV2 fields are no longer read — left on the struct for
// back-compat decode of older caches but never trusted by this
// pipeline (mixing them with V3 shingled values would reproduce the
// #310 false positives).
//
// Path normalisation matches the BuildAttributesWith cache-key
// convention (filepath.Abs + filepath.Clean) so two callers walking
// the same tree under different roots hit the same entry.
func fingerprintFromCacheOrCompute(path string, size int64, modTime time.Time, body, contentType string, idx index.Index) uint64 {
	key := ""
	if idx != nil {
		if abs, err := filepath.Abs(path); err == nil {
			key = filepath.Clean(abs)
		}
	}

	// Cache hit with populated V3 fingerprint → return. Older V1 /
	// single-token V2 values are ignored on purpose — they're
	// incomparable with the V3 shingled fingerprints and would
	// reproduce the #310 (V1: #274) false positives.
	var cached *index.Entry
	if key != "" {
		if c, ok := idx.Lookup(key, size, modTime); ok {
			cached = c
			if c.FingerprintV3 != 0 {
				return c.FingerprintV3
			}
		}
	}

	preprocessed := preprocessForFingerprint(body, contentType)
	fp := fingerprint.Compute(preprocessed)
	if fp == 0 {
		return 0
	}

	// Write-back, merging with cached fields when present.
	if key != "" {
		entry := &index.Entry{
			Size:            size,
			ModTimeUnixNano: modTime.UnixNano(),
			FingerprintV3:   fp,
		}
		if cached != nil {
			entry.ContentType = cached.ContentType
			entry.Extra = cached.Extra
			entry.Hash = cached.Hash
			entry.Fingerprint = cached.Fingerprint     // preserve legacy V1 on round-trip
			entry.FingerprintV2 = cached.FingerprintV2 // preserve legacy V2 on round-trip
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
// for v1. The O(N²) pass runs AFTER the (cancellable) walk, so it
// checks ctx once per outer iteration and bails with no groups on
// cancellation — otherwise a timed-out / Ctrl-C'd call would still
// grind through tens of millions of comparisons on a large candidate
// set before returning. The caller stamps cancelled=true via
// classifyCancellation when ctx is done.
func groupNearDuplicates(ctx context.Context, candidates []nearDupCandidate, threshold float64) []NearDuplicateGroup {
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
		if ctx.Err() != nil {
			return nil // cancelled mid-grouping — caller reports cancelled=true
		}
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

// applyNearDupCaps applies the per-group member cap and the top-N
// group cap requested by the caller. Mutates *groups in place because
// the slice is a fresh allocation owned by FindNearDuplicates — no
// aliasing risk.
//
// Member-cap semantics: when memberLimit > 0 AND a group has more
// members than the cap, keep the first memberLimit (groupNearDuplicates
// already sorts by similarity descending, so the survivors are the
// strongest matches) and stamp MembersTotal + MembersTruncated. When
// memberLimit ≤ 0 the cap is disabled and groups are returned
// unchanged.
//
// Group-cap semantics: when groupLimit > 0 AND there are more groups
// than the cap, truncate to the first groupLimit. groupNearDuplicates
// already sorts groups by member count desc / representative size
// desc so the kept groups are the largest / most-interesting clusters.
// Issue #279.
func applyNearDupCaps(groups *[]NearDuplicateGroup, memberLimit, groupLimit int) {
	if memberLimit > 0 {
		for i := range *groups {
			g := &(*groups)[i]
			if len(g.Members) > memberLimit {
				g.MembersTotal = len(g.Members)
				g.MembersTruncated = true
				g.Members = g.Members[:memberLimit]
			}
		}
	}
	if groupLimit > 0 && len(*groups) > groupLimit {
		*groups = (*groups)[:groupLimit]
	}
}
