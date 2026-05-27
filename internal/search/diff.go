package search

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
)

// DiffOp is the set operation applied across two file trees in
// DiffTrees. Set operations are keyed by sha256 content hash, except
// OpMismatch which is keyed by relative path.
type DiffOp string

const (
	// OpAMinusB returns files whose content (by sha256) is present in
	// tree A but absent from tree B. The default.
	OpAMinusB DiffOp = "a-minus-b"
	// OpBMinusA is the inverse of OpAMinusB.
	OpBMinusA DiffOp = "b-minus-a"
	// OpIntersect returns content present (by sha256) in both trees.
	OpIntersect DiffOp = "intersect"
	// OpUnion returns every distinct content hash across both trees.
	OpUnion DiffOp = "union"
	// OpMismatch returns files that share a relative path between the
	// two trees but whose content (sha256) differs — drift detection.
	OpMismatch DiffOp = "mismatch"
)

// ValidDiffOp reports whether s names a known diff operation.
func ValidDiffOp(s string) bool {
	switch DiffOp(s) {
	case OpAMinusB, OpBMinusA, OpIntersect, OpUnion, OpMismatch:
		return true
	}
	return false
}

// DiffRecord is one file (or content-hash pairing) in a diff result.
// PathA / PathB are OS-native absolute paths; which are populated
// depends on Status. SHA256 is the content hash the record is keyed by
// (empty for name_match_content_differs, where the two sides differ by
// definition). Size is the per-file byte count.
type DiffRecord struct {
	Status string `json:"status"`
	PathA  string `json:"path_a,omitempty"`
	PathB  string `json:"path_b,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// Status values for DiffRecord.
const (
	StatusOnlyInA   = "only_in_a"
	StatusOnlyInB   = "only_in_b"
	StatusBoth      = "both"
	StatusNameMatch = "name_match_content_differs"
)

// DiffResult is the aggregate output. Records is sorted deterministically
// by (path_a, path_b, sha256). TotalA / TotalB are the file counts the
// walker emitted for each tree (after the CEL filter). Cancelled /
// CancellationReason mirror the other partial-result-returning entry
// points.
type DiffResult struct {
	Op                 string       `json:"op"`
	Records            []DiffRecord `json:"records"`
	Count              int          `json:"count"`
	TotalA             int          `json:"total_a"`
	TotalB             int          `json:"total_b"`
	Cancelled          bool         `json:"cancelled,omitempty"`
	CancellationReason string       `json:"cancellation_reason,omitempty"`
}

// treeFile is one hashed file within a single tree.
type treeFile struct {
	rel  string // path relative to the tree root, slash-separated
	abs  string // OS-native absolute / display path (openable)
	size int64
	hash string // sha256 hex
}

// DiffTrees walks treeA and treeB, hashes every file (honouring opts.Expr
// and the usual walk-pruning options), and applies the set operation op.
// Hashes are read from / written to opts.Index when set, so repeat diffs
// of two warm trees re-read no bytes.
//
// Issue #210. Read-only discovery — DiffTrees never mutates either tree.
func DiffTrees(ctx context.Context, treeA, treeB string, op DiffOp, opts Options, registry *content.Registry) (*DiffResult, error) {
	if !ValidDiffOp(string(op)) {
		return nil, errors.New("invalid diff op: " + string(op))
	}

	aByHash, aByRel, totalA, errA := walkTreeHashes(ctx, treeA, opts, registry)
	bByHash, bByRel, totalB, errB := walkTreeHashes(ctx, treeB, opts, registry)

	out := &DiffResult{Op: string(op), TotalA: totalA, TotalB: totalB}

	switch op {
	case OpAMinusB:
		for h, files := range aByHash {
			if _, in := bByHash[h]; in {
				continue
			}
			for _, f := range files {
				out.Records = append(out.Records, DiffRecord{
					Status: StatusOnlyInA, PathA: f.abs, SHA256: h, Size: f.size,
				})
			}
		}
	case OpBMinusA:
		for h, files := range bByHash {
			if _, in := aByHash[h]; in {
				continue
			}
			for _, f := range files {
				out.Records = append(out.Records, DiffRecord{
					Status: StatusOnlyInB, PathB: f.abs, SHA256: h, Size: f.size,
				})
			}
		}
	case OpIntersect:
		for h, aFiles := range aByHash {
			bFiles, in := bByHash[h]
			if !in {
				continue
			}
			out.Records = append(out.Records, DiffRecord{
				Status: StatusBoth, PathA: aFiles[0].abs, PathB: bFiles[0].abs, SHA256: h, Size: aFiles[0].size,
			})
		}
	case OpUnion:
		for h, aFiles := range aByHash {
			if bFiles, in := bByHash[h]; in {
				out.Records = append(out.Records, DiffRecord{
					Status: StatusBoth, PathA: aFiles[0].abs, PathB: bFiles[0].abs, SHA256: h, Size: aFiles[0].size,
				})
			} else {
				out.Records = append(out.Records, DiffRecord{
					Status: StatusOnlyInA, PathA: aFiles[0].abs, SHA256: h, Size: aFiles[0].size,
				})
			}
		}
		for h, bFiles := range bByHash {
			if _, in := aByHash[h]; in {
				continue // already emitted as "both"
			}
			out.Records = append(out.Records, DiffRecord{
				Status: StatusOnlyInB, PathB: bFiles[0].abs, SHA256: h, Size: bFiles[0].size,
			})
		}
	case OpMismatch:
		for rel, af := range aByRel {
			bf, in := bByRel[rel]
			if !in || af.hash == bf.hash {
				continue
			}
			out.Records = append(out.Records, DiffRecord{
				Status: StatusNameMatch, PathA: af.abs, PathB: bf.abs, Size: af.size,
			})
		}
	}

	sort.Slice(out.Records, func(i, j int) bool {
		a, b := out.Records[i], out.Records[j]
		if a.PathA != b.PathA {
			return a.PathA < b.PathA
		}
		if a.PathB != b.PathB {
			return a.PathB < b.PathB
		}
		return a.SHA256 < b.SHA256
	})
	out.Count = len(out.Records)

	if err := firstCancellation(errA, errB); err != nil {
		out.Cancelled = true
		if errors.Is(err, context.DeadlineExceeded) {
			out.CancellationReason = "timeout"
		} else {
			out.CancellationReason = "client_cancel"
		}
		return out, nil
	}
	if errA != nil {
		return out, errA
	}
	if errB != nil {
		return out, errB
	}
	return out, nil
}

// walkTreeHashes walks a single tree root and returns its files indexed
// by content hash (many files may share a hash) and by relative path
// (last writer wins — relative paths are unique within a tree). total
// is the number of files the walker emitted before hashing.
func walkTreeHashes(ctx context.Context, root string, opts Options, registry *content.Registry) (byHash map[string][]treeFile, byRel map[string]treeFile, total int, err error) {
	o := opts
	o.Root = ""
	o.Roots = []string{root}
	if o.Expr == "" {
		o.Expr = "true"
	}
	o.IncludeAttributes = true // need ModTime for the hash cache
	o.Sort = ""
	o.Order = ""
	o.Limit = 0
	o.RankExpr = ""
	o.IncludeSnippet = false
	o.IncludeBody = false

	results, walkErr := Walk(ctx, o, registry)
	byHash = make(map[string][]treeFile)
	byRel = make(map[string]treeFile)
	for _, r := range results {
		if ctx.Err() != nil {
			break
		}
		if r.Size <= 0 {
			continue
		}
		var mt time.Time
		if r.Attrs != nil {
			mt = r.Attrs.ModTime
		}
		h, herr := readOrComputeHash(ctx, r.Path, r.Size, mt, opts.Index)
		if herr != nil || h == "" {
			continue
		}
		rel, rerr := filepath.Rel(root, r.Path)
		if rerr != nil {
			rel = r.Path
		}
		rel = filepath.ToSlash(rel)
		tf := treeFile{rel: rel, abs: r.Path, size: r.Size, hash: h}
		byHash[h] = append(byHash[h], tf)
		byRel[rel] = tf
	}
	return byHash, byRel, len(results), walkErr
}

// firstCancellation returns the first of the given errors that is a
// context cancellation / deadline, or nil if none are.
func firstCancellation(errs ...error) error {
	for _, e := range errs {
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return e
		}
	}
	return nil
}
