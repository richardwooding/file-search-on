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
	NearDuplicates NearDuplicatesCmd `cmd:"" name:"near-duplicates" help:"Find groups of similar (not identical) files by SimHash fingerprint of their extracted body. Complements 'duplicates' for fuzzy matching — catches files with trailing-newline edits, regenerated headers, typo fixes, template copies."`
	ArchiveContents ArchiveContentsCmd `cmd:"" name:"archive-contents" help:"List or filter entries inside a ZIP / TAR / TAR.GZ / GZIP archive. Per-entry CEL evaluation against the SAME vocabulary the top-level search uses — every is_X predicate and per-family attribute applies inside archives."`
	ArchiveRead     ArchiveReadCmd     `cmd:"" name:"archive-read" help:"Read a single file's content out of a ZIP / TAR / TAR.GZ / GZIP archive without extracting. Returns the bytes plus detected content_type + attributes."`
	FindMatches FindMatchesCmd  `cmd:"" name:"find-matches" help:"Scan text files for an RE2 regex; report line-level hits with optional context windows (combines CEL type-pruning with grep-style output)."`
	Detect      DetectProjectCmd `cmd:"" name:"detect-project" help:"Identify project type(s) (go / node / rust / …) for a directory by checking canonical indicator files."`
	Projects    FindProjectsCmd  `cmd:"" name:"find-projects" help:"Walk a root and list every project subdirectory under it."`
	WhichProject WhichProjectCmd `cmd:"" name:"which-project" help:"Given a file or directory path, walk up the chain and identify the nearest enclosing project root and type(s)."`
	ConfigPaths ConfigPathsCmd   `cmd:"" name:"config-paths" help:"Print the project-type config search paths for this platform. Use to discover where to drop your user-wide config (mkdir -p \"$(file-search-on config-paths -o bare | head -1 | xargs dirname)\")."`
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
	FollowSymlinks   bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories. Off by default; symlinks-to-dirs surface as is_symlink=true leaf entries."`
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
		idx, err = openIndex(s.IndexPath, index.BodyCacheCap{})
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
		FollowSymlinks:   s.FollowSymlinks,
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
	FollowSymlinks   bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories. Off by default; symlinks-to-dirs surface as is_symlink=true leaf entries. Useful for duplicates audits where symlinked copies should be deduplicated."`
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
		idx, err = openIndex(d.IndexPath, index.BodyCacheCap{})
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
		FollowSymlinks:   d.FollowSymlinks,
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

type NearDuplicatesCmd struct {
	Dir              []string      `short:"d" help:"Directory to walk. Repeatable for multi-root near-duplicate detection." default:"."`
	Expr             string        `arg:"" help:"Optional CEL expression to scope candidates (e.g. 'is_markdown && word_count > 100'). Defaults to every text-shaped file." optional:""`
	Threshold        float64       `name:"threshold" default:"0.85" help:"Minimum similarity (0..1) at which two files are considered near-duplicates. 0.85 ≈ 9 bits Hamming distance on a 64-bit SimHash. 0.95 ≈ 3 bits (whitespace / typo edits only). 0.75 ≈ 16 bits (significant overlap, different docs)."`
	Workers          int           `short:"w" help:"Parallel workers. 0 = runtime.NumCPU()." default:"0"`
	MaxLineBytes     int           `short:"L" name:"max-line-bytes" help:"Per-line scanner cap (bytes). 0 uses the 1 MiB default." default:"0"`
	BodyMaxBytes     int           `name:"body-max-bytes" default:"0" help:"Cap on the body read per file in bytes. 0 uses the 1 MiB default. Files larger than the cap are silently truncated; the prefix still participates in the fingerprint."`
	IndexPath        string        `name:"index-path" help:"Persistent attribute index file (bbolt). Caches the per-file SimHash fingerprint; repeat runs on an unchanged tree skip the body read AND the SimHash compute."`
	Timeout          time.Duration `name:"timeout" help:"Maximum duration. On expiry, the partial result is still printed and the process exits 124."`
	Exclude          []string      `name:"exclude" help:"Glob pattern matched against file/dir basenames; matches are skipped. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks   bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories. Off by default."`
	MinSize          int64         `name:"min-size" default:"0" help:"Skip files smaller than this many bytes (on-disk size, not extracted body)."`
	Output           string        `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table (default; human-readable) | json (machine-readable)."`
}

func (n *NearDuplicatesCmd) Run(ctx context.Context) error {
	expr := n.Expr
	if expr == "" {
		expr = "true"
	}

	parentCtx := ctx
	effectiveCtx := ctx
	if n.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, n.Timeout)
		defer cancel()
	}

	var idx index.Index
	if n.IndexPath != "" {
		var err error
		idx, err = openIndex(n.IndexPath, index.BodyCacheCap{})
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()
	}

	dups, err := search.FindNearDuplicates(effectiveCtx, search.Options{
		Roots:               n.Dir,
		Expr:                expr,
		Workers:             n.Workers,
		MaxLineBytes:        n.MaxLineBytes,
		BodyMaxBytes:        n.BodyMaxBytes,
		Index:               idx,
		Excludes:            n.Exclude,
		RespectGitignore:    n.RespectGitignore,
		FollowSymlinks:      n.FollowSymlinks,
		MinSize:             n.MinSize,
		SimilarityThreshold: n.Threshold,
	}, contentpkg.DefaultRegistry())

	if dups != nil {
		if n.Output == "json" {
			if err := printNearDuplicatesJSON(os.Stdout, dups); err != nil {
				return err
			}
		} else {
			printNearDuplicatesTable(os.Stdout, dups)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("near-duplicates failed: %w", err)
	}
	if dups != nil && dups.Cancelled {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "near-duplicates interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case n.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "near-duplicates timed out after %s; results above may be incomplete\n", n.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}

type ArchiveContentsCmd struct {
	Archive           string        `arg:"" help:"Path to the archive file (.zip / .tar / .tar.gz / .gz)."`
	Expr              string        `name:"expr" short:"e" help:"Optional CEL expression to filter entries (e.g. 'is_source && language == \"go\"'). Empty matches every entry."`
	Glob              string        `name:"glob" help:"Optional filepath.Match basename pattern applied BEFORE the CEL filter as a cheap pre-prune (e.g. '*.go')."`
	IncludeAttributes bool          `name:"include-attributes" help:"Include the full per-entry attribute map in the output. Off by default for terse listings."`
	Body              bool          `name:"body" help:"Read entry bodies so body.contains() / body.matches() CEL filters fire. Capped at --entry-read-cap. Bypasses the entry-list cache (bodies aren't cached)."`
	EntryReadCap      int64         `name:"entry-read-cap" default:"0" help:"Cap on per-entry bytes read into memory for detection and body evaluation (bytes). 0 uses the 8 MiB default — enough for typical PDF / DOCX / EPUB / email bodies inside archives. Raise for archives containing huge documents; lower if memory pressure matters on large collections."`
	MaxEntries        int           `name:"max" default:"0" help:"Cap on entries returned. 0 = unlimited."`
	IndexPath         string        `name:"index-path" help:"Persistent attribute index file (bbolt). When set, per-archive entry-list cache is consulted before each walk and populated on miss."`
	Timeout           time.Duration `name:"timeout" help:"Maximum walk duration. On expiry the partial set is still printed and the process exits 124."`
	Output            string        `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json."`
}

func (c *ArchiveContentsCmd) Run(ctx context.Context) error {
	parentCtx := ctx
	effectiveCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	var idx index.Index
	if c.IndexPath != "" {
		var err error
		idx, err = openIndex(c.IndexPath, index.BodyCacheCap{})
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()
	}

	result, err := search.WalkArchiveEntries(effectiveCtx, c.Archive, search.ArchiveWalkOptions{
		Expr:              c.Expr,
		Glob:              c.Glob,
		IncludeAttributes: c.IncludeAttributes,
		IncludeBody:       c.Body,
		EntryReadCap:      c.EntryReadCap,
		MaxEntries:        c.MaxEntries,
		Index:             idx,
	}, contentpkg.DefaultRegistry())

	if result != nil {
		if c.Output == "json" {
			if err := printArchiveContentsJSON(os.Stdout, result); err != nil {
				return err
			}
		} else {
			printArchiveContentsTable(os.Stdout, result)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("archive-contents failed: %w", err)
	}
	if result != nil && result.Cancelled {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "archive-contents interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case c.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "archive-contents timed out after %s; results above may be incomplete\n", c.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}

type ArchiveReadCmd struct {
	Archive  string        `arg:"" help:"Path to the archive file (.zip / .tar / .tar.gz / .gz)."`
	Entry    string        `arg:"" help:"Exact entry path inside the archive (e.g. 'src/main.go')."`
	MaxBytes int64         `name:"max-bytes" default:"0" help:"Cap on bytes returned. 0 uses the 1 MiB default. Files larger than the cap are silently truncated; the prefix is still returned."`
	Output   string        `short:"o" name:"output" enum:"raw,json" default:"raw" help:"Output format: raw (entry content to stdout) | json (envelope with metadata + content)."`
	Timeout  time.Duration `name:"timeout" help:"Maximum duration. On expiry the process exits 124."`
}

func (c *ArchiveReadCmd) Run(ctx context.Context) error {
	effectiveCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	r, err := search.ReadFileInArchive(effectiveCtx, c.Archive, c.Entry, c.MaxBytes, contentpkg.DefaultRegistry())
	if err != nil {
		if errors.Is(err, search.ErrArchiveEntryNotFound) {
			return &exitCodeError{code: 1, msg: fmt.Sprintf("entry %q not found in archive %q", c.Entry, c.Archive)}
		}
		return fmt.Errorf("archive-read failed: %w", err)
	}

	if c.Output == "json" {
		return printArchiveReadJSON(os.Stdout, r)
	}
	_, werr := os.Stdout.Write(r.Content)
	if werr != nil {
		return werr
	}
	if r.Truncated {
		fmt.Fprintf(os.Stderr, "(truncated at %d bytes; entry is %d bytes total)\n", len(r.Content), r.Size)
	}
	return nil
}

type FindMatchesCmd struct {
	Pattern             string        `arg:"" help:"RE2 regular expression matched line-by-line against each candidate file. Same flavour as Go's regexp/re2 and CEL's matches(). Example: '(?i)\\bTODO\\b'."`
	Dir                 []string      `short:"d" help:"Directory to search. Repeatable — pass -d a -d b to walk multiple roots." default:"."`
	Expr                string        `short:"e" name:"expr" help:"Optional CEL expression to scope candidate files BEFORE the regex scan (e.g. 'is_source && language == \"go\"'). Empty means every file (filtered to text content types)."`
	Workers             int           `short:"w" help:"Parallel workers. 0 = runtime.NumCPU()." default:"0"`
	MaxLineBytes        int           `short:"L" name:"max-line-bytes" help:"Per-line scanner cap for walk-stage attribute extraction (bytes). 0 uses the 1 MiB default." default:"0"`
	ContextBefore       int           `short:"B" name:"before" help:"Number of lines of leading context to attach to each match." default:"0"`
	ContextAfter        int           `short:"A" name:"after" help:"Number of lines of trailing context to attach to each match." default:"0"`
	Context             int           `short:"C" name:"context" help:"Shortcut: set both --before and --after to this value. Ignored when --before or --after is set explicitly." default:"0"`
	MaxMatchesPerFile   int           `name:"max-matches-per-file" help:"Cap on matches reported per file. 0 = unlimited." default:"0"`
	Exclude             []string      `name:"exclude" help:"Basename glob pruned during the walk (e.g. node_modules, .git, target). Repeatable."`
	RespectGitignore    bool          `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks      bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories. Off by default."`
	PruneArtefacts      bool          `name:"prune-build-artefacts" help:"Pre-walk and prune canonical build-artefact basenames (vendor / node_modules / target / __pycache__ / …)."`
	IndexPath           string        `name:"index-path" help:"Persistent attribute index file (bbolt). Speeds up the walk-stage content-type detection on unchanged files."`
	Timeout             time.Duration `name:"timeout" help:"Maximum duration (Go duration: 30s, 2m). On expiry, partial results are still printed and the process exits 124."`
	Output              string        `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (grep-style: path:line:text) | json."`
}

func (f *FindMatchesCmd) Run(ctx context.Context) error {
	// --context is a shortcut: only applies when --before / --after weren't set.
	before, after := f.ContextBefore, f.ContextAfter
	if f.Context > 0 {
		if before == 0 {
			before = f.Context
		}
		if after == 0 {
			after = f.Context
		}
	}

	parentCtx := ctx
	effectiveCtx := ctx
	if f.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, f.Timeout)
		defer cancel()
	}

	var idx index.Index
	if f.IndexPath != "" {
		var err error
		idx, err = openIndex(f.IndexPath, index.BodyCacheCap{})
		if err != nil {
			return err
		}
		defer func() { _ = idx.Close() }()
	}

	res, err := search.FindMatches(effectiveCtx, search.Options{
		Roots:               f.Dir,
		Expr:                f.Expr,
		Workers:             f.Workers,
		MaxLineBytes:        f.MaxLineBytes,
		Index:               idx,
		Excludes:            f.Exclude,
		RespectGitignore:    f.RespectGitignore,
		FollowSymlinks:      f.FollowSymlinks,
		PruneBuildArtefacts: f.PruneArtefacts,
		Pattern:             f.Pattern,
		ContextBefore:       before,
		ContextAfter:        after,
		MaxMatchesPerFile:   f.MaxMatchesPerFile,
	}, contentpkg.DefaultRegistry())

	// Print whatever was collected even on cancellation — FindMatches
	// returns the partial set with Cancelled=true rather than nil.
	if res != nil {
		if f.Output == "json" {
			if jerr := printFindMatchesJSON(os.Stdout, res); jerr != nil {
				return jerr
			}
		} else {
			printFindMatches(os.Stdout, res)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("find-matches failed: %w", err)
	}
	if res != nil && res.Cancelled {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "find-matches interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case f.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "find-matches timed out after %s; results above may be incomplete\n", f.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	// grep convention: exit 1 when no matches found, 0 when at least one.
	if res != nil && res.Count == 0 {
		return &exitCodeError{code: 1}
	}
	return nil
}

// ConfigPathsCmd prints the project-type config search paths for
// the current platform. Pairs with PR #101's auto-discovery — users
// can run this to find out where to drop a ~/Library/Application
// Support/file-search-on/project-types.yaml (macOS) /
// ~/.config/file-search-on/project-types.yaml (Linux) / %AppData%
// equivalent without remembering platform conventions.
type ConfigPathsCmd struct {
	Output string `short:"o" name:"output" enum:"default,bare,json" default:"default" help:"Output format: default (path + scope + existence marker), bare (one path per line — shell-friendly), or json."`
}

func (c *ConfigPathsCmd) Run(_ context.Context) error {
	entries := projecttype.DiscoveryEntries()
	switch c.Output {
	case "bare":
		for _, e := range entries {
			fpn(os.Stdout, e.Path)
		}
	case "json":
		return printConfigPathsJSON(os.Stdout, entries)
	default:
		printConfigPaths(os.Stdout, entries)
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

// WhichProjectCmd is the path-anchored counterpart to DetectProjectCmd
// and FindProjectsCmd. Given a file (or directory) path it walks up
// the directory chain and reports the nearest enclosing project root.
// Mirrors the MCP `resolve_project_for_path` tool.
type WhichProjectCmd struct {
	Path   string `arg:"" help:"File or directory to anchor on. The walk-up climbs from this path's parent (when a file) or itself (when a directory) until a project root or the filesystem root is reached."`
	Output string `short:"o" name:"output" enum:"default,json" default:"default" help:"Output format: default (human-readable) | json (same wire shape as the MCP resolve_project_for_path tool)."`
}

func (w *WhichProjectCmd) Run(_ context.Context) error {
	abs, err := filepath.Abs(w.Path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	// ResolveForPath walks from Dir(abs). When the caller hands us a
	// directory we want the walk to start AT that directory, not its
	// parent — pretend the caller asked about a sentinel file inside it.
	probe := abs
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		probe = filepath.Join(abs, ".")
	}
	root, matches := projecttype.ResolveForPath(probe, nil)
	if w.Output == "json" {
		if err := printWhichProjectJSON(os.Stdout, abs, root, matches); err != nil {
			return err
		}
	} else {
		printWhichProject(os.Stdout, abs, root, matches)
	}
	if len(matches) == 0 {
		return &exitCodeError{code: 1}
	}
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
	IndexPath         string        `name:"index-path" help:"Persistent attribute index file (bbolt). When unset the server uses an in-memory cache that lives for the process lifetime; setting this makes the cache survive restarts. The file is created on first use."`
	BodyCacheMaxBytes int           `name:"body-cache-max-bytes" default:"268435456" help:"Total size cap (bytes) for the body cache inside the bbolt index file. Default 256 MiB. FIFO eviction by access time once exceeded. Only relevant when --index-path is set; in-memory indexes have no cap."`
	NoBodyCache       bool          `name:"no-body-cache" help:"Disable the body cache. LookupBody always misses; PutBody is a no-op. Bodies are re-extracted on every include_body query."`
	Timeout           time.Duration `name:"timeout" default:"60s" help:"Default per-tool-call timeout (Go duration: 30s, 2m, 5m). Each search/read_attributes invocation is wrapped with this deadline. Per-call 'timeout_seconds' input on the search tool overrides this. Set to 0 to disable the default (not recommended — long-running calls can exceed MCP client read deadlines)."`
}

func (m *MCPCmd) Run(ctx context.Context) error {
	idx, err := openIndex(m.IndexPath, index.BodyCacheCap{MaxBytes: int64(m.BodyCacheMaxBytes), Disable: m.NoBodyCache})
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
//
// bodyCap controls the body-cache total-size cap and opt-out for the
// bodies_v1 bucket. Zero-value uses defaults (256 MiB cap, body cache
// enabled). Subcommands that don't expose body-cache flags pass the
// zero value; SearchCmd threads its --body-cache-max-bytes / --no-body-cache
// through.
func openIndex(path string, bodyCap index.BodyCacheCap) (index.Index, error) {
	if path == "" {
		return index.NewMemory(), nil
	}
	idx, err := index.OpenWith(path, bodyCap)
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
	BodyCacheMaxBytes int          `name:"body-cache-max-bytes" default:"268435456" help:"Total size cap (bytes) for the body cache inside the bbolt index file. Default 256 MiB. FIFO eviction by access time once exceeded. Only relevant when --body and --index-path are both set."`
	NoBodyCache      bool          `name:"no-body-cache" help:"Disable the body cache entirely. PutBody is a no-op and LookupBody always misses. Use when caching adds no value (one-shot search of a tree that won't be queried again) or when storage is at a premium."`
	WithHashes       bool          `name:"with-hashes" help:"Compute MD5, SHA1, and SHA256 of every matched file in a single io.MultiWriter pass and expose them as md5 / sha1 / sha256 CEL variables (and on the JSON/template output). Hashes cache in the index alongside (size, mtime), so subsequent runs are free on unchanged files. Off by default — hashing every file reads multi-GB videos / archives in full. Opt-in for forensic / NSRL / VirusTotal / threat-intel-feed workflows."`
	CheckDisguised   bool          `name:"check-disguised" help:"Run both the name-based and magic-byte detection passes on every matched file, populating magic_content_type / extension_content_type / is_disguised CEL variables. is_disguised fires when the bytes disagree with the extension — classic 'this .txt is actually a PE binary' indicator. One extra 512-byte file read per match (cached in the index)."`
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
	includeAttrs := s.Format != "" || s.Output == "verbose" || s.Output == "json" || s.Sort != "" || s.Snippet || s.WithHashes || s.CheckDisguised

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
		ComputeHashes:     s.WithHashes,
		CheckDisguised:    s.CheckDisguised,
		Excludes:            s.Exclude,
		RespectGitignore:    s.RespectGitignore,
		FollowSymlinks:      s.FollowSymlinks,
		ResolveProjects:     s.ResolveProjects,
		PruneBuildArtefacts: s.PruneArtefacts,
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
