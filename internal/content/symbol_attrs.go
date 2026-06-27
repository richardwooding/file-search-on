package content

import (
	"fmt"
	"strconv"
	"strings"

	tssymbols "github.com/richardwooding/treesitter-symbols"
)

// This file holds the helpers that pack the treesitter-symbols result (#540)
// into file-search-on's builder-internal attribute formats, plus two small
// utilities (dedupeStrings, maxComplexityOf) shared with the Go-path extractor.

// dedupeStrings returns s with duplicates removed, preserving first-seen order.
// Returns nil for empty input so the caller's len() guards skip the attribute.
func dedupeStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(s))
	out := s[:0]
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// maxComplexityOf returns the highest cyclomatic complexity across the
// per-function rows ("func\x00complexity\x00startLine\x00endLine[\x00cognitive]")
// — the int64 the CEL max_complexity attribute carries. 0 when no rows.
func maxComplexityOf(rows []string) int64 {
	var maxCx int64
	for _, r := range rows {
		parts := strings.SplitN(r, "\x00", 4)
		if len(parts) < 2 {
			continue
		}
		if cx, err := strconv.ParseInt(parts[1], 10, 64); err == nil && cx > maxCx {
			maxCx = cx
		}
	}
	return maxCx
}

// callEdgeStrings packs treesitter-symbols call edges into the builder-internal
// "caller\x00callee" form the code graph (#368) consumes.
func callEdgeStrings(edges []tssymbols.CallEdge) []string {
	if len(edges) == 0 {
		return nil
	}
	out := make([]string, len(edges))
	for i, e := range edges {
		out[i] = e.Caller + "\x00" + e.Callee
	}
	return out
}

// methodOwnerStrings packs treesitter-symbols method→owner pairs into the
// builder-internal "method\x00owner" form (#445).
func methodOwnerStrings(owners []tssymbols.MethodOwner) []string {
	if len(owners) == 0 {
		return nil
	}
	out := make([]string, len(owners))
	for i, o := range owners {
		out[i] = o.Method + "\x00" + o.Owner
	}
	return out
}

// complexityRowStrings packs treesitter-symbols function spans into the
// builder-internal per-function rows (#364, #485):
// "name\x00complexity\x00startLine\x00endLine[\x00cognitive]". The trailing
// cognitive field is emitted only when available (nil for Swift).
func complexityRowStrings(spans []tssymbols.FunctionSpan) []string {
	if len(spans) == 0 {
		return nil
	}
	rows := make([]string, 0, len(spans))
	for _, s := range spans {
		if s.Cognitive != nil {
			rows = append(rows, fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%d", s.Name, s.Cyclomatic, s.StartLine, s.EndLine, *s.Cognitive))
		} else {
			rows = append(rows, fmt.Sprintf("%s\x00%d\x00%d\x00%d", s.Name, s.Cyclomatic, s.StartLine, s.EndLine))
		}
	}
	return rows
}
