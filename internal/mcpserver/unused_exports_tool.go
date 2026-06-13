package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// UnusedExportsInput is the JSON-schema input for the `unused_exports` tool.
type UnusedExportsInput struct {
	codeGraphWalkInput
}

// UnusedExportsOutput lists exported symbols referenced only intra-package.
type UnusedExportsOutput struct {
	CommonOutput
	Module             string                `json:"module"`
	Candidates         []search.UnusedExport `json:"candidates"`
	Count              int                   `json:"count"`
	Cancelled          bool                  `json:"cancelled,omitempty"`
	CancellationReason string                `json:"cancellation_reason,omitempty"`
}

func (h *handlers) unusedExportsHandler(ctx context.Context, _ *mcp.CallToolRequest, in UnusedExportsInput) (*mcp.CallToolResult, UnusedExportsOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, UnusedExportsOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.UnusedExports(ctx, opts, content.DefaultRegistry())
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, UnusedExportsOutput{}, fmt.Errorf("unused_exports: %w", err)
	}

	out := UnusedExportsOutput{Candidates: []search.UnusedExport{}}
	if res != nil {
		out.Module = res.Module
		out.Cancelled = res.Cancelled
		out.CancellationReason = res.CancellationReason
		if res.Candidates != nil {
			out.Candidates = res.Candidates
		}
		out.Count = len(out.Candidates)
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
