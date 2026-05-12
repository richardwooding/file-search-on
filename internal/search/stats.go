package search

import (
	"context"
	"sort"

	"github.com/richardwooding/file-search-on/internal/content"
)

// Stats is the aggregate shape returned by ComputeStats — a quick
// reconnaissance view of a directory tree. Each ContentTypeBucket
// carries the count + total bytes for files of that registered type
// (or "unknown" when the detector returned nothing).
//
// Cancelled / CancellationReason mirror the search tool's
// partial-result fields: on ctx-cancellation the aggregator returns
// whatever was tallied up to that point, with the flag set.
type Stats struct {
	TotalCount         int64               `json:"total_count"`
	TotalSize          int64               `json:"total_size"`
	ContentTypes       []ContentTypeBucket `json:"content_types"`
	Cancelled          bool                `json:"cancelled,omitempty"`
	CancellationReason string              `json:"cancellation_reason,omitempty"`
}

// ContentTypeBucket is one row of the stats histogram.
type ContentTypeBucket struct {
	Name      string `json:"name"`
	Count     int64  `json:"count"`
	TotalSize int64  `json:"total_size"`
}

// ComputeStats walks the directory tree under opts.Root and tallies
// per-content-type counts + sizes for files matching opts.Expr.
// Reuses the standard Walk/CEL pipeline so excludes, .gitignore, the
// attribute cache, and ctx-cancellation all behave the same way as
// the search tool.
//
// For the typical "give me an overview of this tree" call, pass
// Expr = "" (or "true") so every file is counted; pass a real CEL
// expression to get scoped stats like "how many markdown files have
// > 500 words".
//
// IncludeAttributes is forced on internally because a non-trivial
// Expr almost always depends on type-specific attributes, and
// turning it off would surprise callers whose expressions reference
// fields like word_count. The cost is the same per-file parse the
// search tool pays.
func ComputeStats(ctx context.Context, opts Options, registry *content.Registry) (*Stats, error) {
	// Force the attribute path on so any non-trivial filter
	// expression sees the full FileAttributes. Result.ContentType +
	// Result.Size are populated regardless.
	opts.IncludeAttributes = true
	// Stats doesn't sort or top-K — that's the caller's job
	// post-hoc if they want it. Wipe these so an inherited
	// Options{} doesn't accidentally truncate the histogram.
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0

	results, walkErr := Walk(ctx, opts, registry)

	stats := &Stats{}
	byType := map[string]*ContentTypeBucket{}
	for _, r := range results {
		stats.TotalCount++
		stats.TotalSize += r.Size
		name := r.ContentType
		if name == "" {
			name = "unknown"
		}
		b, ok := byType[name]
		if !ok {
			b = &ContentTypeBucket{Name: name}
			byType[name] = b
		}
		b.Count++
		b.TotalSize += r.Size
	}
	stats.ContentTypes = make([]ContentTypeBucket, 0, len(byType))
	for _, b := range byType {
		stats.ContentTypes = append(stats.ContentTypes, *b)
	}
	// Sort by count desc, tie-break by name asc so the output is
	// stable across runs (useful for diffing reconnaissance reports).
	sort.Slice(stats.ContentTypes, func(i, j int) bool {
		if stats.ContentTypes[i].Count != stats.ContentTypes[j].Count {
			return stats.ContentTypes[i].Count > stats.ContentTypes[j].Count
		}
		return stats.ContentTypes[i].Name < stats.ContentTypes[j].Name
	})

	// Partial-result semantics: on ctx cancellation, Walk still
	// returns the slice of what it collected; we tag the Stats
	// result with the cancellation reason rather than discarding.
	if walkErr != nil {
		switch walkErr {
		case context.Canceled:
			stats.Cancelled = true
			stats.CancellationReason = "client_cancel"
			return stats, nil
		case context.DeadlineExceeded:
			stats.Cancelled = true
			stats.CancellationReason = "timeout"
			return stats, nil
		}
		return stats, walkErr
	}
	return stats, nil
}
