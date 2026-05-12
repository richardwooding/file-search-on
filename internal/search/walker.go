package search

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/richardwooding/file-search-on/internal/celexpr"
	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
)

// Result represents a matching file
type Result struct {
	Path        string
	ContentType string
	Size        int64
	// Attrs is set when Options.IncludeAttributes is true. Nil otherwise.
	// Carries the full FileAttributes that the CEL evaluator already built
	// for this file, so callers can render verbose / JSON / template output
	// without re-statting or re-parsing.
	Attrs *celexpr.FileAttributes
	// Snippet, when Options.IncludeSnippet is true and the file's content
	// type is text-based (markdown / text / html / csv / json / xml /
	// source/*), holds the first Options.SnippetLines lines of the file
	// joined by "\n". Empty for non-text content types or when snippets
	// are disabled.
	Snippet string
}

// Options configures the search
type Options struct {
	// Root is the single-directory case. Set Roots instead for
	// multi-root walks; when Roots is non-empty Root is ignored.
	Root string
	// Roots is the multi-directory case — each entry is walked in
	// turn through a per-root fs.DirFS and a per-root excluder (so
	// each root's .gitignore is honoured independently). When
	// non-empty, Root and FS are ignored. When empty, Root falls
	// through to the historical single-root path.
	Roots []string
	Expr    string
	Workers int
	// MaxLineBytes overrides the per-line scanner buffer cap honoured by the
	// text, csv, and html content types. Zero means use the package default
	// (see content.DefaultMaxLineBytes). Process-global; concurrent Walk
	// calls with different caps will race.
	MaxLineBytes int
	// IncludeAttributes, when true, populates Result.Attrs with the full
	// FileAttributes the CEL evaluator built. Off by default so the cheap
	// path-and-size case does not pay the pointer-keeping cost.
	IncludeAttributes bool
	// FS overrides the filesystem used for walking and IO. Defaults to
	// `os.DirFS(Root)` when nil. Tests inject embed.FS or fstest.MapFS for
	// hermetic execution; production almost never sets this.
	FS fs.FS
	// Index, when non-nil, is consulted by each worker to skip the
	// expensive ContentType.Attributes parse for files whose
	// (size, mtime) match a previous walk. The index handles its own
	// concurrency; workers never block on it.
	Index index.Index

	// Sort, when non-empty, sorts the buffered Walk() result set by
	// the named attribute. Recognised keys: size, name, path,
	// mod_time, word_count, line_count, page_count, duration, bitrate,
	// sample_rate, video_height, video_width, frame_rate, iso,
	// focal_length, taken_at, sent_at, year, entry_count,
	// uncompressed_size, loc, attachment_count, email_count.
	// Streaming WalkStream() ignores Sort — sort happens post-collect.
	Sort string
	// Order: "asc" (default) or "desc". Ignored when Sort is empty.
	Order string
	// Limit caps the returned match count. 0 = unlimited. With Sort
	// set, the limit is applied AFTER sorting (top-K). Without Sort,
	// the buffered Walk() truncates collected matches; the streaming
	// WalkStream() does NOT enforce Limit — callers stop early
	// themselves if they want.
	Limit int

	// IncludeSnippet, when true, makes the walker read the first
	// SnippetLines lines of each match's body and surface them via
	// Result.Snippet. Only text-based content types (markdown, text,
	// html, csv, json, xml, source/*) populate; binary families
	// (image / audio / video / archive / binary / office / epub /
	// email) leave Snippet empty.
	IncludeSnippet bool
	SnippetLines   int // default 10 when IncludeSnippet is true and this is <= 0

	// IncludeBody, when true, makes BuildAttributesWith read each
	// candidate file's body for text content types and expose it as
	// the "body" CEL variable, so filters like
	// body.contains("transformer") or body.matches("\\bAPI\\b") fire
	// at search time. Distinct from IncludeSnippet (which surfaces
	// a preview on Result for display) — body participates in the
	// filter; snippet is for the caller to see.
	IncludeBody  bool
	BodyMaxBytes int // hard cap on the body string in bytes; 0 → 1 MiB default

	// Excludes is a list of glob patterns matched against each
	// directory or file's BASENAME during walk (filepath.Match
	// semantics). Matched directories are skipped via fs.SkipDir,
	// pruning their entire subtree. Common patterns: "node_modules",
	// ".git", "*.bak", "dist". Path-component matching (e.g.
	// "src/build") is not supported here — use RespectGitignore for
	// that.
	Excludes []string

	// RespectGitignore, when true, parses a .gitignore at the walk
	// root (if present) and skips matching paths. Nested .gitignore
	// files in subdirectories are NOT honoured in this version —
	// only the root file is consulted. Patterns follow standard
	// gitignore semantics including ** and negation. In multi-root
	// mode (Roots non-empty), each root is checked independently.
	RespectGitignore bool

	// GroupBy controls the bucketing key used by ComputeStats. See
	// stats.go ValidGroupBys for the recognised set. Ignored by
	// Walk/WalkStream — it only affects ComputeStats's aggregation.
	GroupBy string

	// MinSize is a duplicate-detection threshold: files smaller
	// than this are not considered when finding duplicates. 0
	// disables the threshold (every file participates). Ignored
	// by Walk/WalkStream and ComputeStats — only FindDuplicates
	// consults it.
	MinSize int64
}

// Walk walks the directory and returns every matching file. It is a
// thin wrapper over WalkStream that drains the channel into a slice
// and applies Options.Sort / Options.Order / Options.Limit
// post-collection. Use WalkStream directly when callers want to
// process matches as they arrive (incremental output, MCP progress
// notifications, bounded memory on huge result sets); WalkStream
// does NOT honour Sort/Limit.
func Walk(ctx context.Context, opts Options, registry *content.Registry) ([]Result, error) {
	out := make(chan Result, 64)
	var results []Result
	var walkErr error
	done := make(chan struct{})
	go func() {
		walkErr = WalkStream(ctx, opts, registry, out)
		close(done)
	}()
	for r := range out {
		results = append(results, r)
	}
	<-done
	// Sort + limit live in the buffered path because top-K and
	// "ordered by attribute" semantics are incoherent with streaming.
	// The CLI's bufferedSearch and the MCP search handler both flow
	// through here (or apply the same helper on their collected
	// matches — see mcpserver.searchHandler).
	results = SortAndLimit(results, opts)
	return results, walkErr
}

// WalkStream walks the directory and sends each matching file on out.
// out is closed before WalkStream returns; consumers should range over
// it. The error return reports walker setup failures (CEL compile,
// root open) and any error fs.WalkDir surfaces (cancellation,
// permission). Per-file scan failures are silently skipped — same
// semantics as Walk.
//
// out should be buffered. An unbuffered channel works but couples
// worker throughput to consumer speed; a buffer of opts.Workers or
// larger keeps producer and consumer loosely coupled.
//
// Cancellation propagates to three sites: the producer (fs.WalkDir
// callback), each worker's receive on the jobs channel, and the
// per-file ContentType.Attributes calls inside BuildAttributes.
func WalkStream(ctx context.Context, opts Options, registry *content.Registry, out chan<- Result) error {
	defer close(out)

	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	content.SetMaxLineBytes(opts.MaxLineBytes)

	evaluator, err := celexpr.New(opts.Expr)
	if err != nil {
		return err
	}

	// Resolve which root(s) we're walking. opts.Roots takes
	// precedence; falling back to opts.Root preserves the
	// single-root (and opts.FS-override) test path.
	type rootSpec struct {
		root string
		fsys fs.FS
		exc  *excluder
	}
	var specs []rootSpec
	if len(opts.Roots) > 0 {
		// Multi-root: ignore opts.FS (it can't represent multiple
		// roots) and build a per-root os.DirFS + excluder so each
		// root's .gitignore is honoured independently.
		for _, r := range opts.Roots {
			rfs := os.DirFS(r)
			specs = append(specs, rootSpec{
				root: r,
				fsys: rfs,
				exc:  newExcluder(rfs, opts.Excludes, opts.RespectGitignore),
			})
		}
	} else {
		fsys := opts.FS
		root := opts.Root
		if fsys == nil {
			if root == "" {
				root = "."
			}
			fsys = os.DirFS(root)
		}
		specs = append(specs, rootSpec{
			root: root,
			fsys: fsys,
			exc:  newExcluder(fsys, opts.Excludes, opts.RespectGitignore),
		})
	}

	// Jobs carry their own fsys + root so workers know which
	// filesystem to read from (multi-root walks have different
	// fs.FS per match).
	type job struct {
		fsys        fs.FS
		fsPath      string
		displayPath string
	}
	jobs := make(chan job, opts.Workers*2)
	var wg sync.WaitGroup

	for range opts.Workers {
		wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case j, ok := <-jobs:
					if !ok {
						return
					}
					attrs, err := celexpr.BuildAttributesWith(ctx, j.fsys, j.fsPath, j.displayPath, registry, celexpr.BuildOptions{
						Index:        opts.Index,
						IncludeBody:  opts.IncludeBody,
						BodyMaxBytes: opts.BodyMaxBytes,
					})
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					if err != nil {
						continue
					}
					match, err := evaluator.Evaluate(attrs)
					if err != nil || !match {
						continue
					}
					r := Result{
						Path:        j.displayPath,
						ContentType: attrs.ContentType,
						Size:        attrs.Size,
					}
					if opts.IncludeAttributes {
						r.Attrs = attrs
					}
					// Snippets are only meaningful for text content
					// types — readSnippet returns ("", nil) on a
					// missing file or unscannable input, so a binary
					// match passes through with Snippet="" and the
					// caller can treat absence as "not text".
					if opts.IncludeSnippet && isTextContentType(attrs.ContentType) {
						s, _ := readSnippet(ctx, j.fsys, j.fsPath, opts.SnippetLines)
						r.Snippet = s
					}
					select {
					case <-ctx.Done():
						return
					case out <- r:
					}
				}
			}
		})
	}

	// Producer: iterate each root through fs.WalkDir, feeding the
	// shared jobs channel. Errors across roots are concatenated so
	// the caller sees them all (rather than just the first); the
	// post-loop ctx.Err() sweep covers worker-side cancellation.
	var walkErrs []error
	for _, spec := range specs {
		err := fs.WalkDir(spec.fsys, ".", func(fsPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			// Honour excludes before anything else. Matched directories
			// return fs.SkipDir so their subtree is pruned.
			if fsPath != "." && spec.exc.Match(fsPath, d.IsDir()) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			// User-facing path: OS-native join with the root the
			// match came from. Tests that pass an in-memory FS
			// without a root see fs-style paths in Result.Path.
			displayPath := fsPath
			if spec.root != "" {
				displayPath = filepath.Join(spec.root, filepath.FromSlash(fsPath))
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case jobs <- job{fsys: spec.fsys, fsPath: fsPath, displayPath: displayPath}:
			}
			return nil
		})
		if err != nil {
			walkErrs = append(walkErrs, err)
		}
		if ctx.Err() != nil {
			// Cancellation mid-walk: don't iterate remaining roots.
			break
		}
	}
	close(jobs)
	wg.Wait()
	walkErr := errors.Join(walkErrs...)

	// Workers exit on ctx.Done() without surfacing an error of their
	// own, so a fast producer + tightly-deadlined ctx can leave
	// walkErr=nil even though the walk was cancelled mid-flight:
	// fs.WalkDir finished queueing 5 small files cleanly, workers
	// drained ctx.Done() before ever processing them, and the
	// "return nil" from the WalkDir callback travelled all the way
	// back up. Surface ctx.Err() here so callers (CLI exit codes,
	// MCP partial-result flags) reliably detect that the walk was
	// cancelled rather than complete-but-empty.
	if walkErr == nil {
		if err := ctx.Err(); err != nil {
			walkErr = err
		}
	}
	return walkErr
}
