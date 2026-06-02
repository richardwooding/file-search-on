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

// FindNearDuplicatesInput is the JSON-schema input for the
// `find_near_duplicates` tool.
type FindNearDuplicatesInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"Optional CEL expression to scope candidates (e.g. 'is_markdown' for note dedup, 'is_source && language == \"go\"' for code dedup). Same CEL surface as search. Empty means every file."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to walk. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to dedup across in one call."`
	Threshold        float64  `json:"threshold,omitempty" jsonschema:"Minimum similarity (0..1) at which two files are considered near-duplicates. Omit / 0 lets the engine pick: 0.92 for source-dominated trees (Go idioms — func/err/nil/ctx — keep raw SimHash similarity high regardless of content), 0.85 elsewhere (the prose / markdown default). 0.95 ≈ 3 bits Hamming distance (typo / whitespace edits only). 0.75 ≈ 16 bits (significant structural overlap)."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes     int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap (bytes). 0 uses the 1 MiB default."`
	BodyMaxBytes     int      `json:"body_max_bytes,omitempty" jsonschema:"Cap on the body read per file in bytes. 0 uses the 1 MiB default. Files larger than the cap are silently truncated; the prefix still participates in the fingerprint."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool. First runs are expensive (body extraction + SimHash compute on every candidate) — pair with a generous timeout. Repeat runs benefit from the per-process attribute cache."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks   bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default."`
	MinSize              int64 `json:"min_size,omitempty" jsonschema:"Skip files smaller than this many bytes (on-disk size, not extracted body)."`
	MembersLimitPerGroup int   `json:"members_limit_per_group,omitempty" jsonschema:"Cap the per-group members list to this many entries. 0 (default) returns every member. Members are sorted by similarity descending before truncation so the survivors are the strongest matches. When a group is truncated, members_total + members_truncated are stamped on the group so the caller knows there's more to drill into. Useful for triage queries that just want a top-N preview per cluster. Issue #279."`
	GroupLimit           int   `json:"group_limit,omitempty" jsonschema:"Cap the number of groups returned. 0 (default) returns every group. Groups are sorted by member count desc / representative size desc before truncation, so the largest / most-interesting clusters are kept. Pair with members_limit_per_group for bounded responses on huge corpora. Issue #279."`
}

// FindNearDuplicatesOutput mirrors search.NearDuplicates with a
// JSON-tagged shape suitable for MCP clients.
type FindNearDuplicatesOutput struct {
	CommonOutput
	TotalFiles         int64                    `json:"total_files"`
	FingerPrinted      int64                    `json:"fingerprinted"`
	GroupCount         int64                    `json:"group_count"`
	Threshold          float64                  `json:"threshold"`
	Groups             []NearDuplicateGroupWire `json:"groups"`
	Cancelled          bool                     `json:"cancelled,omitempty"`
	CancellationReason string                   `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64                  `json:"elapsed_seconds,omitempty"`
}

// NearDuplicateGroupWire is one group on the wire. Members are
// sorted similarity-desc so the representative leads. When the
// caller passed members_limit_per_group AND the group had more
// members, MembersTotal carries the pre-truncation count and
// MembersTruncated is true — the caller can drill in via a fresh
// call against the representative if needed. Issue #279.
type NearDuplicateGroupWire struct {
	Representative   string                    `json:"representative"`
	Fingerprint      string                    `json:"fingerprint"`
	Count            int                       `json:"count"`
	Members          []NearDuplicateMemberWire `json:"members"`
	MembersTotal     int                       `json:"members_total,omitempty"`
	MembersTruncated bool                      `json:"members_truncated,omitempty"`
}

// NearDuplicateMemberWire is one file inside a near-duplicate group.
type NearDuplicateMemberWire struct {
	Path       string  `json:"path"`
	Size       int64   `json:"size"`
	Similarity float64 `json:"similarity"`
}

func (h *handlers) findNearDuplicatesHandler(ctx context.Context, _ *mcp.CallToolRequest, in FindNearDuplicatesInput) (*mcp.CallToolResult, FindNearDuplicatesOutput, error) {
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, FindNearDuplicatesOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, FindNearDuplicatesOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" && len(dirs) == 0 {
		dir = "."
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, FindNearDuplicatesOutput{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, FindNearDuplicatesOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, FindNearDuplicatesOutput{}, err
	}

	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()
	dups, err := search.FindNearDuplicates(ctx, search.Options{
		Root:                dir,
		Roots:               dirs,
		Expr:                expr,
		Workers:             in.Workers,
		MaxLineBytes:        in.MaxLineBytes,
		BodyMaxBytes:        in.BodyMaxBytes,
		Index:               h.idx,
		Excludes:            in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		FollowSymlinks:      in.FollowSymlinks,
		MinSize:             in.MinSize,
		SimilarityThreshold: in.Threshold,
		NearDupMembersLimit: in.MembersLimitPerGroup,
		NearDupGroupLimit:   in.GroupLimit,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, FindNearDuplicatesOutput{}, fmt.Errorf("find_near_duplicates: %w", err)
	}

	out := FindNearDuplicatesOutput{ElapsedSeconds: elapsed}
	if dups != nil {
		out.TotalFiles = dups.TotalFiles
		out.FingerPrinted = dups.FingerPrinted
		out.GroupCount = dups.GroupCount
		out.Threshold = dups.Threshold
		out.Cancelled = dups.Cancelled
		out.CancellationReason = dups.CancellationReason
		out.Groups = make([]NearDuplicateGroupWire, len(dups.Groups))
		for i, g := range dups.Groups {
			members := make([]NearDuplicateMemberWire, len(g.Members))
			for j, m := range g.Members {
				members[j] = NearDuplicateMemberWire{
					Path:       m.Path,
					Size:       m.Size,
					Similarity: m.Similarity,
				}
			}
			out.Groups[i] = NearDuplicateGroupWire{
				Representative:   g.Representative,
				Fingerprint:      g.Fingerprint,
				Count:            g.Count,
				Members:          members,
				MembersTotal:     g.MembersTotal,
				MembersTruncated: g.MembersTruncated,
			}
		}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
