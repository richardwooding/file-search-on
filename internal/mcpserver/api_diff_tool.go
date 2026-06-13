package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// APIDiffInput is the JSON-schema input for the `api_diff` tool.
type APIDiffInput struct {
	TreeA               string   `json:"tree_a" jsonschema:"Baseline tree — the 'before' / released side. Required."`
	TreeB               string   `json:"tree_b" jsonschema:"Candidate tree — the 'after' / proposed side. Required."`
	Expr                string   `json:"expr,omitempty" jsonschema:"CEL pre-filter scoping which files enter each graph. Defaults to 'is_source'. Narrow it to a language for accuracy, e.g. 'is_source && language == \"go\"'. Same vocabulary as the search tool."`
	Workers             int      `json:"workers,omitempty" jsonschema:"Parallel walk workers per tree. Defaults to runtime.NumCPU()."`
	Excludes            []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against basenames; matches are pruned from both trees."`
	RespectGitignore    bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at each tree root and skip matching paths."`
	FollowSymlinks      bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default."`
	PruneBuildArtefacts bool     `json:"prune_build_artefacts,omitempty" jsonschema:"When true, prune canonical build-artefact dirs (vendor / node_modules / target / __pycache__ / …) from both trees."`
	TimeoutSeconds      *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout. Positive = seconds, 0 = no timeout."`
}

// APIDiffOutput is the exported-symbol delta between the two trees.
type APIDiffOutput struct {
	CommonOutput
	TreeA        string             `json:"tree_a"`
	TreeB        string             `json:"tree_b"`
	Breaking     bool               `json:"breaking"`
	Removed      []search.APISymbol `json:"removed"`
	Added        []search.APISymbol `json:"added"`
	RemovedCount int                `json:"removed_count"`
	AddedCount   int                `json:"added_count"`
	ExportedA    int                `json:"exported_a"`
	ExportedB    int                `json:"exported_b"`
}

func (h *handlers) apiDiffHandler(ctx context.Context, _ *mcp.CallToolRequest, in APIDiffInput) (*mcp.CallToolResult, APIDiffOutput, error) {
	treeA, err := expandHomeDir(in.TreeA)
	if err != nil {
		return nil, APIDiffOutput{}, fmt.Errorf("expand tree_a: %w", err)
	}
	treeB, err := expandHomeDir(in.TreeB)
	if err != nil {
		return nil, APIDiffOutput{}, fmt.Errorf("expand tree_b: %w", err)
	}
	if treeA == "" || treeB == "" {
		return nil, APIDiffOutput{}, fmt.Errorf("tree_a and tree_b are both required")
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, APIDiffOutput{}, err
	}
	if treeA, err = h.validatePath(treeA); err != nil {
		return nil, APIDiffOutput{}, err
	}
	if treeB, err = h.validatePath(treeB); err != nil {
		return nil, APIDiffOutput{}, err
	}

	expr := in.Expr
	if expr == "" {
		expr = "is_source"
	}
	mkOpts := func(root string) search.Options {
		return search.Options{
			Root:                root,
			Expr:                expr,
			Workers:             in.Workers,
			Index:               h.idx,
			Excludes:            in.Excludes,
			RespectGitignore:    in.RespectGitignore,
			FollowSymlinks:      in.FollowSymlinks,
			PruneBuildArtefacts: in.PruneBuildArtefacts,
		}
	}

	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.APIDiff(ctx, mkOpts(treeA), mkOpts(treeB), content.DefaultRegistry())
	if err != nil {
		return nil, APIDiffOutput{}, fmt.Errorf("api_diff: %w", err)
	}

	out := APIDiffOutput{
		TreeA:        treeA,
		TreeB:        treeB,
		Breaking:     res.Breaking,
		Removed:      res.Removed,
		Added:        res.Added,
		RemovedCount: res.RemovedCount,
		AddedCount:   res.AddedCount,
		ExportedA:    res.ExportedA,
		ExportedB:    res.ExportedB,
	}
	if out.Removed == nil {
		out.Removed = []search.APISymbol{}
	}
	if out.Added == nil {
		out.Added = []search.APISymbol{}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
