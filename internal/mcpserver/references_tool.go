package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReferencesInput is the JSON-schema input for the `references` tool.
type ReferencesInput struct {
	codeGraphWalkInput
	Symbol string `json:"symbol" jsonschema:"The exact function / type name to find all usages of (e.g. 'BuildCodeGraph', 'Widget'). Required. Name-based: a call pkg.Foo() or a type pkg.T is keyed by 'Foo' / 'T'."`
	Kind   string `json:"kind,omitempty" jsonschema:"Filter the usage kind: 'call' (call site), 'type' (used as a field / parameter / return / variable / generic-arg type), or 'value' (Go function value passed as an argument). Empty returns all kinds."`
}

// ReferencesOutput is the find-all-usages report.
type ReferencesOutput struct {
	CommonOutput
	Symbol             string                 `json:"symbol"`
	Kind               string                 `json:"kind,omitempty"`
	References         []search.ReferenceSite `json:"references"`
	Count              int                    `json:"count"`
	TotalFiles         int64                  `json:"total_files"`
	Cancelled          bool                   `json:"cancelled,omitempty"`
	CancellationReason string                 `json:"cancellation_reason,omitempty"`
}

func (h *handlers) referencesHandler(ctx context.Context, _ *mcp.CallToolRequest, in ReferencesInput) (*mcp.CallToolResult, ReferencesOutput, error) {
	if in.Symbol == "" {
		return nil, ReferencesOutput{}, fmt.Errorf("symbol is required")
	}
	if in.Kind != "" && in.Kind != "call" && in.Kind != "type" && in.Kind != "value" {
		return nil, ReferencesOutput{}, fmt.Errorf(`kind must be "call", "type", "value", or empty`)
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, ReferencesOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	res, err := search.References(ctx, opts, in.Symbol, in.Kind, content.DefaultRegistry())
	if err != nil {
		return nil, ReferencesOutput{}, fmt.Errorf("references: %w", err)
	}
	out := ReferencesOutput{
		Symbol:             res.Symbol,
		Kind:               in.Kind,
		References:         res.References,
		Count:              res.Count,
		TotalFiles:         res.TotalFiles,
		Cancelled:          res.Cancelled,
		CancellationReason: res.CancellationReason,
	}
	if out.References == nil {
		out.References = []search.ReferenceSite{}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
