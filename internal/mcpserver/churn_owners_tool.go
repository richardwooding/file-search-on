package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ChurnOwnersInput is the JSON-schema input for the `churn_owners` tool.
type ChurnOwnersInput struct {
	Expr                string   `json:"expr,omitempty" jsonschema:"CEL pre-filter for which files are counted. Defaults to 'is_git_tracked' (every tracked file — source, docs, config). Narrow with e.g. 'is_source' for code-only ownership. Same vocabulary as the search tool."`
	Dir                 string   `json:"dir,omitempty" jsonschema:"Directory to analyse. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs                []string `json:"dirs,omitempty" jsonschema:"Multiple directories analysed in one report. When non-empty, takes precedence over 'dir'."`
	MinFiles            int      `json:"min_files,omitempty" jsonschema:"Drop directories with fewer than this many matching files. Defaults to 1 (keep all). Raise it to focus on substantial subtrees."`
	Workers             int      `json:"workers,omitempty" jsonschema:"Parallel walk workers. Defaults to runtime.NumCPU()."`
	Excludes            []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against basenames; matches are pruned."`
	RespectGitignore    bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root and skip matching paths."`
	FollowSymlinks      bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default."`
	PruneBuildArtefacts bool     `json:"prune_build_artefacts,omitempty" jsonschema:"When true, prune canonical build-artefact dirs (vendor / node_modules / target / __pycache__ / …)."`
	TimeoutSeconds      *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. On expiry the partial report is returned with cancelled=true."`
}

// ChurnOwnersOutput is the directory-ownership report.
type ChurnOwnersOutput struct {
	CommonOutput
	Dirs               []search.ChurnOwnerDir `json:"dirs"`
	TotalFiles         int                    `json:"total_files"`
	Cancelled          bool                   `json:"cancelled,omitempty"`
	CancellationReason string                 `json:"cancellation_reason,omitempty"`
}

func (h *handlers) churnOwnersHandler(ctx context.Context, _ *mcp.CallToolRequest, in ChurnOwnersInput) (*mcp.CallToolResult, ChurnOwnersOutput, error) {
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, ChurnOwnersOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, ChurnOwnersOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" && len(dirs) == 0 {
		dir = "."
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, ChurnOwnersOutput{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, ChurnOwnersOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, ChurnOwnersOutput{}, err
	}

	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.ChurnOwners(ctx, search.Options{
		Root:                dir,
		Roots:               dirs,
		Expr:                in.Expr,
		Workers:             in.Workers,
		Index:               h.idx,
		Excludes:            in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		FollowSymlinks:      in.FollowSymlinks,
		PruneBuildArtefacts: in.PruneBuildArtefacts,
	}, in.MinFiles, content.DefaultRegistry())
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, ChurnOwnersOutput{}, fmt.Errorf("churn_owners: %w", err)
	}

	out := ChurnOwnersOutput{Dirs: []search.ChurnOwnerDir{}}
	if res != nil {
		out.TotalFiles = res.TotalFiles
		out.Cancelled = res.Cancelled
		out.CancellationReason = res.CancellationReason
		if res.Dirs != nil {
			out.Dirs = res.Dirs
		}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
