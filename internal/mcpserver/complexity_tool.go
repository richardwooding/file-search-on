package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ComplexityInput is the JSON-schema input for the `complexity` tool.
type ComplexityInput struct {
	codeGraphWalkInput
	Top int `json:"top,omitempty" jsonschema:"Cap on the number of functions returned (worst-first). Defaults to 50."`
}

// ComplexityOutput is the per-function complexity report.
type ComplexityOutput struct {
	CommonOutput
	Functions          []search.FunctionComplexity `json:"functions"`
	TotalFunctions     int64                        `json:"total_functions"`
	Cancelled          bool                         `json:"cancelled,omitempty"`
	CancellationReason string                       `json:"cancellation_reason,omitempty"`
}

func (h *handlers) complexityHandler(ctx context.Context, _ *mcp.CallToolRequest, in ComplexityInput) (*mcp.CallToolResult, ComplexityOutput, error) {
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, ComplexityOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	rep, err := search.Complexity(ctx, opts, content.DefaultRegistry(), in.Top)
	if err != nil {
		return nil, ComplexityOutput{}, fmt.Errorf("complexity: %w", err)
	}
	out := ComplexityOutput{
		Functions:          rep.Functions,
		TotalFunctions:     rep.TotalFunctions,
		Cancelled:          rep.Cancelled,
		CancellationReason: rep.CancellationReason,
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
