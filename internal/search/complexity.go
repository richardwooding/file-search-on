package search

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/richardwooding/file-search-on/internal/content"
)

// FunctionComplexity is one function's cyclomatic complexity + line span.
type FunctionComplexity struct {
	Path       string `json:"path"`
	Function   string `json:"function"`
	Complexity int    `json:"complexity"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Lines      int    `json:"lines"`
}

// ComplexityReport is the aggregate result of Complexity: per-function
// rows sorted by complexity descending (ties broken by path, then line),
// capped by top. Cancelled / CancellationReason mirror the other
// walk-based aggregators.
type ComplexityReport struct {
	TotalFunctions     int64                `json:"total_functions"`
	Functions          []FunctionComplexity `json:"functions"`
	Cancelled          bool                 `json:"cancelled,omitempty"`
	CancellationReason string               `json:"cancellation_reason,omitempty"`
}

// Complexity walks opts.Root / opts.Roots and returns per-function
// cyclomatic complexity, sorted worst-first. Reads the builder-internal
// `complexity_rows` produced by the source extractors (Go + the
// tree-sitter languages). top caps the returned rows (<= 0 → 50).
// Modelled on FindDuplicates.
func Complexity(ctx context.Context, opts Options, registry *content.Registry, top int) (*ComplexityReport, error) {
	if top <= 0 {
		top = 50
	}
	opts.IncludeAttributes = true
	opts.Sort = ""
	opts.Order = ""
	opts.Limit = 0
	opts.IncludeSnippet = false
	opts.IncludeBody = false

	results, walkErr := Walk(ctx, opts, registry)

	out := &ComplexityReport{}
	for _, r := range results {
		if r.Attrs == nil {
			continue
		}
		rows, _ := r.Attrs.Extra["complexity_rows"].([]string)
		for _, row := range rows {
			// "func\x00complexity\x00startLine\x00endLine"
			p := strings.SplitN(row, "\x00", 4)
			if len(p) < 4 {
				continue
			}
			cx, err := strconv.Atoi(p[1])
			if err != nil {
				continue
			}
			start, _ := strconv.Atoi(p[2])
			end, _ := strconv.Atoi(p[3])
			lines := max(end-start+1, 0)
			out.Functions = append(out.Functions, FunctionComplexity{
				Path:       r.Path,
				Function:   p[0],
				Complexity: cx,
				StartLine:  start,
				EndLine:    end,
				Lines:      lines,
			})
		}
	}
	out.TotalFunctions = int64(len(out.Functions))

	sort.Slice(out.Functions, func(i, j int) bool {
		a, b := out.Functions[i], out.Functions[j]
		if a.Complexity != b.Complexity {
			return a.Complexity > b.Complexity
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.StartLine < b.StartLine
	})
	if len(out.Functions) > top {
		out.Functions = out.Functions[:top]
	}

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
