package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"text/template"
	"time"

	"github.com/alecthomas/kong"
	"github.com/richardwooding/file-search-on/internal/celexpr"
	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/mcpserver"
	"github.com/richardwooding/file-search-on/internal/projecttype"
	"github.com/richardwooding/file-search-on/internal/search"
)

// exitCodeError lets a subcommand request a specific process exit code.
// main() type-switches on it via errors.As; the wrapped msg is used only
// if a code is paired with a non-empty diagnostic, which it usually
// isn't (subcommands typically print their own stderr explanation).
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.msg
}

// isCancellation reports whether err is one of the context-cancellation
// signals (deadline-exceeded / canceled). Used to fork the post-walk
// path between "real error" and "partial results due to ctx".
func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var CLI struct {
	ProjectTypeConfig string           `name:"project-type-config" help:"Path to a YAML config registering custom project types (CEL-driven or file-based indicators). Loaded LAST, after any auto-discovered configs. Loaded before any subcommand runs; the new types appear alongside built-ins in detect-project / find-projects / search results."`
	NoConfigSearch    bool             `name:"no-config-search" help:"Skip automatic discovery of project-type configs at the standard search paths (user-wide UserConfigDir()/file-search-on/project-types.yaml and per-project ./.file-search-on/project-types.yaml). Use for hermetic invocations (tests, CI) where only the explicit --project-type-config should apply."`
	Search     SearchCmd        `cmd:"" help:"Search for files matching a CEL expression." default:"withargs"`
	Attrs      AttrsCmd         `cmd:"" name:"attrs" help:"Print attributes for a single file (no walk, no CEL)."`
	Stats      StatsCmd         `cmd:"" name:"stats" help:"Aggregate content-type counts and total sizes for a directory tree."`
	Lines      LinesCmd         `cmd:"" name:"lines" help:"Print a range of lines from a single file (no walk, no CEL)."`
	Duplicates DuplicatesCmd    `cmd:"" name:"duplicates" help:"Find groups of byte-identical files by sha256 hash."`
	Detect     DetectProjectCmd `cmd:"" name:"detect-project" help:"Identify project type(s) (go / node / rust / …) for a directory by checking canonical indicator files."`
	Projects   FindProjectsCmd  `cmd:"" name:"find-projects" help:"Walk a root and list every project subdirectory under it."`
	MCP        MCPCmd           `cmd:"" name:"mcp" help:"Run as a Model Context Protocol server (stdio, http, or sse)."`
	Version    kong.VersionFlag `short:"V" help:"Print version and exit."`
}

type AttrsCmd struct {
	Path   string `arg:"" help:"File to inspect."`
	Output string `short:"o" name:"output" enum:"default,verbose,json" default:"verbose" help:"Output format: default | verbose | json."`
	Format string `name:"format" help:"Custom Go text/template applied to the record (e.g. '{{.Path}}\\t{{.Title}}'). When set, takes precedence over -o."`
}

func (a *AttrsCmd) Run(ctx context.Context) error {
	abs, err := filepath.Abs(a.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; use the search subcommand to walk a tree", abs)
	}

	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	attrs, err := celexpr.BuildAttributes(ctx, os.DirFS(dir), base, abs, contentpkg.DefaultRegistry())
	if err != nil {
		return fmt.Errorf("read attributes: %w", err)
	}

	result := search.Result{
		Path:        abs,
		ContentType: attrs.ContentType,
		Size:        attrs.Size,
		Attrs:       attrs,
	}
	results := []search.Result{result}

	if a.Format != "" {
		tmpl, err := parseFormatTemplate(a.Format)
		if err != nil {
			return fmt.Errorf("parse --format template: %w", err)
		}
		return printTemplate(os.Stdout, results, tmpl)
	}
	switch a.Output {
	case "json":
		return printJSON(os.Stdout, results)
	case "default":
		printDefault(os.Stdout, results)
	default: // "" or "verbose"
		printVerbose(os.Stdout, results)
	}
	return nil
}

type LinesCmd struct {
	Path     string `arg:"" help:"File to read lines from."`
	Start    int    `short:"s" name:"start" help:"First line to print (1-indexed, inclusive)." default:"1"`
	End      int    `short:"e" name:"end" help:"Last line to print (1-indexed, inclusive). 0 = end of file." default:"0"`
	MaxLines int    `name:"max-lines" help:"Cap on lines returned. 0 uses the 1000-line default." default:"0"`
	Output   string `short:"o" name:"output" enum:"text,json" default:"text" help:"Output format: text (default; raw lines) | json (machine-readable with start/end/total/truncated)."`
}

func (l *LinesCmd) Run(ctx context.Context) error {
	abs, err := filepath.Abs(l.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", abs)
	}
	dir := filepath.Dir(abs)
	base := filepath.Base(abs)

	res, err := search.ReadLines(ctx, os.DirFS(dir), base, l.Start, l.End, l.MaxLines)
	if err != nil {
		return fmt.Errorf("read lines: %w", err)
	}

	if l.Output == "json" {
		return printLinesJSON(os.Stdout, abs, res)
	}
	for _, line := range res.Lines {
		_, _ = fmt.Fprintln(os.Stdout, line)
	}
	if res.Truncated {
		fmt.Fprintf(os.Stderr, "(truncated at %d lines; total lines in file: %d)\n", len(res.Lines), res.TotalLines)
	}
	return nil
}

type StatsCmd struct {
	Dir              []string      `short:"d" help:"Directory to walk. Repeatable — pass -d ./a -d ./b to aggregate stats across multiple roots in one call." default:"."`
	Expr             string        `arg:"" help:"Optional CEL expression to scope the stats (e.g. 'is_markdown' for markdown-only counts). Defaults to matching every file." optional:""`
	Workers          int           `short:"w" help:"Parallel workers." default:"0"`
	MaxLineBytes     int           `short:"L" name:"max-line-bytes" help:"Per-line scanner cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default." default:"0"`
	IndexPath        string        `name:"index-path" help:"Persistent attribute index file (bbolt); see search subcommand."`
	Timeout          time.Duration `name:"timeout" help:"Maximum walk duration. On expiry, the partial histogram is still printed and the process exits 124."`
	Exclude          []string      `name:"exclude" help:"Glob pattern matched against file/dir basenames; matches are skipped. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at the walk root and skip matching paths."`
	GroupBy          string        `name:"group-by" help:"Attribute to bucket by. Default 'content_type'. Recognised: content_type, ext, dir, language, camera_make, camera_model, lens, artist, album, genre, kernel, binary_format, binary_type, frontmatter_format. Unknown values fall back to content_type."`
	Output           string        `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table (default; human-readable) | json (machine-readable)."`
}

func (s *StatsCmd) Run(ctx context.Context) error {
	expr := s.Expr
	if expr == "" {
		expr = "true"
	}

	parentCtx := ctx
	effectiveCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	var idx index.Index
	if s.IndexPath != "" {
		var err error
		idx, err = openIndex(s.IndexPath)
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()
	}

	stats, err := search.ComputeStats(effectiveCtx, search.Options{
		Roots:            s.Dir,
		Expr:             expr,
		Workers:          s.Workers,
		MaxLineBytes:     s.MaxLineBytes,
		Index:            idx,
		Excludes:         s.Exclude,
		RespectGitignore: s.RespectGitignore,
		GroupBy:          s.GroupBy,
	}, contentpkg.DefaultRegistry())

	// Print even on cancellation — ComputeStats returns the partial
	// tally with Cancelled=true rather than nil.
	if stats != nil {
		if s.Output == "json" {
			if err := printStatsJSON(os.Stdout, stats); err != nil {
				return err
			}
		} else {
			printStatsTable(os.Stdout, stats)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("stats failed: %w", err)
	}
	// Same exit-code contract as search: 124 on timeout, 130 on
	// Ctrl-C, otherwise 0 (partial results aren't a hard failure).
	if stats != nil && stats.Cancelled {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "stats interrupted; counts above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case s.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "stats timed out after %s; counts above may be incomplete\n", s.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}

type DuplicatesCmd struct {
	Dir              []string      `short:"d" help:"Directory to walk. Repeatable for multi-root duplicate detection." default:"."`
	Expr             string        `arg:"" help:"Optional CEL expression to scope candidates (e.g. 'is_image' for photo dedup). Defaults to every file." optional:""`
	Workers          int           `short:"w" help:"Parallel workers." default:"0"`
	MaxLineBytes     int           `short:"L" name:"max-line-bytes" help:"Per-line scanner cap for text/CSV/HTML (bytes). 0 uses the 1 MiB default." default:"0"`
	IndexPath        string        `name:"index-path" help:"Persistent attribute index file (bbolt). Caches sha256 hashes alongside other attributes; repeat runs on an unchanged tree don't re-read any bytes."`
	Timeout          time.Duration `name:"timeout" help:"Maximum duration. On expiry, the partial result is still printed and the process exits 124."`
	Exclude          []string      `name:"exclude" help:"Glob pattern matched against file/dir basenames; matches are skipped. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	MinSize          int64         `name:"min-size" default:"0" help:"Skip files smaller than this many bytes. 0 considers every file; raise to e.g. 4096 to ignore tiny duplicates that aren't worth reclaiming."`
	Output           string        `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table (default; human-readable) | json (machine-readable)."`
}

func (d *DuplicatesCmd) Run(ctx context.Context) error {
	expr := d.Expr
	if expr == "" {
		expr = "true"
	}

	parentCtx := ctx
	effectiveCtx := ctx
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	var idx index.Index
	if d.IndexPath != "" {
		var err error
		idx, err = openIndex(d.IndexPath)
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()
	}

	dups, err := search.FindDuplicates(effectiveCtx, search.Options{
		Roots:            d.Dir,
		Expr:             expr,
		Workers:          d.Workers,
		MaxLineBytes:     d.MaxLineBytes,
		Index:            idx,
		Excludes:         d.Exclude,
		RespectGitignore: d.RespectGitignore,
		MinSize:          d.MinSize,
	}, contentpkg.DefaultRegistry())

	// Print even on cancellation — FindDuplicates returns the
	// partial set with Cancelled=true rather than nil.
	if dups != nil {
		if d.Output == "json" {
			if err := printDuplicatesJSON(os.Stdout, dups); err != nil {
				return err
			}
		} else {
			printDuplicatesTable(os.Stdout, dups)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("duplicates failed: %w", err)
	}
	if dups != nil && dups.Cancelled {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "duplicates interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case d.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "duplicates timed out after %s; results above may be incomplete\n", d.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}

// DetectProjectCmd inspects a single directory and prints which
// project type(s) it matches. Non-recursive — only the directory's
// own listing is read.
type DetectProjectCmd struct {
	Dir    string `arg:"" optional:"" help:"Directory to inspect. Defaults to '.'." default:"."`
	Output string `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json."`
}

func (d *DetectProjectCmd) Run(_ context.Context) error {
	abs, err := filepath.Abs(d.Dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	matches := projecttype.Detect(nil, abs)
	if d.Output == "json" {
		return printDetectProjectJSON(os.Stdout, abs, matches)
	}
	printDetectProject(os.Stdout, abs, matches)
	return nil
}

// FindProjectsCmd walks a root and prints every project subdirectory
// it finds. Default behaviour: stop at the first match per branch
// (the 'find me all my Go repos' shape). Pass --nested to also surface
// sub-projects inside matched roots.
type FindProjectsCmd struct {
	Dir              string        `arg:"" optional:"" help:"Root directory to walk. Defaults to '.'." default:"."`
	Type             []string      `name:"type" help:"Restrict to specific project types. Repeatable: --type go --type rust."`
	Exclude          []string      `name:"exclude" help:"Basename glob pruned during the walk (e.g. node_modules, .git, target). Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse .gitignore at the walk root and skip matching paths."`
	Nested           bool          `name:"nested" help:"Keep descending into matched project roots so nested sub-projects are also reported."`
	Timeout          time.Duration `name:"timeout" help:"Maximum duration. On expiry, the partial result is still printed and the process exits 124."`
	Output           string        `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json."`
}

func (f *FindProjectsCmd) Run(ctx context.Context) error {
	abs, err := filepath.Abs(f.Dir)
	if err != nil {
		return fmt.Errorf("resolve dir: %w", err)
	}
	result, err := projecttype.Find(ctx, abs, projecttype.FindOptions{
		Types:            f.Type,
		Excludes:         f.Exclude,
		RespectGitignore: f.RespectGitignore,
		Nested:           f.Nested,
		Timeout:          f.Timeout,
	})
	if err != nil && !isCancellation(err) {
		return fmt.Errorf("find-projects failed: %w", err)
	}
	if result != nil {
		if f.Output == "json" {
			if err := printFindProjectsJSON(os.Stdout, result); err != nil {
				return err
			}
		} else {
			printFindProjects(os.Stdout, result)
		}
	}
	if result != nil && result.Cancelled {
		switch result.CancellationReason {
		case "client_cancel":
			fmt.Fprintln(os.Stderr, "find-projects interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case "timeout":
			fmt.Fprintf(os.Stderr, "find-projects timed out after %s; results above may be incomplete\n", f.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}

type MCPCmd struct {
	Transport string        `name:"transport" enum:"stdio,http,sse" default:"stdio" help:"Transport: stdio (default; for desktop clients), http (Streamable HTTP, MCP 2025-03-26), or sse (DEPRECATED — HTTP+SSE, MCP 2024-11-05)."`
	Addr      string        `name:"addr" default:":8080" help:"host:port to bind for http or sse transports. Ignored for stdio."`
	Path      string        `name:"path" default:"/" help:"URL path prefix the handler is mounted at. Ignored for stdio."`
	IndexPath string        `name:"index-path" help:"Persistent attribute index file (bbolt). When unset the server uses an in-memory cache that lives for the process lifetime; setting this makes the cache survive restarts. The file is created on first use."`
	Timeout   time.Duration `name:"timeout" default:"60s" help:"Default per-tool-call timeout (Go duration: 30s, 2m, 5m). Each search/read_attributes invocation is wrapped with this deadline. Per-call 'timeout_seconds' input on the search tool overrides this. Set to 0 to disable the default (not recommended — long-running calls can exceed MCP client read deadlines)."`
}

func (m *MCPCmd) Run(ctx context.Context) error {
	idx, err := openIndex(m.IndexPath)
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	switch m.Transport {
	case "http":
		return mcpserver.RunHTTP(ctx, version, m.Addr, m.Path, idx, m.Timeout)
	case "sse":
		fmt.Fprintln(os.Stderr, "warning: --transport sse is DEPRECATED (MCP 2024-11-05); prefer --transport http for new clients.")
		return mcpserver.RunSSE(ctx, version, m.Addr, m.Path, idx, m.Timeout)
	default:
		return mcpserver.Run(ctx, version, idx, m.Timeout)
	}
}

// openIndex returns an index.Index for the given path. Empty path
// means in-memory only. On schema mismatch it surfaces a helpful
// "delete or re-point --index-path" message rather than a raw error.
func openIndex(path string) (index.Index, error) {
	if path == "" {
		return index.NewMemory(), nil
	}
	idx, err := index.Open(path)
	if err != nil {
		if errors.Is(err, index.ErrSchemaMismatch) {
			return nil, fmt.Errorf("index file at %s has an incompatible schema; delete it or pass a new --index-path", path)
		}
		return nil, fmt.Errorf("open index: %w", err)
	}
	return idx, nil
}

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
	Order            string        `name:"order" enum:"asc,desc" default:"asc" help:"Sort direction. Ignored without --sort."`
	Limit            int           `name:"limit" default:"0" help:"Cap the result set at N matches. With --sort, returns the top-N (after sorting). Without --sort, returns the first N in walk order. 0 = unlimited."`
	Snippet          bool          `name:"snippet" help:"Read a snippet of each match's body (first N lines, see --snippet-lines) and include it in verbose/json/template output. Only text-based content types (markdown / text / html / csv / json / xml / source/*) populate; binary families leave the snippet empty."`
	SnippetLines     int           `name:"snippet-lines" default:"10" help:"How many lines of body content to include per match when --snippet is set."`
	Body             bool          `name:"body" help:"Make file body available to the CEL expression as the 'body' string variable. Pair with CEL's built-in string methods to filter on content: --body 'is_markdown && body.contains(\"transformer\")', or for regex: --body 'is_source && body.matches(\"(?i)\\\\bTODO\\\\b\")'. Only text-based content types populate; the body is capped at --body-max-bytes (default 1 MiB). Expensive: reads every candidate file's body, not just headers."`
	BodyMaxBytes     int           `name:"body-max-bytes" default:"0" help:"Cap on the body string read per file in bytes. 0 uses the 1 MiB default. Files larger than the cap are silently truncated; the prefix still participates in the CEL filter."`
	Exclude          []string      `name:"exclude" help:"Glob pattern matched against the basename of each file/directory; matches are skipped (directories are pruned). Repeatable: --exclude node_modules --exclude '*.bak'."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at the walk root (if present) and skip matching paths. Nested .gitignore files in subdirectories are NOT honoured in this version."`
	ResolveProjects  bool          `name:"resolve-projects" help:"Populate the 'project_types' (list<string>) and 'project_type' (string) CEL variables for each match by resolving the file's containing project root (go.mod, package.json, Cargo.toml, …). Enables filters like 'is_source && project_type == \"go\"'. Adds one ReadDir per unique directory walked (cached) — opt-in to avoid the cost when not needed."`
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
	includeAttrs := s.Format != "" || s.Output == "verbose" || s.Output == "json" || s.Sort != "" || s.Snippet

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
		idx, err = openIndex(s.IndexPath)
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

	opts := search.Options{
		Roots:             s.Dir,
		Expr:              s.Expr,
		Workers:           s.Workers,
		MaxLineBytes:      s.MaxLineBytes,
		IncludeAttributes: includeAttrs,
		Index:             idx,
		Sort:              s.Sort,
		Order:             s.Order,
		Limit:             s.Limit,
		IncludeSnippet:    s.Snippet,
		SnippetLines:      s.SnippetLines,
		IncludeBody:       s.Body,
		BodyMaxBytes:      s.BodyMaxBytes,
		Excludes:          s.Exclude,
		RespectGitignore:  s.RespectGitignore,
		ResolveProjects:   s.ResolveProjects,
	}

	// --sort and --limit both need the full result set in memory
	// (sort, then truncate), so they force buffered mode regardless
	// of --unsorted / -o bare / json / --format. Streaming +
	// top-K is incoherent; bail to buffered.
	forceBuffered := s.Sort != "" || s.Limit > 0
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
	}

	// Distinguish timeout, Ctrl-C, and real errors. The stream/buffered
	// helpers print whatever they collected before returning the ctx
	// error, so stdout already reflects the partial set.
	if isCancellation(runErr) {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "search interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case s.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "search timed out after %s; results above may be incomplete\n", s.Timeout)
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
	// ordering. With --sort set, results are already in the order
	// Walk produced; re-sorting would defeat the flag.
	if opts.Sort == "" {
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

func main() {
	// Bridge OS signals into a cancellable ctx so subcommands shut down
	// cleanly: HTTP server gets graceful Shutdown, walker workers exit,
	// etc. Stop the relay on return so a second Ctrl-C falls through to
	// the default runtime handler and abruptly kills the process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	kctx := kong.Parse(&CLI,
		kong.Name("file-search-on"),
		kong.Description("Content-type aware file search with CEL attribute filtering."),
		kong.UsageOnError(),
		kong.Vars{"version": fmt.Sprintf("file-search-on %s (commit %s, built %s)", version, commit, date)},
		kong.BindTo(ctx, (*context.Context)(nil)),
	)
	// Load custom project types before the subcommand runs so they
	// appear in every project-aware surface (detect-project,
	// find-projects, --resolve-projects search, MCP tools when the
	// mcp subcommand wires the same path). Precedence (later layers
	// register on top of earlier):
	//   1. Auto-discovered configs from standard paths (gated by
	//      --no-config-search; default on).
	//   2. Explicit --project-type-config path.
	if !CLI.NoConfigSearch {
		if _, err := projecttype.LoadDiscovered(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}
	if CLI.ProjectTypeConfig != "" {
		if _, err := projecttype.LoadFromFile(CLI.ProjectTypeConfig); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
	}
	if err := kctx.Run(); err != nil {
		var ece *exitCodeError
		if errors.As(err, &ece) {
			// The subcommand has already printed its own diagnostic
			// to stderr; surface only the exit code.
			os.Exit(ece.code)
		}
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
