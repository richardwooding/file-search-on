package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// CoverageGapsInput is the JSON-schema input for the `coverage_gaps` tool.
type CoverageGapsInput struct {
	Profile        string   `json:"profile" jsonschema:"Path to a Go coverage profile produced by 'go test -coverprofile=cover.out ./...'. Required."`
	Dir            string   `json:"dir,omitempty" jsonschema:"Module root (the directory holding go.mod) used to resolve the profile's import-path filenames to files on disk. Defaults to '.'."`
	Threshold      float64  `json:"threshold,omitempty" jsonschema:"Coverage fraction in [0,1]; report functions whose covered fraction is strictly below it. 0 / omitted means 1.0 — every function not fully covered. 0.8 reports functions under 80%."`
	TimeoutSeconds *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout."`
}

// CoverageGapsOutput mirrors search.CoverageGapsResult on the wire.
type CoverageGapsOutput struct {
	CommonOutput
	ProfileMode   string               `json:"profile_mode"`
	FilesAnalysed int                  `json:"files_analysed"`
	Threshold     float64              `json:"threshold"`
	Gaps          []search.CoverageGap `json:"gaps"`
	Count         int                  `json:"count"`
}

func (h *handlers) coverageGapsHandler(ctx context.Context, _ *mcp.CallToolRequest, in CoverageGapsInput) (*mcp.CallToolResult, CoverageGapsOutput, error) {
	if in.Profile == "" {
		return nil, CoverageGapsOutput{}, fmt.Errorf("profile is required")
	}
	profile, err := expandHomeDir(in.Profile)
	if err != nil {
		return nil, CoverageGapsOutput{}, fmt.Errorf("expand profile: %w", err)
	}
	if profile, err = h.validatePath(profile); err != nil {
		return nil, CoverageGapsOutput{}, err
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, CoverageGapsOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	if dir == "" {
		dir = "."
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, CoverageGapsOutput{}, err
	}

	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.CoverageGaps(ctx, profile, dir, in.Threshold, content.DefaultRegistry())
	if err != nil {
		return nil, CoverageGapsOutput{}, fmt.Errorf("coverage_gaps: %w", err)
	}
	out := CoverageGapsOutput{
		ProfileMode:   res.ProfileMode,
		FilesAnalysed: res.FilesAnalysed,
		Threshold:     res.Threshold,
		Gaps:          res.Gaps,
		Count:         res.Count,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
