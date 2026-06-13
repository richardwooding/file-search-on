package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// CouplingInput is the JSON-schema input for the `coupling` tool.
type CouplingInput struct {
	codeGraphWalkInput
	Top int `json:"top,omitempty" jsonschema:"Cap the number of packages returned (ranked most-depended-upon then most unstable). 0 (default) returns all."`
}

// CouplingOutput is the per-package coupling report.
type CouplingOutput struct {
	CommonOutput
	Module             string                   `json:"module"`
	Packages           []search.PackageCoupling `json:"packages"`
	Count              int                      `json:"count"`
	Cancelled          bool                     `json:"cancelled,omitempty"`
	CancellationReason string                   `json:"cancellation_reason,omitempty"`
}

func (h *handlers) couplingHandler(ctx context.Context, _ *mcp.CallToolRequest, in CouplingInput) (*mcp.CallToolResult, CouplingOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, CouplingOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.Coupling(ctx, opts, in.Top, content.DefaultRegistry())
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, CouplingOutput{}, fmt.Errorf("coupling: %w", err)
	}

	out := CouplingOutput{Packages: []search.PackageCoupling{}}
	if res != nil {
		out.Module = res.Module
		out.Cancelled = res.Cancelled
		out.CancellationReason = res.CancellationReason
		if res.Packages != nil {
			out.Packages = res.Packages
		}
		out.Count = len(out.Packages)
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
