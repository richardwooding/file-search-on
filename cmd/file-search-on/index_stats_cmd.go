package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/richardwooding/file-search-on/internal/index"
)

// IndexStatsCmd is the CLI counterpart to the MCP index_stats tool: open
// the attribute/body/embedding cache and report its counters without
// running a search. The persistent gauges (entry counts, bytes used) are
// the useful standalone signal — the Hits/Misses/Puts counters are
// process-lifetime monotonic, so they read as 0 right after Open.
type IndexStatsCmd struct {
	IndexPath string `name:"index-path" help:"Path to the on-disk index to inspect. Empty uses the per-cwd default (same resolution as search)."`
	NoIndex   bool   `name:"no-index" help:"Inspect a fresh in-memory index instead of an on-disk one (counters will be zero)."`
	Output    string `short:"o" name:"output" enum:"text,json" default:"text" help:"Output format: text | json."`
}

// indexStatsJSON mirrors the field names of mcpserver.IndexStatsOutput
// so the CLI and MCP JSON shapes match. The CLI adds backend/path since
// it picks the index file itself (the MCP server is handed one).
type indexStatsJSON struct {
	Backend string `json:"backend"`
	Path    string `json:"path,omitempty"`

	Hits          uint64 `json:"hits"`
	Misses        uint64 `json:"misses"`
	Puts          uint64 `json:"puts"`
	Stales        uint64 `json:"stales"`
	Errors        uint64 `json:"errors"`
	EntryOversize uint64 `json:"entry_oversize,omitempty"`

	AttrEntriesCount uint64 `json:"attr_entries_count"`
	BodyEntriesCount uint64 `json:"body_entries_count"`
	BodiesTotalBytes int64  `json:"bodies_total_bytes"`

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

func (c *IndexStatsCmd) Run(_ context.Context) error {
	idx, backend, err := openIndex(c.IndexPath, c.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return fmt.Errorf("index-stats failed to open index: %w", err)
	}
	defer func() { _ = idx.Close() }()

	st := idx.Stats()

	if c.Output == "json" {
		return writeJSON(os.Stdout, indexStatsJSON{
			Backend:              backend.Mode,
			Path:                 backend.Path,
			Hits:                 st.Hits,
			Misses:               st.Misses,
			Puts:                 st.Puts,
			Stales:               st.Stales,
			Errors:               st.Errors,
			EntryOversize:        st.EntryOversize,
			AttrEntriesCount:     st.AttrEntriesCount,
			BodyEntriesCount:     st.BodyEntriesCount,
			BodiesTotalBytes:     st.BodiesTotalBytes,
			BodyHits:             st.BodyHits,
			BodyMisses:           st.BodyMisses,
			BodyPuts:             st.BodyPuts,
			BodyStales:           st.BodyStales,
			BodyEvictions:        st.BodyEvictions,
			BodyOversize:         st.BodyOversize,
			BodyErrors:           st.BodyErrors,
			EmbedHits:            st.EmbedHits,
			EmbedMisses:          st.EmbedMisses,
			EmbedPuts:            st.EmbedPuts,
			EmbedErrors:          st.EmbedErrors,
			EmbedModelMismatches: st.EmbedModelMismatches,
		})
	}

	printIndexStatsText(os.Stdout, backend, st)
	return nil
}

func printIndexStatsText(w *os.File, backend IndexBackend, st index.Stats) {
	_, _ = fmt.Fprintf(w, "backend: %s", backend.Mode)
	if backend.Path != "" {
		_, _ = fmt.Fprintf(w, " (%s)", backend.Path)
	}
	_, _ = fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "attr entries\t%d\n", st.AttrEntriesCount)
	_, _ = fmt.Fprintf(tw, "body entries\t%d\n", st.BodyEntriesCount)
	_, _ = fmt.Fprintf(tw, "bodies size\t%s\n", humanBytes(st.BodiesTotalBytes))
	_, _ = fmt.Fprintf(tw, "attr hits/misses\t%d / %d\n", st.Hits, st.Misses)
	_, _ = fmt.Fprintf(tw, "body hits/misses\t%d / %d\n", st.BodyHits, st.BodyMisses)
	_, _ = fmt.Fprintf(tw, "embed hits/misses\t%d / %d\n", st.EmbedHits, st.EmbedMisses)
	if st.EntryOversize > 0 || st.BodyOversize > 0 || st.Errors > 0 || st.BodyErrors > 0 {
		_, _ = fmt.Fprintf(tw, "errors (attr/body)\t%d / %d\n", st.Errors, st.BodyErrors)
		_, _ = fmt.Fprintf(tw, "oversize (attr/body)\t%d / %d\n", st.EntryOversize, st.BodyOversize)
	}
	_ = tw.Flush()
}
