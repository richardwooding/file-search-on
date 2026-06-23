package main

import (
	"context"
	"fmt"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/playground"
	"github.com/richardwooding/file-search-on/internal/search"
	"github.com/richardwooding/ollamaembed"
)

// PlaygroundCmd launches the interactive CEL-filtering TUI: type a CEL
// expression up top and watch a one-shot snapshot of the directory's files
// filter live as you type, over the same attribute vocabulary search uses.
// On exit it prints the final expression to stdout so a query built here is
// directly reusable with `search`.
//
// Passing --embedding-model switches on semantic mode: a second input appears
// for a natural-language query (embedded via Ollama and ranked by cosine
// similarity), and the CEL box filters that ranked snapshot live
// ("is_source && similarity > 0.6"). On exit it prints a reproducible
// `file-search-on search --semantic-query …` command.
type PlaygroundCmd struct {
	Expr             string   `arg:"" optional:"" help:"Initial CEL expression to pre-fill the input with (optional). e.g. 'is_source && max_complexity > 15'."`
	Dir              []string `short:"d" default:"." help:"Directory to search in. Repeatable — pass -d ./docs -d ./posts to snapshot multiple roots."`
	Exclude          []string `name:"exclude" help:"Glob pattern matched against the basename of each file/directory; matches are skipped. Repeatable."`
	RespectGitignore bool     `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	PruneArtefacts   bool     `name:"prune-build-artefacts" help:"Union canonical build-artefact basenames (vendor / node_modules / target / …) into --exclude before snapshotting."`
	Workers          int      `short:"w" default:"0" help:"Number of parallel workers for the initial snapshot. 0 uses NumCPU."`
	Limit            int      `name:"limit" default:"5000" help:"Cap on the number of files snapshotted for in-memory filtering. Keeps eval instant on large trees; surfaced as 'first N shown' when hit."`
	Body             bool     `name:"body" help:"Make file bodies available to the CEL expression as the 'body' string variable so body.contains(...) works. Expensive: reads every candidate file's body during the snapshot."`
	BodyMaxBytes     int      `name:"body-max-bytes" default:"0" help:"Cap on the body string read per file in bytes. 0 uses the 1 MiB default."`

	IndexPath string `name:"index-path" help:"Persistent attribute index file (bbolt). Overrides the default per-cwd index. Caches file embeddings across semantic re-queries so changing the natural-language query only re-embeds the query, not every file."`
	NoIndex   bool   `name:"no-index" help:"Disable the on-disk index entirely; use only the in-memory cache for the process lifetime."`

	// Semantic mode (issue: semantic search TUI). Passing --embedding-model
	// turns on the natural-language query box; mirrors the `search` flags.
	SemanticQuery       string  `name:"semantic-query" help:"Initial natural-language query to pre-fill the semantic box (only used when --embedding-model is set)."`
	EmbeddingModel      string  `name:"embedding-model" help:"Ollama embedding model name (e.g. all-minilm). Setting this enables semantic mode: a natural-language query box ranks files by cosine 'similarity' and the CEL box filters live."`
	EmbeddingServer     string  `name:"embedding-server" env:"OLLAMA_HOST" default:"http://localhost:11434" help:"Ollama base URL for embedding requests. Resolution order: --embedding-server flag > $OLLAMA_HOST env var > http://localhost:11434."`
	SimilarityThreshold float64 `name:"similarity-threshold" default:"0.5" help:"Default similarity floor carried into the reproducible 'search' command printed on exit. Filter interactively in the CEL box with 'similarity > X'."`
	EmbedMaxBytes       int     `name:"embed-max-bytes" default:"0" help:"Cap on the body text handed to the embedding model (bytes). 0 uses an 8 KiB default that fits common models' context windows."`
}

func (p *PlaygroundCmd) Run(ctx context.Context) error {
	// On-disk index is on by default (per-cwd file); caches embeddings across
	// semantic re-queries. Opt out with --no-index.
	idx, _, err := openIndex(p.IndexPath, p.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	opts := search.Options{
		Roots:               p.Dir,
		Excludes:            p.Exclude,
		RespectGitignore:    p.RespectGitignore,
		PruneBuildArtefacts: p.PruneArtefacts,
		Workers:             p.Workers,
		IncludeBody:         p.Body,
		BodyMaxBytes:        p.BodyMaxBytes,
		Index:               idx,
	}

	ro := playground.RunOptions{
		Opts:     opts,
		Registry: content.DefaultRegistry(),
		Initial:  p.Expr,
		Limit:    p.Limit,
	}

	// Semantic mode is opt-in via --embedding-model. The embedder is
	// lazy-connect; no HTTP call happens until the TUI submits a query.
	if p.EmbeddingModel != "" {
		ro.Embedder = ollamaembed.NewOllama(p.EmbeddingServer, p.EmbeddingModel)
		ro.EmbeddingModel = p.EmbeddingModel
		ro.EmbeddingServer = p.EmbeddingServer
		ro.SimilarityThreshold = p.SimilarityThreshold
		ro.EmbedMaxBytes = p.EmbedMaxBytes
		ro.SemanticQuery = p.SemanticQuery
	}

	final, err := playground.Run(ctx, ro)
	if err != nil {
		return err
	}
	// Print the final expression / reproducible command so a query authored in
	// the TUI is reusable.
	if final != "" {
		fmt.Println(final)
	}
	return nil
}
