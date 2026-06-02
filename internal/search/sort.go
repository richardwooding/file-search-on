package search

import (
	"sort"
	"strings"
	"time"
)

// SortAndLimit applies Options.Sort + Options.Order + Options.Limit to
// a buffered result slice (the path used by Walk and the MCP search
// handler). When Sort is empty the slice is left in walk order
// (post-emit, so not deterministic across runs); Limit is still
// applied. When Sort is set the slice is reordered stably by the
// named key and then truncated.
//
// Unknown sort keys are tolerated: every result tied at sortable=0
// places by stable order, so callers see a no-op rather than an
// error. Documented sort keys (see Options.Sort) are intentionally
// the union of common scalar attributes; list/map attributes are not
// sortable.
//
// Exported so the MCP search handler (which collects matches into
// its own slice for progress-notification reasons) can reuse the
// same logic without re-implementing it.
func SortAndLimit(results []Result, opts Options) []Result {
	if opts.Sort != "" {
		results = sortResults(results, opts.Sort, opts.Order)
	}
	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results
}

func sortResults(in []Result, key, order string) []Result {
	desc := strings.EqualFold(order, "desc")
	sort.SliceStable(in, func(i, j int) bool {
		c := compareByKey(in[i], in[j], key)
		if desc {
			return c > 0
		}
		return c < 0
	})
	return in
}

// compareByKey returns negative / zero / positive depending on whether
// a's sort value for the given key is less than / equal to / greater
// than b's. Files missing the attribute (e.g. sort by "duration" on a
// markdown file) compare as zero — they group together in walk order.
func compareByKey(a, b Result, key string) int {
	switch key {
	case "size":
		return cmpInt(a.Size, b.Size)
	case "name":
		return cmpString(filenameOf(a), filenameOf(b))
	case "path":
		return cmpString(a.Path, b.Path)
	case "mod_time":
		return cmpTime(modTimeOf(a), modTimeOf(b))
	case "similarity":
		return cmpFloat(similarityOf(a), similarityOf(b))
	case "rank":
		return cmpFloat(a.Rank, b.Rank)
	// Git-aware sort keys (parity follow-up to #271, #299). git_*
	// fields are typed FileAttributes fields, NOT entries on the
	// Extra map, so the extraScalar fallback below misses them and
	// the sort silently no-ops. Pull them straight off Attrs.
	case "git_last_commit_time":
		return cmpTime(gitLastCommitTimeOf(a), gitLastCommitTimeOf(b))
	case "git_first_seen":
		return cmpTime(gitFirstSeenOf(a), gitFirstSeenOf(b))
	case "git_commit_count":
		return cmpInt(gitCommitCountOf(a), gitCommitCountOf(b))
	}
	// Per-family scalar keys live in FileAttributes.Extra. Pull the
	// value via the Attrs pointer (nil when IncludeAttributes is
	// false, which means we can't sort — every result compares
	// equal and the caller gets walk order).
	av, aok := extraScalar(a, key)
	bv, bok := extraScalar(b, key)
	if !aok && !bok {
		return 0
	}
	if !aok {
		return 1 // a missing → sorts after b
	}
	if !bok {
		return -1
	}
	return cmpScalar(av, bv)
}

func filenameOf(r Result) string {
	if r.Attrs != nil {
		return r.Attrs.Name
	}
	// Fallback: last path component, without forcing a filepath dep
	// for what's almost always set via Attrs anyway.
	if i := strings.LastIndexAny(r.Path, "/\\"); i >= 0 {
		return r.Path[i+1:]
	}
	return r.Path
}

func modTimeOf(r Result) time.Time {
	if r.Attrs != nil {
		return r.Attrs.ModTime
	}
	return time.Time{}
}

func similarityOf(r Result) float64 {
	if r.Attrs != nil {
		return r.Attrs.Similarity
	}
	return 0
}

func gitLastCommitTimeOf(r Result) time.Time {
	if r.Attrs != nil {
		return r.Attrs.GitLastCommitTime
	}
	return time.Time{}
}

func gitFirstSeenOf(r Result) time.Time {
	if r.Attrs != nil {
		return r.Attrs.GitFirstSeen
	}
	return time.Time{}
}

func gitCommitCountOf(r Result) int64 {
	if r.Attrs != nil {
		return r.Attrs.GitCommitCount
	}
	return 0
}

// extraScalar pulls a comparable value (int64, float64, time.Time)
// from Result.Attrs.Extra by key. Returns (zero, false) when the
// result has no Attrs (IncludeAttributes was false) or when the key
// is absent or has a non-scalar type. List/map values are not
// sortable; we don't try to invent a comparison.
func extraScalar(r Result, key string) (any, bool) {
	if r.Attrs == nil || r.Attrs.Extra == nil {
		return nil, false
	}
	v, ok := r.Attrs.Extra[key]
	if !ok {
		return nil, false
	}
	switch v.(type) {
	case int64, float64, time.Time:
		return v, true
	}
	return nil, false
}

func cmpInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpString(a, b string) int { return strings.Compare(a, b) }

func cmpTime(a, b time.Time) int {
	switch {
	case a.Before(b):
		return -1
	case a.After(b):
		return 1
	default:
		return 0
	}
}

func cmpScalar(a, b any) int {
	switch av := a.(type) {
	case int64:
		if bv, ok := b.(int64); ok {
			return cmpInt(av, bv)
		}
	case float64:
		if bv, ok := b.(float64); ok {
			return cmpFloat(av, bv)
		}
	case time.Time:
		if bv, ok := b.(time.Time); ok {
			return cmpTime(av, bv)
		}
	}
	return 0
}
