package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// DiffTreesInput is the JSON-schema input for the `diff_trees` tool.
type DiffTreesInput struct {
	TreeA            string   `json:"tree_a" jsonschema:"First tree (the 'A' side). Required."`
	TreeB            string   `json:"tree_b" jsonschema:"Second tree (the 'B' side). Required."`
	Op               string   `json:"op,omitempty" jsonschema:"Set operation by sha256 content hash: 'a-minus-b' (default — content in A but not B), 'b-minus-a' (in B but not A), 'intersect' (in both), 'union' (all distinct content across both), 'mismatch' (files sharing a relative path but with differing content — drift detection)."`
	Expr             string   `json:"expr,omitempty" jsonschema:"Optional CEL expression scoping which files are considered in BOTH trees before hashing (same vocabulary as the search tool, e.g. 'size > 1000000' or 'is_image'). Empty means every file."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Parallel workers per tree walk. Defaults to runtime.NumCPU()."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. On expiry the partial result is returned with cancelled=true. Cold diffs hash every candidate file — pair with a generous timeout for first runs."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned from both trees."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at each tree root and skip matching paths."`
	FollowSymlinks   bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default."`
	MinSize          int64    `json:"min_size,omitempty" jsonschema:"Skip files smaller than this many bytes in both trees."`
}

// DiffTreesOutput is the structured output of `diff_trees`. Records is
// sorted deterministically by (path_a, path_b, sha256).
type DiffTreesOutput struct {
	CommonOutput
	Op                 string              `json:"op"`
	Records            []search.DiffRecord `json:"records"`
	Count              int                 `json:"count"`
	TotalA             int                 `json:"total_a"`
	TotalB             int                 `json:"total_b"`
	Cancelled          bool                `json:"cancelled,omitempty"`
	CancellationReason string              `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64             `json:"elapsed_seconds,omitempty"`
}

func (h *handlers) diffTreesHandler(ctx context.Context, _ *mcp.CallToolRequest, in DiffTreesInput) (*mcp.CallToolResult, DiffTreesOutput, error) {
	treeA, err := expandHomeDir(in.TreeA)
	if err != nil {
		return nil, DiffTreesOutput{}, fmt.Errorf("expand tree_a: %w", err)
	}
	treeB, err := expandHomeDir(in.TreeB)
	if err != nil {
		return nil, DiffTreesOutput{}, fmt.Errorf("expand tree_b: %w", err)
	}
	if treeA == "" || treeB == "" {
		return nil, DiffTreesOutput{}, fmt.Errorf("tree_a and tree_b are both required")
	}
	op := in.Op
	if op == "" {
		op = string(search.OpAMinusB)
	}
	if !search.ValidDiffOp(op) {
		return nil, DiffTreesOutput{}, fmt.Errorf("invalid op %q: want one of a-minus-b, b-minus-a, intersect, union, mismatch", op)
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()
	res, err := search.DiffTrees(ctx, treeA, treeB, search.DiffOp(op), search.Options{
		Expr:             in.Expr,
		Workers:          in.Workers,
		Index:            h.idx,
		Excludes:         in.Excludes,
		RespectGitignore: in.RespectGitignore,
		FollowSymlinks:   in.FollowSymlinks,
		MinSize:          in.MinSize,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, DiffTreesOutput{}, fmt.Errorf("diff_trees: %w", err)
	}

	out := DiffTreesOutput{Op: op, ElapsedSeconds: elapsed, Records: []search.DiffRecord{}}
	if res != nil {
		out.Op = res.Op
		out.TotalA = res.TotalA
		out.TotalB = res.TotalB
		out.Count = res.Count
		out.Cancelled = res.Cancelled
		out.CancellationReason = res.CancellationReason
		if res.Records != nil {
			out.Records = res.Records
		}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
