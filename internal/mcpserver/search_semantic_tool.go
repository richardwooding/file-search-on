package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/search"
)

// SearchSemanticInput is the JSON-schema input for the `search_semantic`
// tool. Distinct from SearchInput because the discovery shape differs:
// `query` is required, results rank by similarity desc rather than path,
// and the embedding-model overrides surface as first-class inputs.
type SearchSemanticInput struct {
	Query           string   `json:"query" jsonschema:"Natural-language query to embed via Ollama and rank every matched file against. Returns conceptually-related files even when the exact words don't appear in the body. Required."`
	Dir             string   `json:"dir,omitempty" jsonschema:"Directory to search in. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs            []string `json:"dirs,omitempty" jsonschema:"Multiple directories to search in one call. When non-empty, takes precedence over 'dir'."`
	Expr            string   `json:"expr,omitempty" jsonschema:"Optional CEL pre-filter using the regular search vocabulary (is_pdf, is_office, etc.). When set, the threshold is AND-combined: only files matching the CEL filter AND with similarity >= threshold are returned."`
	Threshold       float64  `json:"threshold,omitempty" jsonschema:"Minimum cosine similarity (0..1) for a match. Default 0.5. Higher = stricter. 0.7 is a useful tightness for 'definitely about this topic'; 0.4-0.5 catches related-but-tangential content."`
	Limit           int      `json:"limit,omitempty" jsonschema:"Cap on returned matches (top-K ranked by similarity desc). Default 50."`
	Model           string   `json:"model,omitempty" jsonschema:"Override the server's default embedding model for this call (e.g. nomic-embed-text, mxbai-embed-large). When the server has no default and this is empty, the call returns 'no embedding model configured'."`
	EmbeddingServer string   `json:"embedding_server,omitempty" jsonschema:"Override the server's default Ollama base URL for this call (e.g. http://gpu-box:11434). Defaults to the server-startup setting or http://localhost:11434."`
	Excludes        []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against the basename of each file/directory; matched directories are pruned. Same semantics as the search tool."`
	RespectGitignore bool    `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root and skip matching paths."`
	FollowSymlinks   bool    `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories."`
	IncludeBody      bool    `json:"include_body,omitempty" jsonschema:"When true, the full file body is exposed to the CEL pre-filter as the 'body' string variable. Most semantic-search workflows leave this off — the similarity score already captures conceptual match — but it's available for cases like 'similar AND contains a specific term'."`
	TimeoutSeconds  *float64 `json:"timeout_seconds,omitempty" jsonschema:"Per-call timeout in seconds. 0 disables; nil falls through to the server default. On timeout the partial ranked set is returned with cancelled=true; not an error."`
	Workers         int      `json:"workers,omitempty" jsonschema:"Parallel walker workers. 0 = runtime.NumCPU()."`
}

// SearchSemanticOutput mirrors SearchOutput but the Matches field is
// implicitly ranked by similarity desc. similarity_threshold echoes
// what the call actually filtered with, embedding_model echoes which
// model was used — both surface so agents can audit what they got.
type SearchSemanticOutput struct {
	CommonOutput
	Matches            []search.Match `json:"matches"`
	Count              int            `json:"count"`
	ElapsedSeconds     float64        `json:"elapsed_seconds"`
	EmbeddingModel     string         `json:"embedding_model"`
	SimilarityThreshold float64       `json:"similarity_threshold"`
	Cancelled          bool           `json:"cancelled,omitempty"`
	CancellationReason string         `json:"cancellation_reason,omitempty"`
}

func (h *handlers) searchSemanticHandler(ctx context.Context, req *mcp.CallToolRequest, in SearchSemanticInput) (*mcp.CallToolResult, SearchSemanticOutput, error) {
	if in.Query == "" {
		return nil, SearchSemanticOutput{}, errors.New("query is required")
	}

	// Resolve model + server: per-call inputs override server-startup
	// defaults. Empty model is a fatal error here — embedding lookup
	// can't function without one. Empty server falls through to
	// localhost:11434 (the OllamaEmbedder default).
	model := in.Model
	if model == "" {
		model = h.defaultEmbeddingModel
	}
	if model == "" {
		return nil, SearchSemanticOutput{}, errors.New("no embedding model configured (pass --embedding-model at MCP startup or 'model' on the call)")
	}
	server := in.EmbeddingServer
	if server == "" {
		server = h.defaultEmbeddingServer
	}

	threshold := in.Threshold
	if threshold == 0 {
		threshold = 0.5
	}
	limit := in.Limit
	if limit == 0 {
		limit = 50
	}

	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, SearchSemanticOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, SearchSemanticOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" {
		dir = "."
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, SearchSemanticOutput{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, SearchSemanticOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, SearchSemanticOutput{}, err
	}

	parentCtx := ctx
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()

	// Build + embed the query. This is the first HTTP call to Ollama
	// — failures here are the natural "is Ollama running?" / "is the
	// model pulled?" checkpoint. Surface them clearly.
	embedder := embed.NewOllama(server, model)
	queryVec, err := embedder.Embed(ctx, in.Query)
	if err != nil {
		return nil, SearchSemanticOutput{}, fmt.Errorf("embed query (model %q at %s): %w", model, server, err)
	}
	embed.Normalize(queryVec)

	// Fold the threshold into the CEL filter. Pre-prune via in.Expr
	// when set; otherwise just the threshold gate.
	expr := fmt.Sprintf("similarity >= %g", threshold)
	if in.Expr != "" {
		expr = "(" + in.Expr + ") && " + expr
	}

	walkOpts := search.Options{
		Root:                   dir,
		Roots:                  dirs,
		Expr:                   expr,
		Workers:                in.Workers,
		IncludeAttributes:      true,
		Index:                  h.idx,
		IncludeBody:            in.IncludeBody,
		Embedder:               embedder,
		SemanticQueryEmbedding: queryVec,
		Excludes:               in.Excludes,
		RespectGitignore:       in.RespectGitignore,
		FollowSymlinks:         in.FollowSymlinks,
	}

	out := make(chan search.Result, 64)
	var walkErr error
	done := make(chan struct{})
	go func() {
		walkErr = search.WalkStream(ctx, walkOpts, content.DefaultRegistry(), out)
		close(done)
	}()

	token := req.Params.GetProgressToken()
	var results []search.Result
	processed := int64(0)
	for r := range out {
		results = append(results, r)
		processed++
		if token != nil && processed%progressNotifyStride == 0 {
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: token,
				Progress:      float64(processed),
				Message:       fmt.Sprintf("%d matches so far", processed),
			})
		}
	}
	<-done
	elapsed := time.Since(start).Seconds()

	cancelled := errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded)
	if walkErr != nil && !cancelled {
		return nil, SearchSemanticOutput{}, fmt.Errorf("walk: %w", walkErr)
	}

	// Sort by similarity desc + apply limit. SortAndLimit handles the
	// case where results have a mix of zero similarities (binary content
	// that skipped the embedding) by grouping them at the end — the
	// threshold filter above already excluded them in normal use, so
	// the bottom of the list is sparse only on edge cases.
	results = search.SortAndLimit(results, search.Options{
		Sort:  "similarity",
		Order: "desc",
		Limit: limit,
	})

	matches := make([]search.Match, len(results))
	for i, r := range results {
		matches[i] = search.MatchFrom(r)
	}
	// Re-sort by similarity to defend against any future shuffling
	// inside SortAndLimit (defensive — Sort=="similarity" already does
	// the right thing today).
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Similarity > matches[j].Similarity })

	output := SearchSemanticOutput{
		Matches:             matches,
		Count:               len(matches),
		ElapsedSeconds:      elapsed,
		EmbeddingModel:      model,
		SimilarityThreshold: threshold,
	}
	if cancelled {
		output.Cancelled = true
		if errors.Is(walkErr, context.DeadlineExceeded) && parentCtx.Err() == nil {
			output.CancellationReason = "timeout"
		} else {
			output.CancellationReason = "client_cancel"
		}
	}
	output.ServerVersion = h.version
	return nil, output, nil
}
