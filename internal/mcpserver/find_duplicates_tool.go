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

// FindDuplicatesInput is the JSON-schema input for the
// `find_duplicates` tool.
type FindDuplicatesInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"Optional CEL expression to scope candidates (e.g. 'is_image' for photo dedup, 'is_archive' for archive dedup). Same CEL surface as search. Empty means every file."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to walk. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to dedup across in one call."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes     int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap (bytes). 0 uses the 1 MiB default."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool. Duplicate detection can be expensive on cold caches — pair with a generous timeout for first runs."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks   bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default. Symlinked duplicates (file A and a symlink pointing at A) are NOT collapsed — they hash to the same bytes via the resolved target and surface as a duplicate group."`
	MinSize          int64    `json:"min_size,omitempty" jsonschema:"Skip files smaller than this many bytes. Raise to e.g. 4096 to ignore tiny duplicates."`
}

// FindDuplicatesOutput is the structured output of `find_duplicates`.
type FindDuplicatesOutput struct {
	TotalFiles         int64            `json:"total_files"`
	DuplicateGroups    int64            `json:"duplicate_groups"`
	WastedBytes        int64            `json:"wasted_bytes"`
	Duplicates         []DuplicateGroup `json:"duplicates"`
	Cancelled          bool             `json:"cancelled,omitempty"`
	CancellationReason string           `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64          `json:"elapsed_seconds,omitempty"`
}

// DuplicateGroup is one row of the duplicates output. Hash is the
// sha256 hex; Size is the per-file byte count (same for every
// file in the group); WastedBytes = (Count-1) * Size — the bytes
// a dedupe would reclaim.
type DuplicateGroup struct {
	Hash        string   `json:"hash"`
	Size        int64    `json:"size"`
	Count       int      `json:"count"`
	WastedBytes int64    `json:"wasted_bytes"`
	Paths       []string `json:"paths"`
}

func (h *handlers) findDuplicatesHandler(ctx context.Context, _ *mcp.CallToolRequest, in FindDuplicatesInput) (*mcp.CallToolResult, FindDuplicatesOutput, error) {
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, FindDuplicatesOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, FindDuplicatesOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" && len(dirs) == 0 {
		dir = "."
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()
	dups, err := search.FindDuplicates(ctx, search.Options{
		Root:             dir,
		Roots:            dirs,
		Expr:             expr,
		Workers:          in.Workers,
		MaxLineBytes:     in.MaxLineBytes,
		Index:            h.idx,
		Excludes:         in.Excludes,
		RespectGitignore: in.RespectGitignore,
		FollowSymlinks:   in.FollowSymlinks,
		MinSize:          in.MinSize,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, FindDuplicatesOutput{}, fmt.Errorf("find_duplicates: %w", err)
	}

	out := FindDuplicatesOutput{ElapsedSeconds: elapsed}
	if dups != nil {
		out.TotalFiles = dups.TotalFiles
		out.DuplicateGroups = dups.DuplicateGroups
		out.WastedBytes = dups.WastedBytes
		out.Cancelled = dups.Cancelled
		out.CancellationReason = dups.CancellationReason
		out.Duplicates = make([]DuplicateGroup, len(dups.Duplicates))
		for i, g := range dups.Duplicates {
			out.Duplicates[i] = DuplicateGroup{
				Hash:        g.Hash,
				Size:        g.Size,
				Count:       g.Count,
				WastedBytes: g.WastedBytes,
				Paths:       g.Paths,
			}
		}
	}
	return nil, out, nil
}
