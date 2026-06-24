package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// CircularInput is the JSON-schema input for the `circular` tool. It reuses
// the shared code-graph walk surface (dir / dirs / excludes / timeout, #467).
type CircularInput struct {
	codeGraphWalkInput
}

// CircularOutput is the circular-dependency report.
type CircularOutput struct {
	CommonOutput
	Module             string         `json:"module"`
	Cycles             []search.Cycle `json:"cycles"`
	Count              int            `json:"count"`
	Cancelled          bool           `json:"cancelled,omitempty"`
	CancellationReason string         `json:"cancellation_reason,omitempty"`
}

func (h *handlers) circularHandler(ctx context.Context, _ *mcp.CallToolRequest, in CircularInput) (*mcp.CallToolResult, CircularOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, CircularOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.Cycles(ctx, opts, content.DefaultRegistry())
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, CircularOutput{}, fmt.Errorf("circular: %w", err)
	}

	out := CircularOutput{Cycles: []search.Cycle{}}
	if res != nil {
		out.Module = res.Module
		out.Cancelled = res.Cancelled
		out.CancellationReason = res.CancellationReason
		if res.Cycles != nil {
			out.Cycles = res.Cycles
		}
		out.Count = len(out.Cycles)
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
