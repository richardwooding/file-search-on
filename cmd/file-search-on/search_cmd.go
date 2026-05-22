package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"text/template"
	"time"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

type SearchCmd struct {
	Expr             string        `arg:"" help:"CEL expression to match files (e.g. 'is_json && size > 1024')." optional:""`
	Dir              []string      `short:"d" help:"Directory to search in. Repeatable — pass -d ./docs -d ./posts to walk multiple roots in one call. Each root's .gitignore is honoured independently when --respect-gitignore is set." default:"."`
	Workers          int           `short:"w" help:"Number of parallel workers." default:"0"`
	List             bool          `short:"l" help:"List supported attributes and content types."`
	MaxLineBytes     int           `short:"L" name:"max-line-bytes" help:"Per-line scanner cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default." default:"0"`
	Output           string        `short:"o" name:"output" enum:"bare,default,verbose,json" default:"default" help:"Output format: bare | default | verbose | json."`
	Format           string        `name:"format" help:"Custom Go text/template applied per match (e.g. '{{.Path}}\\t{{.Title}}'). When set, takes precedence over -o."`
	Unsorted         bool          `name:"unsorted" help:"Stream matches in walk order instead of buffering+sorting. Default and verbose modes still emit the count footer; bare/json/template are streamed and unsorted regardless. Ignored when --sort or --limit is set (those force buffered mode)."`
	IndexPath        string        `name:"index-path" help:"Persistent attribute index file (bbolt). When set, unchanged files (matched by absolute path + size + mtime) skip the per-file content-type parse, making repeat searches dramatically faster. The file is created on first use; delete it to force a full re-extraction."`
	Timeout          time.Duration `name:"timeout" help:"Maximum walk duration (Go duration string: 30s, 2m, 500ms). Default unset = no timeout. On expiry, results collected so far are still printed and the process exits 124. Ctrl-C exits 130 with whatever was collected."`
	Sort             string        `name:"sort" help:"Sort matches by attribute. Recognised keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. Files missing the attribute group at the end. Forces buffered mode."`
	Order            string        `name:"order" help:"Sort direction: 'asc' (default for --sort) or 'desc'. When --rank is set the default flips to 'desc' (higher score first). Ignored without --sort or --rank."`
	Rank             string        `name:"rank" help:"CEL expression returning double / int / bool — evaluated per file (after the filter) as a custom sort key. Higher values rank first. Composes with --semantic-query (similarity is a CEL variable). When set, overrides --sort and defaults --order to desc. Example: --rank 'similarity * 0.7 + (mod_time > timestamp(\"2025-01-01T00:00:00Z\") ? 0.3 : 0.0)'. Forces buffered mode."`
	Limit            int           `name:"limit" default:"0" help:"Cap the result set at N matches. With --sort, returns the top-N (after sorting). Without --sort, returns the first N in walk order. 0 = unlimited."`
	Snippet          bool          `name:"snippet" help:"Read a snippet of each match's body (first N lines, see --snippet-lines) and include it in verbose/json/template output. Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave the snippet empty."`
	SnippetLines     int           `name:"snippet-lines" default:"10" help:"How many lines of body content to include per match when --snippet is set."`
	Body             bool          `name:"body" help:"Make file body available to the CEL expression as the 'body' string variable. Pair with CEL's built-in string methods to filter on content: --body 'is_markdown && body.contains(\"transformer\")', or for regex: --body 'is_source && body.matches(\"(?i)\\\\bTODO\\\\b\")'. Only text-based content types populate; the body is capped at --body-max-bytes (default 1 MiB). Expensive: reads every candidate file's body, not just headers."`
	BodyMaxBytes     int           `name:"body-max-bytes" default:"0" help:"Cap on the body string read per file in bytes. 0 uses the 1 MiB default. Files larger than the cap are silently truncated; the prefix still participates in the CEL filter."`
	BodyCacheMaxBytes int          `name:"body-cache-max-bytes" default:"268435456" help:"Total size cap (bytes) for the body cache inside the bbolt index file. Default 256 MiB. FIFO eviction by access time once exceeded. Only relevant when --body and --index-path are both set."`
	NoBodyCache      bool          `name:"no-body-cache" help:"Disable the body cache entirely. PutBody is a no-op and LookupBody always misses. Use when caching adds no value (one-shot search of a tree that won't be queried again) or when storage is at a premium."`
	WithHashes       bool          `name:"with-hashes" help:"Compute MD5, SHA1, and SHA256 of every matched file in a single io.MultiWriter pass and expose them as md5 / sha1 / sha256 CEL variables (and on the JSON/template output). Hashes cache in the index alongside (size, mtime), so subsequent runs are free on unchanged files. Off by default — hashing every file reads multi-GB videos / archives in full. Opt-in for forensic / NSRL / VirusTotal / threat-intel-feed workflows."`
	CheckDisguised   bool          `name:"check-disguised" help:"Run both the name-based and magic-byte detection passes on every matched file, populating magic_content_type / extension_content_type / is_disguised CEL variables. is_disguised fires when the bytes disagree with the extension — classic 'this .txt is actually a PE binary' indicator. One extra 512-byte file read per match (cached in the index)."`
	HashAllowlist    string        `name:"hash-allowlist" help:"Path to a hash allowlist (newline-separated md5/sha1/sha256 hex, mixed algorithms auto-detected by length, # comments allowed) OR a pre-built bbolt hashset file (via 'hash-set build'). When set, populates the is_known_good CEL predicate by looking up each matched file's hashes. Forces --with-hashes on. NSRL / corp allowlist / threat-intel allowlist interop."`
	HashDenylist     string        `name:"hash-denylist" help:"Path to a hash denylist (same format as --hash-allowlist). Populates is_known_bad. Threat-intel-feed / IOC-list interop."`
	SemanticQuery    string        `name:"semantic-query" help:"Natural-language query to embed and rank every matched file against. Populates the 'similarity' CEL variable (cosine in 0..1) so filters like 'is_pdf && similarity > 0.7' fire. Requires --embedding-model + a running Ollama instance (--embedding-server defaults to http://localhost:11434). Auto-sorts results by similarity desc and applies --similarity-threshold."`
	SimilarityThreshold float64    `name:"similarity-threshold" default:"0.5" help:"Minimum similarity score (0..1) for a file to be returned when --semantic-query is set. Higher = stricter."`
	EmbeddingServer  string        `name:"embedding-server" default:"http://localhost:11434" help:"Ollama base URL for embedding requests."`
	EmbeddingModel   string        `name:"embedding-model" help:"Ollama embedding model name (e.g. nomic-embed-text, mxbai-embed-large). No default — user picks per-tree based on the model they've pulled. Required when --semantic-query is set."`
	Exclude          []string      `name:"exclude" help:"Glob pattern matched against the basename of each file/directory; matches are skipped (directories are pruned). Repeatable: --exclude node_modules --exclude '*.bak'."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at the walk root (if present) and skip matching paths. Nested .gitignore files in subdirectories are NOT honoured in this version."`
	FollowSymlinks   bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories during the walk. Off by default — symlinks-to-dirs surface as leaf entries with is_symlink=true. The is_symlink / target_path / is_broken_symlink CEL attributes are populated regardless of this flag. No loop detection."`
	ResolveProjects  bool          `name:"resolve-projects" help:"Populate the 'project_types' (list<string>) and 'project_type' (string) CEL variables for each match by resolving the file's containing project root (go.mod, package.json, Cargo.toml, …). Enables filters like 'is_source && project_type == \"go\"'. Adds one ReadDir per unique directory walked (cached) — opt-in to avoid the cost when not needed."`
	PruneArtefacts   bool          `name:"prune-build-artefacts" help:"Pre-walk the tree to find project roots and union their canonical build-artefact basenames (vendor for Go, node_modules for Node, target for Rust, __pycache__/.venv for Python, bin/obj for .NET, .terraform for Terraform, …) into --exclude. Saves the boilerplate exclude list when searching monorepos or ~/Code. Opt-in: pre-walk costs I/O proportional to tree size."`
}

func (s *SearchCmd) Run(ctx context.Context) error {
	if s.List {
		printHelp()
		return nil
	}

	if s.Expr == "" {
		s.Expr = "true"
	}

	// --format implies attribute access; same for verbose/json
	// presets. --sort also needs Attrs (per-family sort keys live in
	// FileAttributes.Extra), as does --snippet (rendered through the
	// Record path which already requires attrs). --body doesn't
	// need Attrs surfaced on Result (the body lives in Extra only
	// for CEL evaluation), but it's harmless to keep them.
	includeAttrs := s.Format != "" || s.Output == "verbose" || s.Output == "json" || s.Sort != "" || s.Snippet || s.WithHashes || s.CheckDisguised || s.HashAllowlist != "" || s.HashDenylist != "" || s.SemanticQuery != ""

	// Parse the template up front so a bad template fails before we walk.
	var tmpl *template.Template
	if s.Format != "" {
		var err error
		tmpl, err = parseFormatTemplate(s.Format)
		if err != nil {
			return fmt.Errorf("parse --format template: %w", err)
		}
	}

	// CLI is opt-in: nothing is created unless --index-path is set.
	// One-shot CLI runs without an explicit path don't benefit from a
	// process-local cache, so we skip the allocation entirely.
	var idx index.Index
	if s.IndexPath != "" {
		var err error
		idx, err = openIndex(s.IndexPath, index.BodyCacheCap{
			MaxBytes: int64(s.BodyCacheMaxBytes),
			Disable:  s.NoBodyCache,
		})
		if err != nil {
			return err
		}
	}

	// Layer a timeout on top of the signal-bound parent ctx so we can
	// distinguish "user pressed Ctrl-C" (parent ctx canceled) from
	// "--timeout fired" (effective ctx deadline-exceeded but parent
	// ctx still healthy). The walker, MCP-search, and index code all
	// honour ctx; partial results land in the slice/channel before
	// the helpers return.
	parentCtx := ctx
	effectiveCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	// Load hash allowlist / denylist files when set. Auto-detects
	// bbolt-format vs newline-separated text; an unreadable file
	// fails fast before the walk starts. Membership checking
	// requires --with-hashes so the per-file hash trio is computed;
	// we force it on transparently rather than erroring.
	var allowlist, denylist hashset.Set
	if s.HashAllowlist != "" {
		al, err := hashset.Open(s.HashAllowlist)
		if err != nil {
			return fmt.Errorf("load --hash-allowlist: %w", err)
		}
		allowlist = al
		defer func() { _ = al.Close() }()
		s.WithHashes = true
	}
	if s.HashDenylist != "" {
		dl, err := hashset.Open(s.HashDenylist)
		if err != nil {
			return fmt.Errorf("load --hash-denylist: %w", err)
		}
		denylist = dl
		defer func() { _ = dl.Close() }()
		s.WithHashes = true
	}

	// Semantic search setup (issue #151). Pre-embed the query once
	// so workers do one dot-product per file rather than re-embedding
	// the query for every file. The Embedder is lazy-connect — no
	// HTTP call happens until the first per-file embedding.
	var embedder embed.Embedder
	var queryVector []float32
	if s.SemanticQuery != "" {
		if s.EmbeddingModel == "" {
			return fmt.Errorf("--semantic-query requires --embedding-model (pass an Ollama model name, e.g. nomic-embed-text)")
		}
		embedder = embed.NewOllama(s.EmbeddingServer, s.EmbeddingModel)
		v, err := embedder.Embed(effectiveCtx, s.SemanticQuery)
		if err != nil {
			return fmt.Errorf("embed query: %w", err)
		}
		embed.Normalize(v)
		queryVector = v
		// Force sort by similarity desc unless the user already
		// specified one — semantic search without ranking is useless.
		if s.Sort == "" {
			s.Sort = "similarity"
			s.Order = "desc"
		}
		// Fold the similarity threshold into the CEL filter. Use
		// CEL's fmt.Sprintf-friendly literal for the float so locale
		// settings can't accidentally produce a comma. CEL parses
		// scientific notation, so even very small thresholds work.
		threshold := fmt.Sprintf("similarity >= %g", s.SimilarityThreshold)
		if s.Expr == "" {
			s.Expr = threshold
		} else {
			s.Expr = "(" + s.Expr + ") && " + threshold
		}
	}

	opts := search.Options{
		Roots:             s.Dir,
		Expr:              s.Expr,
		Workers:           s.Workers,
		MaxLineBytes:      s.MaxLineBytes,
		IncludeAttributes: includeAttrs,
		Index:             idx,
		Sort:              s.Sort,
		Order:             s.Order,
		RankExpr:          s.Rank,
		Limit:             s.Limit,
		IncludeSnippet:    s.Snippet,
		SnippetLines:      s.SnippetLines,
		IncludeBody:       s.Body,
		BodyMaxBytes:      s.BodyMaxBytes,
		ComputeHashes:     s.WithHashes,
		CheckDisguised:    s.CheckDisguised,
		Allowlist:         allowlist,
		Denylist:          denylist,
		Embedder:               embedder,
		SemanticQueryEmbedding: queryVector,
		Excludes:            s.Exclude,
		RespectGitignore:    s.RespectGitignore,
		FollowSymlinks:      s.FollowSymlinks,
		ResolveProjects:     s.ResolveProjects,
		PruneBuildArtefacts: s.PruneArtefacts,
	}

	// --sort, --rank, and --limit all need the full result set in
	// memory (sort, then truncate), so they force buffered mode
	// regardless of --unsorted / -o bare / json / --format.
	// Streaming + top-K is incoherent; bail to buffered.
	forceBuffered := s.Sort != "" || s.Rank != "" || s.Limit > 0
	// Streaming-friendly modes (bare / json / template) always stream —
	// first result lands on stdout immediately rather than waiting for
	// the full walk. Default and verbose stream too when --unsorted is
	// set; otherwise they buffer for sort+footer (the historical UX).
	streaming := !forceBuffered && (tmpl != nil || s.Output == "bare" || s.Output == "json" || s.Unsorted)
	var runErr error
	if streaming {
		runErr = streamSearch(effectiveCtx, opts, tmpl, s.Output)
	} else {
		runErr = bufferedSearch(effectiveCtx, opts, tmpl, s.Output)
	}
	// Close the index BEFORE reading Stats so the bbolt writer goroutine
	// has flushed pending puts; otherwise the footer can show "0 stored"
	// even though the writes are queued and will land on disk before the
	// process exits.
	if idx != nil {
		_ = idx.Close()
		st := idx.Stats()
		fmt.Fprintf(os.Stderr, "index: %d hits, %d misses, %d stored, %d stale, %d errors\n",
			st.Hits, st.Misses, st.Puts, st.Stales, st.Errors)
		// Print body-cache line only when body caching actually fired
		// — keeps the footer clean for callers that don't use --body.
		if st.BodyHits+st.BodyMisses+st.BodyPuts+st.BodyStales+st.BodyEvictions+st.BodyOversize+st.BodyErrors > 0 {
			fmt.Fprintf(os.Stderr, "body cache: %d hits, %d misses, %d stored, %d stale, %d evicted, %d oversized, %d errors\n",
				st.BodyHits, st.BodyMisses, st.BodyPuts, st.BodyStales, st.BodyEvictions, st.BodyOversize, st.BodyErrors)
		}
	}

	// Distinguish timeout, Ctrl-C, and real errors. The stream/buffered
	// helpers print whatever they collected before returning the ctx
	// error, so stdout already reflects the partial set.
	if isCancellation(runErr) {
		// matches are already flushed to stdout via stream/buffered
		// helpers; we don't carry them at this scope, so the
		// hot-directory heuristic is skipped for the CLI. The other
		// four suggestions (timeout-bump, body warning, missing
		// prunes, lax filter) still fire on opts alone.
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "search interrupted; results above may be incomplete")
			printSuggestions(os.Stderr, search.SuggestionsForSearch(opts, nil, 0, "client_cancel"))
			return &exitCodeError{code: 130, msg: "interrupted"}
		case s.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "search timed out after %s; results above may be incomplete\n", s.Timeout)
			printSuggestions(os.Stderr, search.SuggestionsForSearch(opts, nil, s.Timeout.Seconds(), "timeout"))
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return runErr
}

// streamSearch drives WalkStream and prints each match as it arrives.
// For default and verbose modes, counts records as they flow through
// and emits the "N file(s) found" footer to stderr after the stream
// closes — preserves the count UX even in streaming mode.
func streamSearch(ctx context.Context, opts search.Options, tmpl *template.Template, mode string) error {
	out := make(chan search.Result, 64)
	var walkErr error
	done := make(chan struct{})
	go func() {
		walkErr = search.WalkStream(ctx, opts, contentpkg.DefaultRegistry(), out)
		close(done)
	}()

	var printErr error
	var count int64
	switch {
	case tmpl != nil:
		printErr = printTemplateStream(os.Stdout, out, tmpl)
	case mode == "json":
		printErr = printJSONStream(os.Stdout, out)
	case mode == "bare":
		printBareStream(os.Stdout, out)
	case mode == "verbose":
		count = printVerboseStream(os.Stdout, out)
	default: // "default"
		count = printDefaultStream(os.Stdout, out)
	}
	<-done

	if mode == "default" || mode == "verbose" {
		fmt.Fprintf(os.Stderr, "\n%d file(s) found\n", count)
	}
	if walkErr != nil {
		// Cancellation gets returned as-is so the parent can surface
		// the right exit code + diagnostic; partial results have
		// already been printed by the per-mode streamer.
		if isCancellation(walkErr) {
			return walkErr
		}
		return fmt.Errorf("search failed: %w", walkErr)
	}
	return printErr
}

// bufferedSearch keeps the historical Walk + sort + print + footer
// flow. Used by default/verbose modes (which always emit a
// "N file(s) found" footer requiring the full result set) and for
// bare/json/template modes when --sort or --limit force buffered
// mode — top-K is incoherent with streaming, so we collect.
//
// search.Walk applies Options.Sort / Options.Order / Options.Limit
// itself; we re-sort by path here only when no explicit Sort is set,
// to preserve the long-standing "path-sorted by default" CLI UX.
//
// On context cancellation, search.Walk still returns the partial set
// in the results slice; we sort+print it before bubbling the
// cancellation up so the user sees what was collected.
func bufferedSearch(ctx context.Context, opts search.Options, tmpl *template.Template, mode string) error {
	results, err := search.Walk(ctx, opts, contentpkg.DefaultRegistry())

	// Path-sort default only when the user didn't ask for a specific
	// ordering. With --sort OR --rank set, results are already in the
	// order Walk produced; re-sorting would defeat the flag. (Walk
	// itself transforms RankExpr into Sort="rank" internally; we
	// check RankExpr here because that transformation runs on Walk's
	// own copy of opts and isn't visible to this caller.)
	if opts.Sort == "" && opts.RankExpr == "" {
		sort.Slice(results, func(i, j int) bool {
			return results[i].Path < results[j].Path
		})
	}

	var printErr error
	switch {
	case tmpl != nil:
		printErr = printTemplate(os.Stdout, results, tmpl)
	case mode == "json":
		printErr = printJSON(os.Stdout, results)
	case mode == "bare":
		printBare(os.Stdout, results)
	case mode == "verbose":
		printVerbose(os.Stdout, results)
	default: // "" or "default"
		printDefault(os.Stdout, results)
	}
	// Footer on stderr — preserve historical UX for default/verbose,
	// and surface a count for the json/template/bare modes too when
	// the user asked for buffering (sort/limit) since the buffered
	// path is no longer the "silent" choice.
	if mode == "default" || mode == "verbose" || opts.Sort != "" || opts.Limit > 0 {
		fmt.Fprintf(os.Stderr, "\n%d file(s) found\n", len(results))
	}

	if err != nil {
		if isCancellation(err) {
			return err
		}
		return fmt.Errorf("search failed: %w", err)
	}
	return printErr
}

func printHelp() {
	schema := celexpr.Schema()

	fmt.Println("Supported CEL attributes:")
	printAttrs(schema.Common, 12, 9)
	fmt.Println()
	fmt.Println("Type-specific attributes:")
	printAttrs(schema.TypeSpecific, 18, 11)
	fmt.Println()
	fmt.Println("Markdown front-matter attributes (YAML ---, TOML +++, JSON {}):")
	printAttrs(schema.Frontmatter, 18, 11)
	fmt.Println()
	fmt.Println("Built-in functions:")
	printFuncs(schema.Functions)
	fmt.Println()
	fmt.Println("Registered content types:")
	for _, ct := range contentpkg.DefaultRegistry().Types() {
		fmt.Printf("  %-20s %v\n", ct.Name(), ct.Extensions())
	}
}

func printAttrs(attrs []celexpr.AttributeDoc, nameWidth, typeWidth int) {
	for _, a := range attrs {
		typeField := "(" + a.Type + ")"
		fmt.Printf("  %-*s %-*s - %s\n", nameWidth, a.Name, typeWidth, typeField, a.Description)
	}
}

func printFuncs(funcs []celexpr.FunctionDoc) {
	for _, f := range funcs {
		fmt.Printf("  %s\n      %s\n", f.Signature, f.Description)
		if f.Example != "" {
			fmt.Printf("      e.g. %s\n", f.Example)
		}
	}
}
