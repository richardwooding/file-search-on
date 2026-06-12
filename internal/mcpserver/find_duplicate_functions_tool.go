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

// FindDuplicateFunctionsInput is the JSON-schema input for the
// `find_duplicate_functions` tool.
type FindDuplicateFunctionsInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"Optional CEL pre-filter scoping which source files are scanned (e.g. 'language == \"go\"'). Defaults to 'is_source'. Non-source files have no function spans and contribute nothing."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to walk. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to scan in one call."`
	Threshold        float64  `json:"threshold,omitempty" jsonschema:"Minimum SimHash similarity (0..1) for two functions to cluster as near-duplicates. Omit / 0 uses 0.92 (code SimHash sits high even for unrelated functions, so this is tighter than the prose default). 0.95 ≈ near-identical; 0.85 ≈ structurally similar."`
	MinLines         int      `json:"min_lines,omitempty" jsonschema:"Skip functions shorter than this many lines. 0 uses the default 5 — filters trivial getters / one-line wrappers whose fingerprints collapse together as noise."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Parallel walk workers. Defaults to runtime.NumCPU()."`
	BodyMaxBytes     int      `json:"body_max_bytes,omitempty" jsonschema:"Cap on the body read per file in bytes. 0 uses the 1 MiB default."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. The grouping pass is O(N²) over scanned functions — pair large trees with a generous timeout."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks   bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default."`
}

// FindDuplicateFunctionsOutput mirrors search.DuplicateFunctions on the wire.
type FindDuplicateFunctionsOutput struct {
	CommonOutput
	TotalFiles         int64                           `json:"total_files"`
	FunctionsScanned   int64                           `json:"functions_scanned"`
	GroupCount         int64                           `json:"group_count"`
	Threshold          float64                         `json:"threshold"`
	MinLines           int                             `json:"min_lines"`
	Groups             []search.DuplicateFunctionGroup `json:"groups"`
	Cancelled          bool                            `json:"cancelled,omitempty"`
	CancellationReason string                          `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64                         `json:"elapsed_seconds,omitempty"`
}

func (h *handlers) findDuplicateFunctionsHandler(ctx context.Context, _ *mcp.CallToolRequest, in FindDuplicateFunctionsInput) (*mcp.CallToolResult, FindDuplicateFunctionsOutput, error) {
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, FindDuplicateFunctionsOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, FindDuplicateFunctionsOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" && len(dirs) == 0 {
		dir = "."
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, FindDuplicateFunctionsOutput{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, FindDuplicateFunctionsOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, FindDuplicateFunctionsOutput{}, err
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()
	dups, err := search.FindDuplicateFunctions(ctx, search.Options{
		Root:                dir,
		Roots:               dirs,
		Expr:                in.Expr,
		Workers:             in.Workers,
		BodyMaxBytes:        in.BodyMaxBytes,
		Index:               h.idx,
		Excludes:            in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		FollowSymlinks:      in.FollowSymlinks,
		SimilarityThreshold: in.Threshold,
		DupFuncMinLines:     in.MinLines,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, FindDuplicateFunctionsOutput{}, fmt.Errorf("find_duplicate_functions: %w", err)
	}

	out := FindDuplicateFunctionsOutput{ElapsedSeconds: elapsed}
	if dups != nil {
		out.TotalFiles = dups.TotalFiles
		out.FunctionsScanned = dups.FunctionsScanned
		out.GroupCount = dups.GroupCount
		out.Threshold = dups.Threshold
		out.MinLines = dups.MinLines
		out.Groups = dups.Groups
		out.Cancelled = dups.Cancelled
		out.CancellationReason = dups.CancellationReason
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
