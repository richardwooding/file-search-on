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

// StatsInput is the JSON-schema input for the `stats` tool.
type StatsInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"Optional CEL expression to scope the histogram (e.g. 'is_markdown' counts only markdown files). Empty means every file. Same CEL surface as the search tool."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to walk. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to aggregate stats across in one call. When non-empty, takes precedence over 'dir'."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes     int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Same semantics as the search tool: positive = seconds, 0 = no timeout, omitted = server default. On timeout the partial histogram is returned with cancelled=true."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against file/dir basenames; matches are pruned. Same as the search tool."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root and skip matching paths."`
	FollowSymlinks      bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default; symlinks-to-dirs surface as is_symlink=true leaf entries."`
	PruneBuildArtefacts bool     `json:"prune_build_artefacts,omitempty" jsonschema:"When true, pre-walks each root to discover project subdirectories and prunes the canonical build-artefact basenames for every detected project type — vendor (Go), node_modules (Node), target (Rust / Java Maven), __pycache__/.venv/.tox (Python), bin/obj (.NET), .terraform (Terraform), etc. Unioned with 'excludes'. Saves the boilerplate exclude list when running stats over monorepos or large multi-project trees. Opt-in: pre-walk I/O is proportional to tree size. Parity with the search / find_matches / find_near_duplicates tools. Issue #277."`
	GroupBy             string   `json:"group_by,omitempty" jsonschema:"Bucket key. Default 'content_type'. Recognised: content_type, ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, frontmatter_format. Unknown values fall back to content_type. Use group_by=ext to histogram by file extension, group_by=language to count source files per language, group_by=camera_make to bucket photos by camera, etc."`
	Limit               int      `json:"limit,omitempty" jsonschema:"Cap the number of buckets returned in this page (buckets are ordered by count desc, then name asc). 0 = all. When truncated, the response carries next_cursor. Useful for high-cardinality group_by like ext / dir / language on a large tree."`
	Cursor              string   `json:"cursor,omitempty" jsonschema:"Opaque pagination token from a previous response's next_cursor. Resumes the bucket list after the last bucket of the prior page. Use the SAME group_by/expr for stable paging. total_count / total_size always reflect the full tree, not the page."`
}

// StatsOutput is the structured output of the `stats` tool — a
// histogram + totals + the standard partial-result fields
// (cancelled, cancellation_reason, elapsed_seconds) shared with
// the search tool.
//
// Groups is the bucket list keyed by the resolved group_by;
// ContentTypes is the legacy v0.20-shaped field, populated
// alongside Groups only when group_by is "content_type" / unset
// for back-compat with older agent integrations.
type StatsOutput struct {
	CommonOutput
	TotalCount         int64                    `json:"total_count"`
	TotalSize          int64                    `json:"total_size"`
	GroupBy            string                   `json:"group_by,omitempty"`
	Groups             []StatsBucket            `json:"groups"`
	ContentTypes       []StatsContentTypeBucket `json:"content_types,omitempty"`
	// NextCursor is present only when the bucket list was truncated by
	// Limit and more buckets remain. Pass it back as 'cursor' to fetch
	// the next page. Issue #336.
	NextCursor         string                   `json:"next_cursor,omitempty"`
	Cancelled          bool                     `json:"cancelled,omitempty"`
	CancellationReason string                   `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64                  `json:"elapsed_seconds,omitempty"`
	// Suggestions populated on cancellation. Issue #168 sub-feature C.
	Suggestions []string `json:"suggestions,omitempty"`
}

// StatsBucket is one row of the stats histogram. ContentTypeBucket
// is a back-compat alias for the same shape.
type StatsBucket struct {
	Name      string `json:"name"`
	Count     int64  `json:"count"`
	TotalSize int64  `json:"total_size"`
}

// StatsContentTypeBucket is the legacy bucket type kept for
// back-compat. Same shape as StatsBucket.
type StatsContentTypeBucket = StatsBucket

func (h *handlers) statsHandler(ctx context.Context, _ *mcp.CallToolRequest, in StatsInput) (*mcp.CallToolResult, StatsOutput, error) {
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, StatsOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, StatsOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" {
		dir = "."
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, StatsOutput{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, StatsOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, StatsOutput{}, err
	}

	// parentCtx separation isn't needed because ComputeStats itself
	// surfaces cancelled=true via the Stats struct rather than via the
	// ctx — we just need to apply the deadline.
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()
	stats, err := search.ComputeStats(ctx, search.Options{
		Root:                dir,
		Roots:               dirs,
		Expr:                expr,
		Workers:             in.Workers,
		MaxLineBytes:        in.MaxLineBytes,
		Index:               h.idx,
		Excludes:            in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		FollowSymlinks:      in.FollowSymlinks,
		PruneBuildArtefacts: in.PruneBuildArtefacts,
		GroupBy:             in.GroupBy,
	}, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, StatsOutput{}, fmt.Errorf("stats: %w", err)
	}

	out := StatsOutput{
		ElapsedSeconds: elapsed,
	}
	if stats != nil {
		out.TotalCount = stats.TotalCount
		out.TotalSize = stats.TotalSize
		out.GroupBy = stats.GroupBy
		out.Cancelled = stats.Cancelled
		out.CancellationReason = stats.CancellationReason
		out.Groups = make([]StatsBucket, len(stats.Groups))
		for i, b := range stats.Groups {
			out.Groups[i] = StatsBucket{
				Name:      b.Name,
				Count:     b.Count,
				TotalSize: b.TotalSize,
			}
		}
		if len(stats.ContentTypes) > 0 {
			out.ContentTypes = make([]StatsContentTypeBucket, len(stats.ContentTypes))
			for i, b := range stats.ContentTypes {
				out.ContentTypes[i] = StatsContentTypeBucket{
					Name:      b.Name,
					Count:     b.Count,
					TotalSize: b.TotalSize,
				}
			}
		}

		// Cursor pagination over the buckets (count desc, name asc — the
		// same order ComputeStats emits). total_count / total_size stay
		// whole-tree. When group_by is content_type the ContentTypes
		// mirror is trimmed to the same page so the two views agree.
		// Issue #336.
		if in.Cursor != "" || in.Limit > 0 {
			page, next, perr := search.PaginateGeneric(out.Groups, func(b StatsBucket) []any {
				return []any{b.Count, b.Name}
			}, []string{"desc", "asc"}, "stats:"+out.GroupBy, in.Cursor, in.Limit)
			if perr != nil {
				return nil, StatsOutput{}, fmt.Errorf("cursor: %w", perr)
			}
			out.Groups, out.NextCursor = page, next
			if out.ContentTypes != nil {
				out.ContentTypes = out.ContentTypes[:0]
				for _, b := range out.Groups {
					out.ContentTypes = append(out.ContentTypes, StatsContentTypeBucket(b))
				}
			}
		}
		if out.Cancelled {
			out.Suggestions = search.SuggestionsForStats(search.Options{
				Expr:             expr,
				IncludeBody:      false, // stats never sets IncludeBody
				Excludes:         in.Excludes,
				RespectGitignore: in.RespectGitignore,
			}, out.ElapsedSeconds, out.CancellationReason)
		}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
