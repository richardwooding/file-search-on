package search

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Cursor-based pagination for buffered result sets (issue #336). The
// design is a STATELESS KEYSET cursor: there is no server-side cached
// result list. Each page re-walks the tree (attribute extraction is
// index-cached, so the re-walk is cheap) and resumes past an opaque
// token that encodes the sort key, the order, and the last returned
// item's (sort value, path). Paging is stable across calls under an
// unchanged tree, survives a server restart, and degrades gracefully
// when the tree changes (the keyset comparison finds the first item
// strictly after the cursor's position regardless of whether the exact
// cursor file still exists).
//
// The total order is (sort key, path). Path is the tiebreaker so the
// ordering is total even when many files share a sort value — without a
// total order, keyset resumption can't be consistent. The tiebreaker is
// ALWAYS ascending by path, independent of the primary order, so the
// page boundary is well-defined in both asc and desc modes.

// pageCursor is the decoded form of the opaque token. Encoded as
// base64(JSON) so it's a single opaque string to the client.
type pageCursor struct {
	Sort  string    `json:"sort"`
	Order string    `json:"order"`
	Val   cursorVal `json:"val"`
	Path  string    `json:"path"`
}

// cursorVal is a type-tagged scalar so the JSON round-trip preserves the
// distinction between int / float / string / time that a bare JSON
// number would lose (every JSON number decodes to float64).
type cursorVal struct {
	Kind string  `json:"k"` // "int" | "float" | "str" | "time" | "nil"
	Int  int64   `json:"i,omitempty"`
	Flt  float64 `json:"f,omitempty"`
	Str  string  `json:"s,omitempty"`
	Time string  `json:"t,omitempty"` // RFC3339Nano
}

// PaginateResults applies a deterministic total-order sort (sortKey with
// path as tiebreaker), resumes past cursor (empty = first page), caps to
// limit, and returns an opaque next cursor when more results remain
// (next == "" means the last page). Empty sortKey orders by path. The
// passed slice is sorted in place. Issue #336.
func PaginateResults(results []Result, sortKey, order, cursor string, limit int) (page []Result, next string, err error) {
	order = normalizeOrder(order)
	sortResultsPageable(results, sortKey, order)

	if cursor != "" {
		cur, derr := decodeCursor(cursor)
		if derr != nil {
			return nil, "", derr
		}
		if cur.Sort != sortKey || cur.Order != order {
			return nil, "", fmt.Errorf("cursor was issued for sort=%q order=%q but this call uses sort=%q order=%q — start a fresh page or match the original sort", cur.Sort, cur.Order, sortKey, order)
		}
		// The slice is in total order, so "is strictly after the cursor"
		// is monotone (false… then true…) and a binary search finds the
		// first surviving element.
		idx := sort.Search(len(results), func(i int) bool {
			return afterCursor(results[i], sortKey, order, cur)
		})
		results = results[idx:]
	}

	if limit > 0 && len(results) > limit {
		last := results[limit-1]
		next = encodeCursor(pageCursor{
			Sort:  sortKey,
			Order: order,
			Val:   toCursorVal(sortValueOf(last, sortKey)),
			Path:  last.Path,
		})
		return results[:limit], next, nil
	}
	return results, "", nil
}

// normalizeOrder lowercases and defaults the order so a cursor issued
// with order "" (asc default) round-trips equal to a later call that
// passes "asc" explicitly.
func normalizeOrder(order string) string {
	if strings.EqualFold(order, "desc") {
		return "desc"
	}
	return "asc"
}

// sortResultsPageable orders results by (sortKey per order, path asc).
// Uses the same scalar comparison as compareByKey via the shared
// cursorVal path, so the pageable order matches the display order.
func sortResultsPageable(in []Result, key, order string) {
	desc := order == "desc"
	sort.Slice(in, func(i, j int) bool {
		c := cmpCursorVal(toCursorVal(sortValueOf(in[i], key)), toCursorVal(sortValueOf(in[j], key)))
		if c != 0 {
			if desc {
				return c > 0
			}
			return c < 0
		}
		return in[i].Path < in[j].Path // total-order tiebreak, always asc
	})
}

// afterCursor reports whether r sorts strictly after the cursor position
// in the (sortKey per order, path asc) total order.
func afterCursor(r Result, key, order string, cur pageCursor) bool {
	c := cmpCursorVal(toCursorVal(sortValueOf(r, key)), cur.Val)
	if order == "desc" {
		c = -c
	}
	if c != 0 {
		return c > 0
	}
	return r.Path > cur.Path
}

// sortValueOf extracts the comparable sort value for a result under the
// named key. Mirrors compareByKey's key handling. Empty/"path" → path.
// Returns nil for an absent attribute (sorts last, like compareByKey).
func sortValueOf(r Result, key string) any {
	switch key {
	case "", "path":
		return r.Path
	case "size":
		return r.Size
	case "name":
		return filenameOf(r)
	case "mod_time":
		return modTimeOf(r)
	case "similarity":
		return similarityOf(r)
	case "rank":
		return r.Rank
	case "git_last_commit_time":
		return gitLastCommitTimeOf(r)
	case "git_first_seen":
		return gitFirstSeenOf(r)
	case "git_commit_count":
		return gitCommitCountOf(r)
	}
	if v, ok := extraScalar(r, key); ok {
		return v
	}
	return nil
}

func toCursorVal(v any) cursorVal {
	switch x := v.(type) {
	case int64:
		return cursorVal{Kind: "int", Int: x}
	case float64:
		return cursorVal{Kind: "float", Flt: x}
	case string:
		return cursorVal{Kind: "str", Str: x}
	case time.Time:
		return cursorVal{Kind: "time", Time: x.Format(time.RFC3339Nano)}
	default:
		return cursorVal{Kind: "nil"}
	}
}

// cmpCursorVal compares two tagged values. A "nil" value (absent
// attribute) sorts after any present value, matching compareByKey's
// "missing groups at the end" rule. Differing non-nil kinds (which a
// single sort key shouldn't produce in practice) fall back to comparing
// the kind string so the order stays total.
func cmpCursorVal(a, b cursorVal) int {
	if a.Kind == "nil" || b.Kind == "nil" {
		if a.Kind == b.Kind {
			return 0
		}
		if a.Kind == "nil" {
			return 1 // a missing → after b
		}
		return -1
	}
	if a.Kind != b.Kind {
		return strings.Compare(a.Kind, b.Kind)
	}
	switch a.Kind {
	case "int":
		return cmpInt(a.Int, b.Int)
	case "float":
		return cmpFloat(a.Flt, b.Flt)
	case "str":
		return cmpString(a.Str, b.Str)
	case "time":
		at, _ := time.Parse(time.RFC3339Nano, a.Time)
		bt, _ := time.Parse(time.RFC3339Nano, b.Time)
		return cmpTime(at, bt)
	}
	return 0
}

func encodeCursor(c pageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (pageCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return pageCursor{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var c pageCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return pageCursor{}, fmt.Errorf("invalid cursor payload: %w", err)
	}
	return c, nil
}
