package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// IndexStatsOutput is the structured output of the `index_stats` tool.
// Counters are monotonic for the server process lifetime; restart resets.
type IndexStatsOutput struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
	Puts   uint64 `json:"puts"`
	Stales uint64 `json:"stales"`
	Errors uint64 `json:"errors"`
}

func (h *handlers) indexStatsHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, IndexStatsOutput, error) {
	if h.idx == nil {
		return nil, IndexStatsOutput{}, nil
	}
	st := h.idx.Stats()
	return nil, IndexStatsOutput{
		Hits:   st.Hits,
		Misses: st.Misses,
		Puts:   st.Puts,
		Stales: st.Stales,
		Errors: st.Errors,
	}, nil
}
