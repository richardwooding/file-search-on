package search

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/cryptohash"
	"github.com/richardwooding/file-search-on/internal/index"
)

// Duplicate is one group of byte-identical files. Hash is the
// sha256 hex digest the group is keyed by; Size is the per-file
// size (every file in the group has the same size); Count is the
// number of files in the group; WastedBytes is (Count-1)*Size,
// the bytes a dedup would reclaim.
type Duplicate struct {
	Hash        string   `json:"hash"`
	Size        int64    `json:"size"`
	Count       int      `json:"count"`
	WastedBytes int64    `json:"wasted_bytes"`
	Paths       []string `json:"paths"`
}

// Duplicates is the aggregate result. Groups list is sorted by
// WastedBytes descending so the biggest reclamation candidates are
// first. TotalFiles is everything the walker emitted (matched the
// CEL filter); DuplicateGroups is len(Duplicates); WastedBytes is
// the sum across all groups.
//
// Cancelled / CancellationReason mirror the search and stats
// tools' partial-result fields.
type Duplicates struct {
	TotalFiles         int64       `json:"total_files"`
	DuplicateGroups    int64       `json:"duplicate_groups"`
	WastedBytes        int64       `json:"wasted_bytes"`
	Duplicates         []Duplicate `json:"duplicates"`
	Cancelled          bool        `json:"cancelled,omitempty"`
	CancellationReason string      `json:"cancellation_reason,omitempty"`
}

// FindDuplicates walks opts.Root / opts.Roots and returns groups
// of files with identical content (keyed by sha256). Two-pass for
// performance: first pass walks to collect (path, size); files
// whose size is unique cannot be duplicates and are skipped. Only
// files in size-collision groups are actually hashed. With the
// index cache, hashes are stored alongside the rest of the entry
// — repeat runs on an unchanged tree don't re-read any bytes.
//
// MinSize, when > 0, prunes files smaller than that threshold
// from both passes — useful for "what's eating my disk?" reports
// where 4-byte duplicates aren't interesting.
func FindDuplicates(ctx context.Context, opts Options, registry *content.Registry) (*Duplicates, error) {
	// We need Result.Attrs to read ModTime; force it on.
	opts.IncludeAttributes = true
	// Sort/Limit / Snippet / Body are irrelevant to duplicate
	// detection; wipe so an inherited Options doesn't accidentally
	// drop matches or read full bodies.
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	opts.IncludeSnippet = false
	opts.IncludeBody = false

	results, walkErr := Walk(ctx, opts, registry)

	out := &Duplicates{}
	out.TotalFiles = int64(len(results))

	// Pass 1: bucket by size, dropping zero-sized files and files
	// smaller than MinSize (when set). Zero-size files are
	// trivially "duplicates" by content; reporting them is rarely
	// useful and would dominate the groups list.
	type candidate struct {
		path    string
		size    int64
		modTime time.Time
	}
	bySize := map[int64][]candidate{}
	for _, r := range results {
		if r.Size <= 0 {
			continue
		}
		if opts.MinSize > 0 && r.Size < opts.MinSize {
			continue
		}
		var mt time.Time
		if r.Attrs != nil {
			mt = r.Attrs.ModTime
		}
		bySize[r.Size] = append(bySize[r.Size], candidate{
			path: r.Path, size: r.Size, modTime: mt,
		})
	}

	// Pass 2: for each size bucket with > 1 file, hash members
	// and group by hash. Files of unique sizes are guaranteed
	// distinct content — the most expensive optimization for
	// large mostly-unique trees.
	byHash := map[string][]candidate{}
	for _, group := range bySize {
		if len(group) < 2 {
			continue
		}
		for _, c := range group {
			if ctx.Err() != nil {
				goto done
			}
			h, herr := readOrComputeHash(ctx, c.path, c.size, c.modTime, opts.Index)
			if herr != nil || h == "" {
				continue
			}
			byHash[h] = append(byHash[h], c)
		}
	}
done:

	// Build the output. Only emit groups with count > 1 (the
	// size bucket may have hashed differently — a 1-byte hash
	// collision is impossible with sha256, but theoretically
	// future-proof).
	for h, group := range byHash {
		if len(group) < 2 {
			continue
		}
		size := group[0].size
		wasted := int64(len(group)-1) * size
		paths := make([]string, len(group))
		for i, c := range group {
			paths[i] = c.path
		}
		sort.Strings(paths)
		out.Duplicates = append(out.Duplicates, Duplicate{
			Hash:        h,
			Size:        size,
			Count:       len(group),
			WastedBytes: wasted,
			Paths:       paths,
		})
		out.WastedBytes += wasted
	}
	out.DuplicateGroups = int64(len(out.Duplicates))
	// Sort by wasted bytes desc; tie-break by hash for
	// determinism.
	sort.Slice(out.Duplicates, func(i, j int) bool {
		if out.Duplicates[i].WastedBytes != out.Duplicates[j].WastedBytes {
			return out.Duplicates[i].WastedBytes > out.Duplicates[j].WastedBytes
		}
		return out.Duplicates[i].Hash < out.Duplicates[j].Hash
	})

	if walkErr != nil {
		switch {
		case errors.Is(walkErr, context.Canceled):
			out.Cancelled = true
			out.CancellationReason = "client_cancel"
			return out, nil
		case errors.Is(walkErr, context.DeadlineExceeded):
			out.Cancelled = true
			out.CancellationReason = "timeout"
			return out, nil
		}
		return out, walkErr
	}
	if ctx.Err() != nil {
		out.Cancelled = true
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			out.CancellationReason = "timeout"
		} else {
			out.CancellationReason = "client_cancel"
		}
	}
	return out, nil
}

// readOrComputeHash returns the sha256 hex of the file. With a
// non-nil idx it consults the cache first; on a hit with non-empty
// Hash returns it; on a hit with empty Hash or a miss, computes
// all three (md5, sha1, sha256) in one pass via HashFile and writes
// back so the next call is free.
//
// The path is the OS-native display path (the same string
// Result.Path carries); we use os.Open against it rather than
// going through fsys because FindDuplicates only supports
// real-filesystem walks (multi-root tests use t.TempDir, which
// yields absolute paths).
func readOrComputeHash(ctx context.Context, path string, size int64, mtime time.Time, idx index.Index) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var existing *index.Entry
	if idx != nil && !mtime.IsZero() {
		if e, ok := idx.Lookup(path, size, mtime); ok {
			existing = e
			if e.Hash != "" {
				return e.Hash, nil
			}
		}
	}

	trio, err := cryptohash.File(ctx, path)
	if err != nil {
		return "", err
	}

	if idx != nil && !mtime.IsZero() {
		entry := existing
		if entry == nil {
			entry = &index.Entry{
				Size:            size,
				ModTimeUnixNano: mtime.UnixNano(),
			}
		}
		entry.Hash = trio.SHA256
		entry.MD5 = trio.MD5
		entry.SHA1 = trio.SHA1
		_ = idx.Put(path, entry)
	}
	return trio.SHA256, nil
}
