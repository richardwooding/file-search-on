// Package playground is the live CEL-filtering TUI (the `playground`
// subcommand): type a CEL expression and watch a snapshot of a directory's
// files filter as you type, over the same attribute vocabulary the search
// tool uses. It reuses internal/celexpr for compile + evaluate and
// internal/search for the one-shot attribute snapshot.
package playground

import (
	"strings"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/search"
)

// filter compiles expr once and returns the indices of results whose
// attributes match. An empty/whitespace expression matches everything. A
// COMPILE error is returned (the caller shows it and keeps the previous
// match set); per-file EVALUATE errors are treated as non-matches rather
// than aborting, so a largely-valid expression still yields results live.
func filter(results []search.Result, expr string) ([]int, error) {
	if strings.TrimSpace(expr) == "" {
		all := make([]int, len(results))
		for i := range all {
			all[i] = i
		}
		return all, nil
	}
	ev, err := celexpr.New(expr)
	if err != nil {
		return nil, err
	}
	var matched []int
	for i := range results {
		attrs := results[i].Attrs
		if attrs == nil {
			continue
		}
		ok, evErr := ev.Evaluate(attrs)
		if evErr != nil {
			continue // runtime error on this file → not a match, don't abort
		}
		if ok {
			matched = append(matched, i)
		}
	}
	return matched, nil
}
