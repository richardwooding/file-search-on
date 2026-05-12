package search

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// Stats is the aggregate shape returned by ComputeStats — a quick
// reconnaissance view of a directory tree. Groups is the primary
// histogram: each entry's Name is the bucket key per Options.GroupBy
// (defaults to "content_type"); Count + TotalSize aggregate the
// files in that bucket. ContentTypes mirrors Groups when GroupBy is
// "content_type" or unset, kept for back-compat with v0.20 clients.
//
// Cancelled / CancellationReason mirror the search tool's
// partial-result fields: on ctx-cancellation the aggregator returns
// whatever was tallied up to that point, with the flag set.
type Stats struct {
	TotalCount         int64               `json:"total_count"`
	TotalSize          int64               `json:"total_size"`
	GroupBy            string              `json:"group_by,omitempty"`
	Groups             []Bucket            `json:"groups"`
	ContentTypes       []ContentTypeBucket `json:"content_types,omitempty"`
	Cancelled          bool                `json:"cancelled,omitempty"`
	CancellationReason string              `json:"cancellation_reason,omitempty"`
}

// Bucket is one row of the stats histogram. Used for the
// type-agnostic Groups slice — the legacy ContentTypeBucket has the
// same shape and exists only for back-compat.
type Bucket struct {
	Name      string `json:"name"`
	Count     int64  `json:"count"`
	TotalSize int64  `json:"total_size"`
}

// ContentTypeBucket is the legacy v0.19/v0.20 stats bucket shape.
// New code should use Bucket; ContentTypeBucket is kept so existing
// MCP clients that hard-coded `content_types[]` continue to work
// when GroupBy is unset or "content_type".
type ContentTypeBucket = Bucket

// validGroupBys is the closed set of attributes ComputeStats knows
// how to bucket. Time-, list-, and numeric-typed attributes are
// intentionally not in this set — they'd need range / per-element
// bucketing semantics that are out of scope.
var validGroupBys = map[string]struct{}{
	"content_type":       {},
	"ext":                {},
	"dir":                {},
	"language":           {},
	"camera_make":        {},
	"camera_model":       {},
	"lens":               {},
	"artist":             {},
	"album":              {},
	"genre":              {},
	"kernel":             {},
	"binary_format":      {},
	"binary_type":        {},
	"frontmatter_format": {},
}

// ValidGroupBys returns the curated set of group_by keys ComputeStats
// supports. Useful for CLI help text and the MCP tool description.
func ValidGroupBys() []string {
	out := make([]string, 0, len(validGroupBys))
	for k := range validGroupBys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ComputeStats walks the directory tree(s) and tallies counts +
// sizes per bucket. Bucketing key is opts.GroupBy when set (and
// recognised); otherwise content_type. Reuses the standard Walk
// pipeline so excludes, .gitignore, the attribute cache, and
// ctx-cancellation all behave the same way as the search tool.
func ComputeStats(ctx context.Context, opts Options, registry *content.Registry) (*Stats, error) {
	// Force the attribute path on so any non-trivial filter
	// expression sees the full FileAttributes. Result.ContentType +
	// Result.Size are populated regardless. Sort/Limit are wiped so
	// an inherited Options doesn't accidentally truncate.
	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0

	groupBy := opts.GroupBy
	if groupBy == "" {
		groupBy = "content_type"
	}
	// Unknown group_by falls back to a single "" bucket holding
	// every match — strictly better than failing the call and
	// matches the broader "broken input degrades, not errors"
	// pattern. Validity is best discovered via list_attributes (or
	// the CLI's --help) rather than a runtime error.
	if _, ok := validGroupBys[groupBy]; !ok {
		groupBy = "content_type"
	}

	results, walkErr := Walk(ctx, opts, registry)

	stats := &Stats{GroupBy: groupBy}
	buckets := map[string]*Bucket{}
	for _, r := range results {
		stats.TotalCount++
		stats.TotalSize += r.Size
		key := bucketKey(r, groupBy)
		b, ok := buckets[key]
		if !ok {
			b = &Bucket{Name: key}
			buckets[key] = b
		}
		b.Count++
		b.TotalSize += r.Size
	}
	stats.Groups = make([]Bucket, 0, len(buckets))
	for _, b := range buckets {
		stats.Groups = append(stats.Groups, *b)
	}
	// Sort by count desc, tie-break by name asc — stable across
	// runs (useful for diffing reconnaissance reports).
	sort.Slice(stats.Groups, func(i, j int) bool {
		if stats.Groups[i].Count != stats.Groups[j].Count {
			return stats.Groups[i].Count > stats.Groups[j].Count
		}
		return stats.Groups[i].Name < stats.Groups[j].Name
	})
	// Back-compat: populate ContentTypes with the same data when
	// the default group_by is in effect. Clients that pinned
	// `content_types[]` (v0.19 / v0.20) keep working; new clients
	// should read `groups[]` regardless of GroupBy.
	if groupBy == "content_type" {
		stats.ContentTypes = stats.Groups
	}

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

// bucketKey pulls the value of the requested attribute from a
// search.Result. content_type / ext / dir come from the path
// directly; everything else is in FileAttributes.Extra (so
// IncludeAttributes had better be set, which ComputeStats enforces).
// Missing or non-string values bucket as "unknown" so every walked
// file lands in some bucket.
func bucketKey(r Result, groupBy string) string {
	switch groupBy {
	case "content_type":
		name := r.ContentType
		if name == "" {
			return "unknown"
		}
		return name
	case "ext":
		if r.Attrs != nil && r.Attrs.Ext != "" {
			return r.Attrs.Ext
		}
		ext := strings.ToLower(filepath.Ext(r.Path))
		if ext == "" {
			return "(no ext)"
		}
		return ext
	case "dir":
		if r.Attrs != nil && r.Attrs.Dir != "" {
			return r.Attrs.Dir
		}
		return filepath.Dir(r.Path)
	}
	if r.Attrs == nil || r.Attrs.Extra == nil {
		return "unknown"
	}
	v, ok := r.Attrs.Extra[groupBy]
	if !ok {
		return "unknown"
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "unknown"
	}
	return s
}
