package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ListPresetsInput has no fields; the tool is a pure catalogue read.
type ListPresetsInput struct{}

// PresetSummary is the per-preset entry returned by list_presets.
type PresetSummary struct {
	Name        string `json:"name" jsonschema:"Canonical preset identifier — pass to query_preset to run."`
	Description string `json:"description" jsonschema:"One-line human summary of what the preset finds."`
}

// ListPresetsOutput is the catalogue, sorted alphabetically by name.
type ListPresetsOutput struct {
	CommonOutput
	Presets []PresetSummary `json:"presets"`
}

// QueryPresetInput drives query_preset. Same dir/dirs/excludes/...
// semantics as the search tool; preset name is the discriminator.
type QueryPresetInput struct {
	Name             string   `json:"name" jsonschema:"Preset name. Call list_presets to discover available recipes."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to walk. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multi-root walk. Takes precedence over 'dir' when non-empty."`
	Limit            int      `json:"limit,omitempty" jsonschema:"Override the preset's default limit. 0 means use the preset's default (which itself may be 0 = unlimited)."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Basename globs pruned from the walk."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"Parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks   bool     `json:"follow_symlinks,omitempty" jsonschema:"Descend through symbolic links to directories."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout."`
}

func (h *handlers) listPresetsHandler(_ context.Context, _ *mcp.CallToolRequest, _ ListPresetsInput) (*mcp.CallToolResult, ListPresetsOutput, error) {
	all := search.Presets()
	out := ListPresetsOutput{
		CommonOutput: CommonOutput{ServerVersion: h.version},
		Presets:      make([]PresetSummary, 0, len(all)),
	}
	for _, p := range all {
		out.Presets = append(out.Presets, PresetSummary{Name: p.Name, Description: p.Description})
	}
	sort.Slice(out.Presets, func(i, j int) bool { return out.Presets[i].Name < out.Presets[j].Name })
	return nil, out, nil
}

func (h *handlers) queryPresetHandler(ctx context.Context, _ *mcp.CallToolRequest, in QueryPresetInput) (*mcp.CallToolResult, SearchOutput, error) {
	preset := search.PresetByName(in.Name)
	if preset == nil {
		names := search.PresetNames()
		return nil, SearchOutput{}, fmt.Errorf("unknown preset %q; available: %v (call list_presets for descriptions)", in.Name, names)
	}

	opts := preset.Build()

	dir := in.Dir
	if dir == "" && len(in.Dirs) == 0 {
		dir = "."
	}

	walkOpts := search.Options{
		Root:              dir,
		Roots:             in.Dirs,
		Expr:              opts.Expr,
		Sort:              opts.Sort,
		Order:             opts.Order,
		RankExpr:          opts.RankExpr,
		Limit:             opts.Limit,
		IncludeAttributes: true,
		Index:             h.idx,
		IncludeBody:       opts.IncludeBody,
		ComputeHashes:     opts.ComputeHashes,
		CheckDisguised:    opts.CheckDisguised,
		Excludes:          in.Excludes,
		RespectGitignore:  in.RespectGitignore,
		FollowSymlinks:    in.FollowSymlinks,
	}
	if in.Limit != 0 {
		walkOpts.Limit = in.Limit
	}

	if in.TimeoutSeconds != nil && *in.TimeoutSeconds < 0 {
		return nil, SearchOutput{}, fmt.Errorf("timeout_seconds must be non-negative, got %g", *in.TimeoutSeconds)
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()
	results, walkErr := search.Walk(ctx, walkOpts, content.DefaultRegistry())
	elapsed := time.Since(start).Seconds()

	cancelled := errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded)
	if walkErr != nil && !cancelled {
		return nil, SearchOutput{}, fmt.Errorf("preset walk: %w", walkErr)
	}

	matches := make([]search.Match, len(results))
	for i, r := range results {
		matches[i] = search.MatchFrom(r)
	}

	out := SearchOutput{
		Matches:        matches,
		Count:          len(matches),
		ElapsedSeconds: elapsed,
	}
	if cancelled {
		out.Cancelled = true
		if errors.Is(walkErr, context.DeadlineExceeded) {
			out.CancellationReason = "timeout"
		} else {
			out.CancellationReason = "client_cancel"
		}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
