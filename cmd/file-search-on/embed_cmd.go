package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/richardwooding/ollamaembed"
	"github.com/richardwooding/file-search-on/internal/index"
)

// EmbedCmd manages Ollama embedding models — what's installed and what
// could be installed. Both subcommands run synchronously and use the
// existing OLLAMA_HOST / --server precedence shared by the MCP server's
// --embedding-server flag.
type EmbedCmd struct {
	List EmbedListCmd `cmd:"" help:"List locally-pulled and recommended embedding models."`
	Pull EmbedPullCmd `cmd:"" help:"Download an embedding model via Ollama."`
	Warm EmbedWarmCmd `cmd:"" help:"Pre-populate the search_semantic embeddings cache for a directory tree. Walks every file, embeds its body via Ollama, stores the vector under (size, mtime) in the bbolt index. Subsequent search_semantic calls hit the cache and return in milliseconds. Re-runs on unchanged trees are sub-second."`
}

// EmbedListCmd shows both arms of file-search-on's embedding-model
// awareness: the locally-pulled models reported by Ollama, and the
// curated catalog of recommended models.
type EmbedListCmd struct {
	Server string `name:"server" env:"OLLAMA_HOST" default:"http://localhost:11434" help:"Ollama base URL (overrides via OLLAMA_HOST env)."`
	Output string `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (two-section table), json (the structured shape returned by the MCP list_embedding_models tool)."`
}

type embedListJSON struct {
	Server  string       `json:"server"`
	Local   []localOut   `json:"local"`
	Catalog []catalogOut `json:"catalog"`
}

type localOut struct {
	Name        string    `json:"name"`
	SizeBytes   int64     `json:"size_bytes"`
	ModifiedAt  time.Time `json:"modified_at"`
	Digest      string    `json:"digest"`
	Catalogued  bool      `json:"catalogued"`
	Description string    `json:"description,omitempty"`
	Dimensions  int       `json:"dimensions,omitempty"`
}

type catalogOut struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Size        string `json:"size"`
	Dimensions  int    `json:"dimensions"`
	Pulled      bool   `json:"pulled"`
}

func (c *EmbedListCmd) Run(ctx context.Context) error {
	oll := ollamaembed.NewOllama(c.Server, "")
	local, err := oll.ListLocal(ctx)
	if err != nil {
		return fmt.Errorf("list ollama models: %w", err)
	}

	resp := embedListJSON{Server: c.Server, Local: []localOut{}, Catalog: []catalogOut{}}
	pulledBare := make(map[string]struct{}, len(local))
	for _, m := range local {
		bare := ollamaembed.BareName(m.Name)
		pulledBare[bare] = struct{}{}
		row := localOut{
			Name:       m.Name,
			SizeBytes:  m.Size,
			ModifiedAt: m.ModifiedAt,
			Digest:     m.Digest,
		}
		if cat := ollamaembed.CatalogLookup(bare); cat != nil {
			row.Catalogued = true
			row.Description = cat.Description
			row.Dimensions = cat.Dimensions
		}
		resp.Local = append(resp.Local, row)
	}
	for _, cat := range ollamaembed.Catalog {
		_, pulled := pulledBare[cat.Name]
		resp.Catalog = append(resp.Catalog, catalogOut{
			Name:        cat.Name,
			Description: cat.Description,
			Size:        cat.Size,
			Dimensions:  cat.Dimensions,
			Pulled:      pulled,
		})
	}
	// Sort local by name for stable display.
	sort.Slice(resp.Local, func(i, j int) bool { return resp.Local[i].Name < resp.Local[j].Name })

	switch c.Output {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	default:
		return printEmbedList(os.Stdout, resp)
	}
}

func printEmbedList(w io.Writer, r embedListJSON) error {
	_, _ = fmt.Fprintf(w, "LOCALLY PULLED  (%s)\n", r.Server)
	if len(r.Local) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")
	} else {
		for _, m := range r.Local {
			catTag := ""
			if m.Catalogued {
				catTag = "  [catalogued]"
			}
			_, _ = fmt.Fprintf(w, "  %-32s %s   modified %s%s\n",
				m.Name,
				humanBytes(m.SizeBytes),
				m.ModifiedAt.Format("2006-01-02"),
				catTag,
			)
			if m.Description != "" {
				_, _ = fmt.Fprintf(w, "    %d dims — %s\n", m.Dimensions, m.Description)
			}
		}
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "NOT YET PULLED (catalog)")
	var any bool
	for _, c := range r.Catalog {
		if c.Pulled {
			continue
		}
		any = true
		_, _ = fmt.Fprintf(w, "  %-26s %-10s %4d dims   %s\n", c.Name, c.Size, c.Dimensions, c.Description)
	}
	if !any {
		_, _ = fmt.Fprintln(w, "  (catalog fully pulled — every recommended model is installed)")
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Pull with: file-search-on embed pull <name>")
	return nil
}

// humanBytes renders a byte count as a short human string. Plenty
// of formats exist; this matches the style used by `du -h`. Returns
// "—" for non-positive sizes.
func humanBytes(n int64) string {
	if n <= 0 {
		return "—"
	}
	const k = 1024.0
	f := float64(n)
	switch {
	case f < k:
		return fmt.Sprintf("%d B", n)
	case f < k*k:
		return fmt.Sprintf("%.1f KB", f/k)
	case f < k*k*k:
		return fmt.Sprintf("%.1f MB", f/(k*k))
	default:
		return fmt.Sprintf("%.2f GB", f/(k*k*k))
	}
}

// EmbedPullCmd downloads an embedding model from Ollama, streaming
// progress to stderr (carriage-return overwrite, throttled to once
// per second) unless --quiet is passed.
type EmbedPullCmd struct {
	Name   string `arg:"" name:"name" help:"Model name to pull (e.g. nomic-embed-text). Omit tag to pull :latest."`
	Server string `name:"server" env:"OLLAMA_HOST" default:"http://localhost:11434" help:"Ollama base URL."`
	Quiet  bool   `name:"quiet" short:"q" help:"Suppress progress output."`
}

func (c *EmbedPullCmd) Run(ctx context.Context) error {
	oll := ollamaembed.NewOllama(c.Server, "")

	// Quick shortcut: if the model is already pulled, say so and exit.
	if local, err := oll.ListLocal(ctx); err == nil {
		bareWant := ollamaembed.BareName(c.Name)
		for _, m := range local {
			if ollamaembed.BareName(m.Name) == bareWant {
				_, _ = fmt.Fprintf(os.Stderr, "%s is already pulled\n", c.Name)
				return nil
			}
		}
	}

	start := time.Now()
	if !c.Quiet {
		_, _ = fmt.Fprintf(os.Stderr, "pulling %s from %s…\n", c.Name, c.Server)
	}

	var lastReport time.Time
	var lastTotal, lastCompleted int64
	progress := func(p ollamaembed.PullProgress) {
		if c.Quiet {
			return
		}
		if p.Total > 0 {
			lastTotal = p.Total
			lastCompleted = p.Completed
		}
		// Throttle to 1Hz to keep the terminal sane.
		if time.Since(lastReport) < time.Second {
			return
		}
		lastReport = time.Now()
		status := p.Status
		if len(status) > 32 {
			status = status[:32]
		}
		if lastTotal > 0 {
			pct := float64(lastCompleted) / float64(lastTotal) * 100.0
			_, _ = fmt.Fprintf(os.Stderr, "\r  %-32s %s / %s  (%5.1f%%)",
				status, humanBytes(lastCompleted), humanBytes(lastTotal), pct,
			)
		} else if status != "" {
			_, _ = fmt.Fprintf(os.Stderr, "\r  %-32s", status)
		}
	}

	if err := oll.Pull(ctx, c.Name, progress); err != nil {
		if !c.Quiet {
			_, _ = fmt.Fprintln(os.Stderr)
		}
		return fmt.Errorf("pull %s: %w", c.Name, err)
	}
	if !c.Quiet {
		// Newline after the carriage-return overwrites
		_, _ = fmt.Fprintln(os.Stderr)
		_, _ = fmt.Fprintf(os.Stderr, "pulled %s in %s\n", c.Name, time.Since(start).Truncate(100*time.Millisecond))
	} else {
		fmt.Printf("%s\n", strings.TrimSpace(c.Name))
	}
	return nil
}

// EmbedWarmCmd pre-populates the search_semantic embeddings cache for a
// directory tree. Walks every file with bbolt index attached, calls
// Ollama once per file to embed its body, stores the vector under
// (size, mtime) so subsequent semantic searches skip the I/O.
//
// Mirrors `mcp --warm-embeddings` exactly, just driven from a standalone
// CLI process — useful for pre-flight warming before opening Claude /
// Cursor (the MCP server will then open the warm bbolt file via the
// per-cwd default index path).
type EmbedWarmCmd struct {
	Dir       string        `arg:"" name:"dir" help:"Directory to warm."`
	Model     string        `name:"model" required:"" help:"Embedding model to use (e.g. nomic-embed-text). Required. Must be pulled — see 'file-search-on embed pull'."`
	Server    string        `name:"server" env:"OLLAMA_HOST" default:"http://localhost:11434" help:"Ollama base URL."`
	Workers   int           `name:"workers" help:"Worker count. Defaults to max(1, NumCPU/4) — a quarter of the cores so the rest of the box keeps its headroom while Ollama is being hammered."`
	Timeout   time.Duration `name:"timeout" default:"30m" help:"Hard deadline. Embeddings can be slow (~1s/file on Ollama localhost); raise for huge trees."`
	IndexPath string        `name:"index-path" help:"Persistent bbolt index file. Defaults to the per-cwd auto-derived path used by 'file-search-on mcp', so a warm here makes the MCP server faster when started from the same cwd."`
	NoIndex   bool          `name:"no-index" help:"Disable the on-disk index; use in-memory only. Useful when another instance holds the writer lock or for one-shot tests. Defeats the purpose of warming for inter-process sharing."`
}

func (c *EmbedWarmCmd) Run(ctx context.Context) error {
	idx, _, err := openIndex(c.IndexPath, c.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return fmt.Errorf("open index: %w", err)
	}
	defer func() { _ = idx.Close() }()

	embedder := ollamaembed.NewOllama(c.Server, c.Model)

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	if err := warmEmbeddings(ctx, idx, c.Dir, c.Workers, embedder, os.Stderr); err != nil {
		return fmt.Errorf("warm embeddings: %w", err)
	}
	return nil
}
