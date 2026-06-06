package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
	Threshold       *float64 `json:"threshold,omitempty" jsonschema:"Minimum cosine similarity (0..1) for a match. OMITTED = default 0.5. Pass 0 explicitly for NO floor (return every ranked result, e.g. when you'll page or rank yourself). Higher = stricter; 0.7 is 'definitely about this topic', 0.4-0.5 catches related-but-tangential. Implemented by AND-ing 'similarity >= <value>' onto the expr filter, so it combines with (does not replace) any similarity comparison in your expr."`
	Limit           int      `json:"limit,omitempty" jsonschema:"Cap on returned matches (top-K ranked by similarity desc). Default 50. When the ranked set is truncated by limit, the response carries an opaque next_cursor — pass it back as 'cursor' to fetch the next page."`
	Cursor          string   `json:"cursor,omitempty" jsonschema:"Opaque pagination token from a previous response's next_cursor. Resumes the similarity-ranked result set immediately after the last item of the prior page. Re-embeds the query and re-walks each page (file vectors are cached, so the re-walk is cheap); paging is stable under an unchanged tree. Use the SAME query/threshold for consistent paging."`
	Hybrid          bool     `json:"hybrid,omitempty" jsonschema:"Hybrid keyword+semantic ranking: fuse the BM25 keyword ranking with the embedding-similarity ranking via reciprocal-rank fusion (no manual weights). The keyword query defaults to 'query' unless keyword_query is set. Catches both exact-term hits and paraphrase. Issue #335."`
	KeywordQuery    string   `json:"keyword_query,omitempty" jsonschema:"Keyword query for the BM25 half of hybrid ranking. Defaults to 'query' when empty. Only used when hybrid=true; the tokenized terms are scored with Okapi BM25 (IDF over the candidate set) and surfaced as each match's bm25 field. Issue #335."`
	Model           string   `json:"model,omitempty" jsonschema:"Override the server's default embedding model for this call (e.g. nomic-embed-text, mxbai-embed-large). When the server has no default and this is empty, the call returns 'no embedding model configured'."`
	EmbeddingServer string   `json:"embedding_server,omitempty" jsonschema:"Override the server's default Ollama base URL for this call (e.g. http://gpu-box:11434). Defaults to the server-startup setting or http://localhost:11434."`
	Excludes        []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against the basename of each file/directory; matched directories are pruned. Same semantics as the search tool."`
	RespectGitignore bool    `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root and skip matching paths."`
	FollowSymlinks   bool    `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories."`
	IncludeBody      bool    `json:"include_body,omitempty" jsonschema:"When true, the full file body is exposed to the CEL pre-filter as the 'body' string variable. Most semantic-search workflows leave this off — the similarity score already captures conceptual match — but it's available for cases like 'similar AND contains a specific term'."`
	BodyMaxBytes     int      `json:"body_max_bytes,omitempty" jsonschema:"Cap on the body string read per file in bytes (default 1 MiB). Only relevant when include_body is set. Files larger than the cap are truncated; the prefix still participates in the CEL pre-filter."`
	EmbedMaxBytes    int      `json:"embed_max_bytes,omitempty" jsonschema:"Cap on the body text handed to the embedding model in bytes. 0 uses an 8 KiB default that fits common models' context windows. Embedding models truncate to their context anyway, and over-long input can be rejected by Ollama; raise only for large-context models (e.g. bge-m3)."`
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
	// NextCursor is an opaque token, present only when the ranked set was
	// truncated by Limit and more matches remain. Pass it back as the
	// 'cursor' input to fetch the next page. Empty means last page. #336.
	NextCursor         string         `json:"next_cursor,omitempty"`
	// AnnUsed reports which path served the query (issue #335 part 2):
	// true when answered from the warm in-memory vector index (no
	// filesystem walk, no re-embedding), false when the full walk ran
	// (cold/uncovered dir — which also warms the index for next time).
	AnnUsed            bool           `json:"ann_used"`
	// AnnStaleSkipped counts candidates the ANN path dropped because the
	// file changed / vanished since indexing (stale cached vector). Only
	// meaningful when AnnUsed is true.
	AnnStaleSkipped    int            `json:"ann_stale_skipped,omitempty"`
	ElapsedSeconds     float64        `json:"elapsed_seconds"`
	EmbeddingModel     string         `json:"embedding_model"`
	SimilarityThreshold float64       `json:"similarity_threshold"`
	// EmbedErrors counts files whose embedding failed during this call
	// (Ollama unreachable, model not pulled, input rejected, etc.).
	// Non-zero with Count==0 means "embedding failed", not "nothing
	// matched" — without it that failure is invisible (issue #305).
	EmbedErrors        uint64         `json:"embed_errors,omitempty"`
	Warning            string         `json:"warning,omitempty"`
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

	// Nil (omitted) → the 0.5 default. An explicit value — INCLUDING 0 —
	// is honoured, so callers can pass threshold:0 for "no floor"
	// (issue #349). Mirrors the CLI's --similarity-threshold, which
	// already honours 0, and the timeout_seconds nil-vs-0 distinction.
	threshold := 0.5
	if in.Threshold != nil {
		threshold = *in.Threshold
	}
	limit := in.Limit
	if limit == 0 {
		limit = 50
	}

	// Hybrid keyword+semantic ranking (issue #335). The BM25 keyword
	// query defaults to the semantic query unless overridden.
	keywordQuery := ""
	if in.Hybrid {
		keywordQuery = in.KeywordQuery
		if keywordQuery == "" {
			keywordQuery = in.Query
		}
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
	// Wrap the threshold literal in double(...) so a whole-number
	// threshold (0.0, 1.0) doesn't compile to an int literal and trip
	// cel-go's missing double>=int overload (issue #307).
	expr := search.SimilarityThresholdExpr(threshold)
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
		BodyMaxBytes:           in.BodyMaxBytes,
		Embedder:               embedder,
		SemanticQueryEmbedding: queryVec,
		EmbedInputMaxBytes:     in.EmbedMaxBytes,
		Excludes:               in.Excludes,
		RespectGitignore:       in.RespectGitignore,
		FollowSymlinks:         in.FollowSymlinks,
		KeywordQuery:           keywordQuery,
		Hybrid:                 in.Hybrid,
	}

	// Snapshot the embed-error counter so we can report how many files
	// failed to embed during THIS call — a non-zero count with no
	// matches means embedding failed, not that nothing matched (#305).
	embedErrsBefore := h.idx.Stats().EmbedErrors

	var results []search.Result
	var walkErr error
	cancelled := false
	annUsed := false
	annStale := 0

	// Warm ANN fast path (issue #335 part 2). Eligible only for a single
	// directory, non-hybrid (hybrid needs the walk's BM25 capture), and
	// without a body filter (the ANN path evaluates over cached attrs and
	// can't bind the `body` variable). When the (dir, model) pair is
	// covered by a prior full walk and the directory structure is
	// unchanged, answer from the in-memory vector index — no walk, no
	// re-embedding.
	absDir := dir
	if a, aerr := filepath.Abs(dir); aerr == nil {
		absDir = a
	}
	annEligible := len(dirs) == 0 && !in.Hybrid && !in.IncludeBody
	if annEligible && h.semIndex.Covered(absDir, model) {
		if r, stale, qerr := h.semIndex.Query(ctx, absDir, model, queryVec, expr, content.DefaultRegistry()); qerr == nil {
			results = r
			annStale = stale
			annUsed = true
		}
		// On a Query error, fall through to the full walk below.
	}

	if !annUsed {
		out := make(chan search.Result, 64)
		done := make(chan struct{})
		go func() {
			walkErr = search.WalkStream(ctx, walkOpts, content.DefaultRegistry(), out)
			close(done)
		}()

		token := req.Params.GetProgressToken()
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

		cancelled = errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded)
		if walkErr != nil && !cancelled {
			return nil, SearchSemanticOutput{}, fmt.Errorf("walk: %w", walkErr)
		}
		// Warm the in-memory vector index from the freshly-cached vectors
		// so subsequent queries against this (dir, model) take the fast
		// path. Only on a complete single-dir walk — a cancelled or
		// multi-root walk hasn't covered the tree. The walk embeds every
		// text file BEFORE the CEL filter, so coverage is query-agnostic.
		if !cancelled && len(dirs) == 0 {
			h.semIndex.Warm(absDir, model)
		}
	}
	elapsed := time.Since(start).Seconds()

	// Hybrid keyword+semantic ranking (issue #335). FinalizeBM25 scores
	// BM25 over the candidate set and writes the reciprocal-rank-fused
	// score to Result.Rank; we then rank by "rank" instead of raw
	// similarity. Pure semantic (no hybrid) keeps similarity ordering.
	sortField := "similarity"
	if in.Hybrid {
		if err := search.FinalizeBM25(results, walkOpts); err != nil {
			return nil, SearchSemanticOutput{}, fmt.Errorf("hybrid: %w", err)
		}
		sortField = "rank"
	}

	// Rank desc, resume past the cursor, cap to limit, and emit
	// next_cursor when more remain. PaginateResults sorts in a stable
	// total order (sort field desc, path asc tiebreak) so paging is
	// consistent across calls. Files with zero similarity (binary content
	// that skipped embedding) group at the end; the threshold filter
	// above already excludes them in normal use.
	page, nextCursor, perr := search.PaginateResults(results, sortField, "desc", in.Cursor, limit)
	if perr != nil {
		return nil, SearchSemanticOutput{}, fmt.Errorf("cursor: %w", perr)
	}
	results = page

	matches := make([]search.Match, len(results))
	for i, r := range results {
		matches[i] = search.MatchFrom(r)
	}

	output := SearchSemanticOutput{
		Matches:             matches,
		Count:               len(matches),
		NextCursor:          nextCursor,
		AnnUsed:             annUsed,
		AnnStaleSkipped:     annStale,
		ElapsedSeconds:      elapsed,
		EmbeddingModel:      model,
		SimilarityThreshold: threshold,
	}
	if embedErrs := h.idx.Stats().EmbedErrors - embedErrsBefore; embedErrs > 0 {
		output.EmbedErrors = embedErrs
		output.Warning = fmt.Sprintf("%d file(s) failed to embed (model %q at %s) — results may be incomplete; check that Ollama is running, the model is pulled, and consider lowering embed_max_bytes", embedErrs, model, server)
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
