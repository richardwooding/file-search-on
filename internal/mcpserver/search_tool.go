package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/richardwooding/file-search-on/internal/content"
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
	Limit            int      `json:"limit,omitempty" jsonschema:"Cap the returned match count. With sort_by, returns the top-N (after sorting). Without sort_by, returns the first N in walk order. 0 = unlimited."`
	IncludeSnippet   bool     `json:"include_snippet,omitempty" jsonschema:"When true, populate each match's 'snippet' field with the first N lines of the file body (see snippet_lines). Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave snippet empty."`
	SnippetLines     int      `json:"snippet_lines,omitempty" jsonschema:"How many lines to include per snippet (default 10). Ignored when include_snippet is false."`
	IncludeBody      bool     `json:"include_body,omitempty" jsonschema:"When true, the full file body is exposed to the CEL expression as the 'body' string variable, so filters like body.contains(\"transformer\") or body.matches(\"\\\\bAPI\\\\b\") run at search time. Only text-based content types populate body; capped at body_max_bytes (default 1 MiB). Expensive: reads every candidate file's body, not just headers — pair with tight expr / excludes / timeout."`
	BodyMaxBytes     int      `json:"body_max_bytes,omitempty" jsonschema:"Cap on the body string in bytes (default 1 MiB). Files larger than the cap are silently truncated; the prefix still participates in body.contains / body.matches. Ignored when include_body is false."`
	Excludes         []string `json:"excludes,omitempty" jsonschema:"Glob patterns matched against the basename of each file/directory; matched directories are pruned. Example: ['node_modules', '.git', 'target', '*.bak']. Use respect_gitignore for path-aware patterns."`
	RespectGitignore bool     `json:"respect_gitignore,omitempty" jsonschema:"When true, parse a .gitignore at the walk root (if present) and skip matching paths. Honours standard gitignore semantics. Nested .gitignore files in subdirectories are NOT honoured in this version."`
	ResolveProjects  bool     `json:"resolve_projects,omitempty" jsonschema:"When true, populate each match's 'project_types' (list<string>) and 'project_type' (string — first match) CEL variables by resolving the file's nearest project-root ancestor (go.mod, package.json, Cargo.toml, etc.). Enables queries like 'is_source && project_type == \"go\"' to find Go source inside actual Go modules. Opt-in: adds one ReadDir per unique dir walked (cached), so default-off avoids the cost when not needed."`
	PruneBuildArtefacts bool  `json:"prune_build_artefacts,omitempty" jsonschema:"When true, pre-walks each search root to discover project subdirectories and prunes the canonical build-artefact basenames for every detected project type — vendor (Go), node_modules (Node), target (Rust / Java Maven), __pycache__/.venv/.tox (Python), bin/obj (.NET), .terraform (Terraform), etc. Unioned with 'excludes'. Saves the boilerplate exclude list when searching monorepos or large multi-project trees. Opt-in: pre-walk I/O is proportional to tree size."`
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
	Matches            []search.Match `json:"matches"`
	Count              int           `json:"count"`
	Cancelled          bool          `json:"cancelled,omitempty"`
	CancellationReason string        `json:"cancellation_reason,omitempty"`
	ElapsedSeconds     float64       `json:"elapsed_seconds,omitempty"`
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
	dir := in.Dir
	if dir == "" {
		dir = "."
	}

	// parentCtx is captured before the timeout wrap so we can later
	// distinguish a server-level cancellation (transport close, parent
	// ctx) from our own timeout firing.
	parentCtx := ctx
	var cancel context.CancelFunc
	ctx, cancel = h.resolveTimeout(ctx, in.TimeoutSeconds)
	defer cancel()

	start := time.Now()

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
		Roots:             in.Dirs,
		Expr:              expr,
		Workers:           in.Workers,
		MaxLineBytes:      in.MaxLineBytes,
		IncludeAttributes: true,
		Index:             h.idx,
		SnippetLines:      in.SnippetLines,
		IncludeSnippet:    in.IncludeSnippet,
		IncludeBody:       in.IncludeBody,
		BodyMaxBytes:      in.BodyMaxBytes,
		Excludes:          in.Excludes,
		RespectGitignore:    in.RespectGitignore,
		ResolveProjects:     in.ResolveProjects,
		PruneBuildArtefacts: in.PruneBuildArtefacts,
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

	// Order: explicit sort_by > legacy path-sort default. Limit is
	// applied AFTER sorting so the response respects top-K
	// semantics. sortAndLimit lives in internal/search next to the
	// type-aware compareByKey so this stays the single source of
	// truth for sort/limit logic.
	if in.SortBy != "" || in.Limit > 0 {
		sortOpts := search.Options{Sort: in.SortBy, Order: in.Order, Limit: in.Limit}
		results = search.SortAndLimit(results, sortOpts)
	} else {
		// Historical contract: matches sorted by path. Preserve it
		// when the caller didn't request a specific sort.
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
	}
	return nil, output, nil
}
