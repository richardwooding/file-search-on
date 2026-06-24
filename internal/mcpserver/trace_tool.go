package mcpserver

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/search"
)

// TraceInput is the JSON-schema input for the `trace` tool.
type TraceInput struct {
	codeGraphWalkInput
	Symbol      string `json:"symbol" jsonschema:"The function / method name to trace (e.g. 'ServeHTTP'). Required. Name-based — same-name symbols across packages are conflated."`
	ImpactDepth int    `json:"impact_depth,omitempty" jsonschema:"Also include the transitive caller closure (blast radius) up to this many hops. 0 (default) omits it; use the 'impact' tool for the full closure."`
}

// TraceOutput is the both-directions call-graph view: callers + callees (+
// optional transitive caller closure) for one symbol.
type TraceOutput struct {
	CommonOutput
	Symbol             string              `json:"symbol"`
	DefinedOn          []string            `json:"defined_on,omitempty"`
	Callers            []search.Importer   `json:"callers"`
	Callees            []string            `json:"callees"`
	Impact             []search.ImpactNode `json:"impact,omitempty"`
	CallersCount       int                 `json:"callers_count"`
	CalleesCount       int                 `json:"callees_count"`
	TotalFiles         int64               `json:"total_files"`
	Cancelled          bool                `json:"cancelled,omitempty"`
	CancellationReason string              `json:"cancellation_reason,omitempty"`
}

func (h *handlers) traceHandler(ctx context.Context, _ *mcp.CallToolRequest, in TraceInput) (*mcp.CallToolResult, TraceOutput, error) {
	if in.Symbol == "" {
		return nil, TraceOutput{}, fmt.Errorf("symbol is required")
	}
	opts, err := h.codeGraphOptions(in.codeGraphWalkInput)
	if err != nil {
		return nil, TraceOutput{}, err
	}
	ctx, cancel := h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	g, err := search.BuildCodeGraph(ctx, opts, content.DefaultRegistry())
	if err != nil {
		return nil, TraceOutput{}, fmt.Errorf("trace: %w", err)
	}

	callers := g.WhoCalls(in.Symbol)
	callees := g.Calls(in.Symbol)
	out := TraceOutput{
		Symbol:             in.Symbol,
		DefinedOn:          g.OwnersOf(in.Symbol),
		Callers:            callers,
		Callees:            callees,
		CallersCount:       len(callers),
		CalleesCount:       len(callees),
		TotalFiles:         g.TotalFiles,
		Cancelled:          g.Cancelled,
		CancellationReason: g.CancellationReason,
	}
	if in.ImpactDepth > 0 {
		out.Impact = g.Impact(in.Symbol, in.ImpactDepth)
	}
	if out.Callers == nil {
		out.Callers = []search.Importer{}
	}
	if out.Callees == nil {
		out.Callees = []string{}
	}
	out.ServerVersion = h.version
	return nil, out, nil
}
