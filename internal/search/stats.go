package search

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
// how to bucket. String-typed attributes are the natural case; the
// time-bucket entries (mtime_year/month/day, taken_at_*, sent_at_*,
// date_*) format the timestamp as a string before bucketing — files
// with zero timestamps fall into the "(no date)" bucket. List- and
// numeric-typed attributes intentionally aren't supported — they'd
// need per-element or range semantics that are out of scope.
var validGroupBys = map[string]struct{}{
	// String attributes.
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
	// Time-bucket attributes (string formatted from the named
	// timestamp). mtime_* reads FileAttributes.ModTime;
	// taken_at_* / sent_at_* / date_* read from Extra.
	"mtime_year":     {},
	"mtime_month":    {},
	"mtime_day":      {},
	"taken_at_year":  {},
	"taken_at_month": {},
	"taken_at_day":   {},
	"sent_at_year":   {},
	"sent_at_month":  {},
	"sent_at_day":    {},
	"date_year":      {},
	"date_month":     {},
	"date_day":       {},
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

// detectorOnlyGroupBys are the group_by keys whose bucket value can be
// read straight off the search.Result without parsing per-format
// attributes. content_type / ext / dir come from Result fields; mtime_*
// reads Result.Attrs.ModTime (populated by the walker's stat call, not
// by ContentType.Attributes). For these keys ComputeStats can short-
// circuit the expensive Attributes() parse via SkipAttributesParse —
// turning /Applications-scale stats from minutes into seconds.
var detectorOnlyGroupBys = map[string]struct{}{
	"content_type": {},
	"ext":          {},
	"dir":          {},
	"mtime_year":   {},
	"mtime_month":  {},
	"mtime_day":    {},
}

// groupByNeedsAttributes reports whether the named bucket key requires
// the per-format ContentType.Attributes() parse to be run. Detector-
// only keys (see detectorOnlyGroupBys) return false; everything else
// returns true.
func groupByNeedsAttributes(groupBy string) bool {
	_, ok := detectorOnlyGroupBys[groupBy]
	return !ok
}

// exprIsTrivial reports whether the CEL expression doesn't need any
// per-format attributes — i.e. it's empty or the literal "true".
// Anything else might filter on title / word_count / camera_make /
// etc. and must keep the parse on. The CEL evaluator already short-
// circuits "true" without inspecting attrs, so when both expr is
// trivial AND group_by is detector-only, the Attributes() call is
// pure waste.
func exprIsTrivial(expr string) bool {
	switch strings.TrimSpace(expr) {
	case "", "true":
		return true
	}
	return false
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
	// Default empty expr → "true" so library callers don't need to do
	// it themselves. Mirrors the CLI / MCP handler convention.
	if opts.Expr == "" {
		opts.Expr = "true"
	}

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

	// Fast path: when the bucket key can be read straight off
	// Result (detector-only) AND the CEL expression doesn't need
	// per-format attributes, skip the expensive ContentType.
	// Attributes() parse. Cuts /Applications-scale stats from
	// minutes (parsing every Mach-O / image / archive on disk) to
	// seconds (just stat + detect). Falls back to the full parse
	// for any non-trivial expr or attribute-derived group_by key.
	if !groupByNeedsAttributes(groupBy) && exprIsTrivial(opts.Expr) {
		opts.SkipAttributesParse = true
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
	// Time-bucket keys: extract the underlying time.Time, format
	// to the requested granularity. Zero timestamps fall into a
	// distinct "(no date)" bucket so they don't collide with
	// "1970-01-01".
	if attr, layout, ok := timeBucketSpec(groupBy); ok {
		t := pullTime(r, attr)
		if t.IsZero() {
			return "(no date)"
		}
		return t.Format(layout)
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

// timeBucketSpec maps a group_by key like "mtime_month" to the
// underlying attribute name + a time.Format layout. Returns
// ok=false for non-time-bucket keys.
func timeBucketSpec(groupBy string) (attr, layout string, ok bool) {
	for _, prefix := range []string{"mtime", "taken_at", "sent_at", "date"} {
		for _, gran := range []struct {
			suffix string
			layout string
		}{
			{"_year", "2006"},
			{"_month", "2006-01"},
			{"_day", "2006-01-02"},
		} {
			if groupBy == prefix+gran.suffix {
				return prefix, gran.layout, true
			}
		}
	}
	return "", "", false
}

// pullTime resolves the named time attribute on a Result. mtime
// reads FileAttributes.ModTime directly; the rest go through
// Extra. Returns zero time when missing or wrong-typed — the
// caller buckets it as "(no date)".
func pullTime(r Result, attr string) time.Time {
	if attr == "mtime" {
		if r.Attrs != nil {
			return r.Attrs.ModTime
		}
		return time.Time{}
	}
	if r.Attrs == nil || r.Attrs.Extra == nil {
		return time.Time{}
	}
	v, ok := r.Attrs.Extra[attr]
	if !ok {
		return time.Time{}
	}
	t, ok := v.(time.Time)
	if !ok {
		return time.Time{}
	}
	return t
}
