package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/search"
)

// SearchInput is the JSON-schema input for the `search` tool.
type SearchInput struct {
	Expr             string   `json:"expr,omitempty" jsonschema:"CEL expression matched against file attributes. Boolean type predicates: is_markdown, is_pdf, is_html, is_xml, is_json, is_yaml, is_toml, is_csv, is_text, is_image, is_audio, is_video, is_office, is_epub, is_archive, is_binary, is_email, is_source, is_notebook. Exact-name types (per-type + family predicate both fire): is_dockerfile/is_build, is_makefile/is_build, is_justfile/is_build, is_rakefile/is_build, is_license/is_repo_meta, is_changelog/is_repo_meta, is_contributing/is_repo_meta, is_codeowners/is_repo_meta, is_gitignore/is_ignore, is_dockerignore/is_ignore, is_gomod/is_manifest (parses module + go_version), is_node_manifest/is_manifest, is_cargo_manifest/is_manifest, is_pipfile/is_manifest, is_python_reqs/is_manifest, is_gemfile/is_manifest, is_procfile/is_platform, is_vagrantfile/is_platform. Common attributes: size (int, bytes), name/path/dir/ext (string), word_count/line_count/page_count (int), title/author/language (string). Examples: 'is_markdown && word_count > 500'; 'is_pdf && page_count > 10'; 'is_image && iso > 1600'; 'is_audio && sample_rate >= 96000'; 'is_video && duration > 1800'; 'is_source && language == \"go\" && loc > 100'; 'size > 1000000 && !is_binary'. Empty means match all. Call list_attributes for the full schema."`
	Dir              string   `json:"dir,omitempty" jsonschema:"Directory to search in. Defaults to '.'. Ignored when 'dirs' is non-empty."`
	Dirs             []string `json:"dirs,omitempty" jsonschema:"Multiple directories to search in one call. When non-empty, takes precedence over 'dir'. Each root's .gitignore is honoured independently when respect_gitignore is set."`
	Workers          int      `json:"workers,omitempty" jsonschema:"Number of parallel workers. Defaults to runtime.NumCPU()."`
	MaxLineBytes     int      `json:"max_line_bytes,omitempty" jsonschema:"Per-line scanner buffer cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default; raise for very long log lines."`
	TimeoutSeconds   *float64 `json:"timeout_seconds,omitempty" jsonschema:"Override the server's default per-call timeout for this invocation (in seconds; fractions allowed). Omit to use the server default (set when the MCP server was started). Pass 0 to disable the timeout for this call. On expiry the walk is cancelled and the partial result set is returned with cancelled=true."`
	SortBy           string   `json:"sort_by,omitempty" jsonschema:"Sort matches by attribute. Recognised keys: size, name, path, mod_time, word_count, line_count, page_count, duration, bitrate, sample_rate, video_height, video_width, frame_rate, iso, focal_length, taken_at, sent_at, year, entry_count, uncompressed_size, loc, attachment_count, email_count. Files missing the attribute group at the end. Sorting buffers the full result set (top-K is incoherent with streaming)."`
	Order            string   `json:"order,omitempty" jsonschema:"Sort direction when sort_by is set: 'asc' (default) or 'desc'."`
	Rank             string   `json:"rank,omitempty" jsonschema:"CEL expression returning double / int / bool — evaluated per file as a custom sort key. Higher values rank first. Composes with semantic_query (similarity is a CEL variable) AND keyword_query (bm25 is a CEL variable) for hybrid ranking, e.g. 'bm25*0.4 + similarity*0.6'. When set, overrides sort_by; default order is desc. Example: 'similarity * 0.7 + (mod_time > timestamp(\"2025-01-01T00:00:00Z\") ? 0.3 : 0.0)'."`
	KeywordQuery     string   `json:"keyword_query,omitempty" jsonschema:"Keyword query for Okapi BM25 relevance scoring. When set, each match's 'bm25' field + CEL variable is populated (IDF computed over the candidate result set) and results sort by bm25 desc by default. Compose with rank for custom weighting, e.g. 'bm25 * 0.5 + (is_recent ? 1.0 : 0.0)'. For hybrid keyword+SEMANTIC ranking (bm25 fused with embedding similarity), use the search_semantic tool's keyword_query/hybrid inputs — this tool has no embedding model. Reads each candidate's body; pair with a tight expr. Issue #335."`
	Limit            int      `json:"limit,omitempty" jsonschema:"Cap the returned match count. With sort_by, returns the top-N (after sorting). Without sort_by, returns the first N ordered by path. 0 = unlimited. When the result set is truncated by limit, the response carries an opaque next_cursor — pass it back as 'cursor' to fetch the next page."`
	Cursor           string   `json:"cursor,omitempty" jsonschema:"Opaque pagination token from a previous response's next_cursor. Resumes the result set immediately after the last item of the prior page, using the SAME sort_by/order/rank as that call (a mismatch errors). Paging is stable across calls under an unchanged tree; the walk is re-run each page (cheap — attributes are cached). Combine with 'limit' to stream a large match set in bounded pages."`
	IncludeSnippet   bool     `json:"include_snippet,omitempty" jsonschema:"When true, populate each match's 'snippet' field with the first N lines of the file body (see snippet_lines). Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave snippet empty."`
	SnippetLines     int      `json:"snippet_lines,omitempty" jsonschema:"How many lines to include per snippet (default 10). Ignored when include_snippet is false."`
	IncludeBody      bool     `json:"include_body,omitempty" jsonschema:"When true, the full file body is exposed to the CEL expression as the 'body' string variable, so filters like body.contains(\"transformer\") or body.matches(\"\\\\bAPI\\\\b\") run at search time. Only text-based content types populate body; capped at body_max_bytes (default 1 MiB). Expensive: reads every candidate file's body, not just headers — pair with tight expr / excludes / timeout."`
	BodyMaxBytes     int      `json:"body_max_bytes,omitempty" jsonschema:"Cap on the body string in bytes (default 1 MiB). Files larger than the cap are silently truncated; the prefix still participates in body.contains / body.matches. Ignored when include_body is false."`
	ComputeHashes    bool     `json:"compute_hashes,omitempty" jsonschema:"When true, populate md5 / sha1 / sha256 (lowercase hex) on each match and expose them as CEL variables. All three compute in one io.MultiWriter pass over the file and cache alongside (size, mtime) — subsequent runs on unchanged files are free. Off by default — hashing every match reads multi-GB videos / archives in full. Opt-in for forensic / NSRL / VirusTotal / threat-intel-feed workflows. Filter examples: 'is_binary && md5 == \"<IOC-hex>\"', 'is_image && sha256.startsWith(\"00\")'."`
	CheckDisguised   bool     `json:"check_disguised,omitempty" jsonschema:"When true, run both the name-based and magic-byte detection passes and populate magic_content_type / extension_content_type / is_disguised CEL variables. is_disguised fires when the bytes disagree with the extension. One extra 512-byte file read per match (cached). Filter examples: 'is_disguised && is_binary' (forensic-grade — disguised executables), 'is_disguised && magic_content_type.startsWith(\"binary/\")'."`
	WithXattrs       bool     `json:"with_xattrs,omitempty" jsonschema:"When true, populate the xattr family of CEL variables — xattr_keys, xattr_count, is_xattr_rich, is_quarantined, quarantine_agent / event_id / source_url / referrer_url / download_date / user_approved, finder_tags, finder_color, has_finder_comment. Darwin-only — non-Darwin walks leave these empty. Two syscalls (Listxattr + Getxattr) per match; off by default. Filter examples: 'is_mach_o && !is_codesigned && is_quarantined' (downloaded unsigned binaries — malware-triage classic), 'quarantine_source_url.contains(\"github.com\")', 'finder_color == \"red\"'."`
	OCRImages        bool     `json:"ocr_images,omitempty" jsonschema:"When true, run OCR over image/* files via the registered OCR provider (macOS Vision today; Linux Tesseract / Windows.Media.Ocr are future providers under the same hook). Populates 'body' with recognized text plus ocr_confidence (0..1 average), ocr_language (BCP-47 detected dominant language), ocr_provider (registered provider name). Works independently of include_body — passing ocr_images alone is enough for body.contains() queries on screenshots. Cached in the body cache (bodies_v1); second walk is free. Expensive on first walk (200ms-2s per image). On platforms without a registered provider, the flag is a no-op. Issue #189."`
	OCRTimeoutMS     int      `json:"ocr_timeout_ms,omitempty" jsonschema:"Per-file OCR timeout in milliseconds. Default 10000 (10s) when zero. The helper subprocess gets SIGKILL on ctx cancellation so a misbehaving image can't stall the walk."`
	VerifyC2PA       bool     `json:"verify_c2pa,omitempty" jsonschema:"When true, cryptographically VERIFY C2PA / Content Credentials on image/* files (pure-Go: COSE signature, certificate chain against the embedded C2PA trust list, hash bindings, RFC 3161 timestamp) and populate the VERIFIED attributes: c2pa_valid (bool — passed full validation), c2pa_verified_signer (trust-anchored signer cert CN/Org), c2pa_verified_signed_at (verified RFC 3161 timestamp), c2pa_validation_status (first C2PA failure code when invalid). These are the authenticated counterpart to the always-on, UNVERIFIED is_c2pa / c2pa_signed_by / c2pa_signed_at attributes. Off by default: real crypto per image, and the result is never cached (validity is clock-dependent). Find authentic Content Credentials: 'is_c2pa && c2pa_valid'; by verified signer: 'c2pa_valid && c2pa_verified_signer.contains(\"Adobe\")'. JPEG / PNG today. Issue #441."`
	WithPHash        bool     `json:"with_phash,omitempty" jsonschema:"When true, compute the 64-bit perceptual hash of every walked image and surface it as the 'phash' CEL string (16-char hex). Pair with the image_similar_to(phash, ref_path, threshold) CEL function to find visually-similar images. Auto-enabled when the expression references image_similar_to. Cached in the index. ~1ms per image. Issue #208."`
	HashAllowlistPath string  `json:"hash_allowlist_path,omitempty" jsonschema:"Path to a hash allowlist (newline-separated md5/sha1/sha256 hex, mixed algorithms auto-detected; # comments allowed) OR a pre-built bbolt hashset file. When set, populates is_known_good on each match. Forces compute_hashes on. NSRL / corp-allowlist / threat-intel-allowlist interop. Combine with '!is_known_good && is_binary' to cut a forensic disk image's review surface to known-unknown executables."`
	HashDenylistPath  string  `json:"hash_denylist_path,omitempty" jsonschema:"Path to a hash denylist (same format as hash_allowlist_path). Populates is_known_bad. Threat-intel-feed / IOC-list interop."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against the basename of each file/directory; matched directories are pruned. Example: ['node_modules', '.git', 'target', '*.bak']. Use respect_gitignore for path-aware patterns."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root (if present) and skip matching paths. Honours standard gitignore semantics. Nested .gitignore files in subdirectories are NOT honoured in this version."`
	FollowSymlinks   bool     `json:"follow_symlinks,omitempty" jsonschema:"When true, descend through symbolic links to directories. Off by default — symlinks-to-dirs surface as leaf entries with is_symlink=true. The is_symlink / target_path / is_broken_symlink CEL attributes are populated regardless of this flag. No loop detection — best avoided unless the tree is known acyclic."`
	ResolveProjects  bool     `json:"resolve_projects,omitempty" jsonschema:"When true, populate each match's 'project_types' (list<string>) and 'project_type' (string — first match) CEL variables by resolving the file's nearest project-root ancestor (go.mod, package.json, Cargo.toml, etc.). Enables queries like 'is_source && project_type == \"go\"' to find Go source inside actual Go modules. Opt-in: adds one ReadDir per unique dir walked (cached), so default-off avoids the cost when not needed."`
	WithGit         bool     `json:"with_git,omitempty" jsonschema:"When true, populate git-aware CEL variables (git_last_commit_time, git_last_commit_author, git_last_commit_subject, git_first_seen, git_commit_count, is_git_tracked, is_git_ignored) by running one 'git log' pass per walk root via the gitmeta package. Auto-enabled when expr / sort_by / rank reference a git_* / is_git_* attribute — pass with_git explicitly only when you need git data but your CEL doesn't name it. Cheap when the root IS a git tree (a single subprocess invocation up front, cached across calls via the MCP server's gitmeta.Pool); silent no-op when the root isn't (or when git isn't on PATH). Use to answer repo-aware time / author / churn queries that filesystem mtimes can't (a fresh clone has all mtimes set to checkout time). Filter examples: 'git_last_commit_time > timestamp(...)' (recent edits), 'is_source && git_commit_count > 50' (hot files), 'is_source && is_git_tracked && !is_test_file' (production code only). Issue #271."`
	PruneBuildArtefacts bool  `json:"prune_build_artefacts,omitempty" jsonschema:"When true, pre-walks each search root to discover project subdirectories and prunes the canonical build-artefact basenames for every detected project type — vendor (Go), node_modules (Node), target (Rust / Java Maven), __pycache__/.venv/.tox (Python), bin/obj (.NET), .terraform (Terraform), etc. Unioned with 'excludes'. Saves the boilerplate exclude list when searching monorepos or large multi-project trees. Opt-in: pre-walk I/O is proportional to tree size."`
	Profile             string `json:"profile,omitempty" jsonschema:"Narrow per-file Attributes parsing to a curated content-type set. Currently recognises 'code' — skips per-format parsing for image / audio / video / binary / office / archive / database / science / font / 3d / chat / browser / bookmark families on mixed source-+-media trees. Detection still runs so ContentType + is_X family flags populate; the expensive ContentType.Attributes call AND the attribute-cache write are skipped for the matched families (the cache skip is intentional — a profile-skipped entry has empty Extra and would poison a later profile-less walk). Empty / unknown values run the full parse (today's default). Issue #284."`
	Fields              []string `json:"fields,omitempty" jsonschema:"Project each match to only the listed attribute names — saves tokens when only a few attributes matter. 'path', 'content_type', and 'size' are always included regardless. Sort still works on attributes not in 'fields' (sort happens before projection). Empty / omitted returns every populated attribute. Unknown names error at request validation time; call 'list_attributes' for the canonical schema or check match field names with search.MatchFieldNames()."`
}

// SearchOutput is the structured output of the `search` tool.
//
// When Cancelled is true, the walk did not complete; Matches contains
// every result that was emitted by the walker before the deadline /
// signal fired. CancellationReason distinguishes "timeout" (server
// default or per-call timeout_seconds expired) from "client_cancel"
// (the MCP client closed the request or the parent ctx was cancelled
// for some other reason). ElapsedSeconds reports wall-clock time spent
// inside the search handler — useful for tuning timeouts.
type SearchOutput struct {
	CommonOutput
	Matches            []search.Match `json:"matches"`
	Count              int            `json:"count"`
	// NextCursor is an opaque token, present only when the result set was
	// truncated by Limit and more matches remain. Pass it back as the
	// 'cursor' input (with the same sort_by/order/rank) to fetch the next
	// page. Empty means this is the last page. Issue #336.
	NextCursor         string         `json:"next_cursor,omitempty"`
	Cancelled          bool           `json:"cancelled,omitempty"`
	CancellationReason string         `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64        `json:"elapsed_seconds,omitempty"`
	// Suggestions are agent-actionable hints generated heuristically
	// from the observed walk state when Cancelled=true. Issue #168
	// sub-feature C. Empty when the walk completed successfully OR
	// when no heuristic fires.
	Suggestions []string `json:"suggestions,omitempty"`
}

// progressNotifyStride is the number of matches between two
// notifications/progress messages. Smaller searches (< stride matches)
// emit zero notifications and just land in one final response. Tunable
// later via an Options field if a client needs finer granularity.
const progressNotifyStride = 50

func (h *handlers) searchHandler(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
	// Validate Fields up-front so a typo errors immediately rather
	// than walking the tree and then dropping every attribute. The
	// canonical name set lives on search.Match's json tags, not on
	// celexpr.Schema (Schema is the CEL-attribute view; Match includes
	// type-predicate fields like is_image that Schema doesn't surface).
	if err := search.ValidateFields(in.Fields); err != nil {
		return nil, SearchOutput{}, fmt.Errorf("fields: %w", err)
	}
	expr := in.Expr
	if expr == "" {
		expr = "true"
	}
	dir, err := expandHomeDir(in.Dir)
	if err != nil {
		return nil, SearchOutput{}, fmt.Errorf("expand dir: %w", err)
	}
	dirs, err := expandHomeDirs(in.Dirs)
	if err != nil {
		return nil, SearchOutput{}, fmt.Errorf("expand dirs: %w", err)
	}
	if dir == "" {
		dir = "."
	}
	if err := h.checkFollowSymlinks(in.FollowSymlinks); err != nil {
		return nil, SearchOutput{}, err
	}
	if dir, err = h.validatePath(dir); err != nil {
		return nil, SearchOutput{}, err
	}
	if dirs, err = h.validatePaths(dirs); err != nil {
		return nil, SearchOutput{}, err
	}
	if in.HashAllowlistPath != "" {
		if p, err := h.validatePath(in.HashAllowlistPath); err != nil {
			return nil, SearchOutput{}, err
		} else {
			in.HashAllowlistPath = p
		}
	}
	if in.HashDenylistPath != "" {
		if p, err := h.validatePath(in.HashDenylistPath); err != nil {
			return nil, SearchOutput{}, err
		} else {
			in.HashDenylistPath = p
		}
	}

	// parentCtx is captured before the timeout wrap so we can later
	// distinguish a server-level cancellation (transport close, parent
	// ctx) from our own timeout firing.
	parentCtx := ctx
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()

	// Load hash allowlist / denylist when supplied. Auto-detects
	// bbolt vs text format. Forces compute_hashes on so the
	// per-file hash trio is computed for membership lookup.
	var allowlist, denylist hashset.Set
	if in.HashAllowlistPath != "" {
		al, alErr := hashset.Open(in.HashAllowlistPath)
		if alErr != nil {
			return nil, SearchOutput{}, fmt.Errorf("load hash_allowlist_path: %w", alErr)
		}
		allowlist = al
		defer func() { _ = al.Close() }()
		in.ComputeHashes = true
	}
	if in.HashDenylistPath != "" {
		dl, dlErr := hashset.Open(in.HashDenylistPath)
		if dlErr != nil {
			return nil, SearchOutput{}, fmt.Errorf("load hash_denylist_path: %w", dlErr)
		}
		denylist = dl
		defer func() { _ = dl.Close() }()
		in.ComputeHashes = true
	}

	out := make(chan search.Result, 64)
	var walkErr error
	done := make(chan struct{})
	// The MCP handler always buffers results (it sorts by path
	// before returning) so we route sort/limit through search.Walk
	// rather than re-implementing the post-sort here. But progress
	// notifications + cancellation handling still want streaming —
	// so we feed the channel ourselves and sort/limit the collected
	// matches post-stream using the same sortAndLimit helper.
	// Multi-dir: in.Dirs wins when non-empty; else fall back to
	// the single 'dir' field (with default "." applied above).
	walkOpts := search.Options{
		Root:              dir,
		Roots:             dirs,
		Expr:              expr,
		Workers:           in.Workers,
		MaxLineBytes:      in.MaxLineBytes,
		IncludeAttributes: true,
		Index:             h.idx,
		SnippetLines:      in.SnippetLines,
		IncludeSnippet:    in.IncludeSnippet,
		IncludeBody:       in.IncludeBody,
		BodyMaxBytes:      in.BodyMaxBytes,
		OCRImages:         in.OCRImages,
		OCRTimeout:        time.Duration(in.OCRTimeoutMS) * time.Millisecond,
		VerifyC2PA:        in.VerifyC2PA,
		WithPHash:         in.WithPHash || strings.Contains(in.Expr, "image_similar_to"),
		ComputeHashes:     in.ComputeHashes,
		CheckDisguised:    in.CheckDisguised,
		ReadExtendedAttributes: in.WithXattrs,
		Allowlist:         allowlist,
		Denylist:          denylist,
		Excludes:          in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		FollowSymlinks:      in.FollowSymlinks,
		ResolveProjects:     in.ResolveProjects,
		PruneBuildArtefacts: in.PruneBuildArtefacts,
		Profile:             in.Profile,
		WithGit:             in.WithGit || celexpr.NeedsGit(in.Expr, in.SortBy, in.Rank),
		GitCachePool:        h.gitPool,
		// RankExpr IS passed to WalkStream because rank is evaluated
		// per file (during the walk), not post-collect. The eventual
		// sort happens below in the sortAndLimit block.
		RankExpr: in.Rank,
		// KeywordQuery enables BM25 keyword ranking. The walker captures
		// per-file carrier data; FinalizeBM25 (below) scores + ranks.
		KeywordQuery: in.KeywordQuery,
		// Sort, Order, Limit are applied via sortAndLimit AFTER we
		// collect — see end of handler. We don't pass them to
		// WalkStream because WalkStream doesn't honour them.
	}
	go func() {
		walkErr = search.WalkStream(ctx, walkOpts, content.DefaultRegistry(), out)
		close(done)
	}()

	// Drain the channel as matches arrive. Emit a progress notification
	// every `progressNotifyStride` matches when the client passed a
	// progressToken — the SDK's NotifyProgress is a no-op for clients
	// that didn't request progress.
	//
	// We collect raw search.Results here (not the projected
	// search.Match wire shape) so sort_by has access to the full
	// FileAttributes for per-family scalar keys. Projection happens
	// after the sort.
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
		return nil, SearchOutput{}, fmt.Errorf("walk: %w", walkErr)
	}

	// BM25 keyword ranking post-pass (issue #335). Computes candidate-set
	// IDF, populates each match's bm25 score, and sets Result.Rank (the
	// rank expression re-evaluated with bm25 bound, or raw bm25). Must
	// run on the collected set, before the sort below.
	if in.KeywordQuery != "" {
		if err := search.FinalizeBM25(results, walkOpts); err != nil {
			return nil, SearchOutput{}, fmt.Errorf("bm25: %w", err)
		}
	}

	// Resolve the effective sort key/order. Rank overrides sort_by:
	// when set, sort by rank desc by default (mirrors the CLI / walker).
	// A keyword query also writes Result.Rank, so it defaults the same.
	sortKey, order := in.SortBy, in.Order
	if in.Rank != "" || in.KeywordQuery != "" {
		sortKey = "rank"
		if order == "" {
			order = "desc"
		}
	}

	// Pagination path: a cursor OR a limit means the caller may be
	// paging, so route through PaginateResults — it sorts in a stable
	// total order (sort key + path tiebreak), resumes past the cursor,
	// caps to limit, and emits next_cursor when more remain. Empty
	// sortKey orders by path (the same default as the non-paged branch).
	var nextCursor string
	switch {
	case in.Cursor != "" || in.Limit > 0:
		page, next, perr := search.PaginateResults(results, sortKey, order, in.Cursor, in.Limit)
		if perr != nil {
			return nil, SearchOutput{}, fmt.Errorf("cursor: %w", perr)
		}
		results, nextCursor = page, next
	case sortKey != "":
		results = search.SortAndLimit(results, search.Options{Sort: sortKey, Order: order})
	default:
		// Historical contract: matches sorted by path when the caller
		// didn't request a specific sort.
		sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	}

	matches := make([]search.Match, len(results))
	for i, r := range results {
		matches[i] = search.MatchFrom(r)
	}
	// Projection happens AFTER sort_by so the sort can use any
	// attribute regardless of whether it's in the response. Empty
	// in.Fields → no-op (ProjectMatches checks).
	search.ProjectMatches(matches, in.Fields)

	output := SearchOutput{
		Matches:        matches,
		Count:          len(matches),
		NextCursor:     nextCursor,
		ElapsedSeconds: elapsed,
	}
	if cancelled {
		output.Cancelled = true
		// "timeout" when our deadline fired and the parent ctx is
		// still healthy; otherwise the parent (transport / client /
		// process signal) is the cause.
		if errors.Is(walkErr, context.DeadlineExceeded) && parentCtx.Err() == nil {
			output.CancellationReason = "timeout"
		} else {
			output.CancellationReason = "client_cancel"
		}
		// Partial-result hints (issue #168 sub-feature C). Generated
		// from the observed walk state — bumped timeout, hot
		// directory, expensive flags, missing prunes, lax filter.
		output.Suggestions = search.SuggestionsForSearch(walkOpts, output.Matches, output.ElapsedSeconds, output.CancellationReason)
	}
	// Always-on wrong-tool hint (issue #281). Fires regardless of
	// cancellation — the body.contains/body.matches → find_matches
	// nudge is most useful on the SUCCESSFUL "I got 50 paths, now
	// where are the matches?" path.
	if hint := search.BodyMatchSuggestion(in.Expr); hint != "" {
		output.Suggestions = append(output.Suggestions, hint)
	}
	output.ServerVersion = h.version
	return nil, output, nil
}
