package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// IndexStatsOutput is the structured output of the `index_stats` tool.
// Counters are monotonic for the server process lifetime; restart resets.
//
// Body* counters report body-cache effectiveness independently of the
// attribute cache. The body cache is the bodies_v1 bbolt bucket (or
// the in-memory equivalent) populated by include_body=true searches;
// hits skip the per-file body extraction entirely.
type IndexStatsOutput struct {
	CommonOutput
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
	Puts   uint64 `json:"puts"`
	Stales uint64 `json:"stales"`
	Errors uint64 `json:"errors"`

	BodyHits      uint64 `json:"body_hits"`
	BodyMisses    uint64 `json:"body_misses"`
	BodyPuts      uint64 `json:"body_puts"`
	BodyStales    uint64 `json:"body_stales"`
	BodyEvictions uint64 `json:"body_evictions"`
	BodyOversize  uint64 `json:"body_oversize"`
	BodyErrors    uint64 `json:"body_errors"`

	EmbedHits            uint64 `json:"embed_hits"`
	EmbedMisses          uint64 `json:"embed_misses"`
	EmbedPuts            uint64 `json:"embed_puts"`
	EmbedErrors          uint64 `json:"embed_errors"`
	EmbedModelMismatches uint64 `json:"embed_model_mismatches"`
}

func (h *handlers) indexStatsHandler(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, IndexStatsOutput, error) {
	if h.idx == nil {
		return nil, IndexStatsOutput{CommonOutput: CommonOutput{ServerVersion: h.version}}, nil
	}
	st := h.idx.Stats()
	return nil, IndexStatsOutput{
		CommonOutput:  CommonOutput{ServerVersion: h.version},
		Hits:          st.Hits,
		Misses:        st.Misses,
		Puts:          st.Puts,
		Stales:        st.Stales,
		Errors:        st.Errors,
		BodyHits:      st.BodyHits,
		BodyMisses:    st.BodyMisses,
		BodyPuts:      st.BodyPuts,
		BodyStales:    st.BodyStales,
		BodyEvictions: st.BodyEvictions,
		BodyOversize:  st.BodyOversize,
		BodyErrors:    st.BodyErrors,
		EmbedHits:            st.EmbedHits,
		EmbedMisses:          st.EmbedMisses,
		EmbedPuts:            st.EmbedPuts,
		EmbedErrors:          st.EmbedErrors,
		EmbedModelMismatches: st.EmbedModelMismatches,
	}, nil
}
