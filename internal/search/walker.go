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
	"github.com/richardwooding/file-search-on/internal/embed"
	"github.com/richardwooding/file-search-on/internal/hashset"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/projecttype"
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
	// Rank is the float64 score from Options.RankExpr (a CEL expression
	// returning double / int / bool — see celexpr.RankEvaluator). Zero
	// when RankExpr is empty OR when the per-file evaluation errored
	// (we keep the file in results rather than dropping it). Issue #168.
	Rank float64
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
	// mod_time, created_at, metadata_changed_at, word_count,
	// line_count, page_count, duration, bitrate, sample_rate,
	// video_height, video_width, frame_rate, iso, focal_length,
	// taken_at, sent_at, year, entry_count, uncompressed_size, loc,
	// attachment_count, email_count, virtual_size, image_count,
	// disk_image_created_at, cluster_bits. Streaming WalkStream()
	// ignores Sort — sort happens post-collect.
	Sort string
	// Order: "asc" (default) or "desc". Ignored when Sort is empty.
	Order string
	// RankExpr is a CEL expression that returns a double / int / bool
	// score per file (issue #168). When non-empty the walker compiles
	// it alongside Expr and evaluates per file; the result lands on
	// Result.Rank. When set, Walk() defaults Sort to "rank" and Order
	// to "desc" so the more expressive primitive wins gracefully —
	// `--rank 'similarity * 0.7 + recency_bonus'` Just Works without
	// also passing --sort rank --order desc. Composes with semantic
	// search because the `similarity` CEL variable is already declared.
	RankExpr string
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

	// ComputeHashes, when true, populates MD5 / SHA1 / SHA256 on
	// each Result by reading the file fully (one io.MultiWriter
	// pass across the three hashers). Cached in the index alongside
	// other Entry fields, keyed on (size, mtime). Expensive: every
	// matched file is read end-to-end. Opt-in for forensic / hash-
	// based interop workflows (NSRL, VirusTotal, threat-intel feeds);
	// CLI exposes via `--with-hashes`, MCP via `compute_hashes`.
	ComputeHashes bool

	// CheckDisguised, when true, populates magic_content_type /
	// extension_content_type / is_disguised on each Result by
	// running both Registry.Detect tiers (name-based and
	// magic-byte) independently. One extra 512-byte file read per
	// file whose extension already won — cheap relative to
	// ComputeHashes but not free. Cached in index.Entry. Opt-in
	// for forensic triage; CLI exposes via `--check-disguised`,
	// MCP via `check_disguised`.
	CheckDisguised bool

	// ReadExtendedAttributes, when true, populates the xattr family
	// of CEL attributes on each matched file via content.ReadXattrs.
	// Darwin-only — non-Darwin builds always surface empty xattr
	// attrs regardless of this flag. Two syscalls per file
	// (Listxattr + Getxattr); off by default. CLI exposes via
	// `--with-xattrs`, MCP via `with_xattrs`. Issue #193.
	ReadExtendedAttributes bool

	// Allowlist / Denylist are hash-allowlist / hash-denylist
	// query layers (PR #146). When non-nil AND ComputeHashes is
	// true, BuildAttributesWith populates is_known_good /
	// is_known_bad on each match by looking up MD5 / SHA1 /
	// SHA256 in the respective Set. NSRL / VirusTotal /
	// threat-intel-feed interop. CLI exposes via
	// `--hash-allowlist` / `--hash-denylist`, MCP via
	// `hash_allowlist_path` / `hash_denylist_path`. Callers
	// should set ComputeHashes alongside (the CLI / MCP entry
	// points force it when the lists are set).
	Allowlist hashset.Set
	Denylist  hashset.Set

	// Embedder + SemanticQueryEmbedding power the `similarity`
	// CEL variable (issue #151). When both are set, the walker
	// passes them down to BuildAttributesWith which embeds each
	// candidate file's body (cache-aware via index.Entry.Vector)
	// and stores the cosine against SemanticQueryEmbedding in
	// FileAttributes.Similarity. CLI threads these from
	// --semantic-query / --embedding-model / --embedding-server;
	// MCP threads them from the search_semantic tool's inputs +
	// server-startup defaults.
	Embedder               embed.Embedder
	SemanticQueryEmbedding []float32

	// ResolveProjects, when true, makes BuildAttributesWith populate
	// each match's `project_types` (list<string>) and `project_type`
	// (string — first match) CEL variables by walking up from the
	// file's directory to the nearest project-root indicator. Opt-in
	// because resolution does extra I/O (one ReadDir per unique dir
	// walked, cached). Without this flag both variables stay at their
	// zero values and CEL expressions referencing them just see
	// "no match".
	ResolveProjects bool

	// PruneBuildArtefacts, when true, pre-walks each root to
	// discover every project subdirectory and unions the canonical
	// build-artefact basenames (`vendor`, `node_modules`, `target`,
	// `__pycache__`, `.venv`, `target`, `bin`, `obj`, `.terraform`,
	// …) from every detected project type into the basename
	// excluder. Saves users from passing `--exclude node_modules
	// --exclude vendor --exclude target …` manually when walking
	// monorepos or `~/Code`. Opt-in because the pre-walk adds I/O
	// proportional to the directory tree's size.
	PruneBuildArtefacts bool

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

	// FollowSymlinks, when true, descends through symbolic links to
	// directories during the walk. The default (false) preserves
	// Go's fs.WalkDir behaviour — symlinks-to-dirs surface as leaf
	// entries with is_symlink=true and are NOT recursed into.
	// Independent of the is_symlink / target_path / is_broken_symlink
	// CEL attributes, which are populated regardless of this flag.
	//
	// No symlink-loop detection — when set true on a tree with a
	// cycle, the walk relies on Go's WalkDir to surface ELOOP from
	// the OS. Best avoided unless you know the tree is acyclic.
	FollowSymlinks bool

	// SimilarityThreshold is consumed by FindNearDuplicates: the
	// minimum SimHash similarity (0..1) at which two files are
	// considered near-duplicates. 0.85 by default (≈ 9 bits Hamming
	// distance on a 64-bit fingerprint). Ignored by the regular
	// Walk path.
	SimilarityThreshold float64

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

	// Pattern is the RE2 regex FindMatches scans each candidate
	// file for, line-by-line. Empty means FindMatches returns
	// ErrEmptyPattern. Ignored by every other entry point.
	Pattern string
	// ContextBefore is the number of lines of leading context to
	// attach to each FindMatches hit. 0 means no Before window.
	// Ignored outside FindMatches.
	ContextBefore int
	// ContextAfter is the number of lines of trailing context to
	// attach to each FindMatches hit. 0 means no After window.
	// Ignored outside FindMatches.
	ContextAfter int
	// MaxMatchesPerFile caps FindMatches hits per file. 0 = no
	// cap. The scan keeps reading past the cap until every pending
	// After window is filled, so the last few matches still carry
	// the requested trailing context.
	MaxMatchesPerFile int

	// SkipAttributesParse, when true, makes BuildAttributesWith detect
	// the file's content type and run setTypeFlags but skip the
	// expensive ContentType.Attributes() parse. The walker still
	// emits a Result with Path/ContentType/Size populated and the
	// per-type / family bools on attrs set, but Extra is empty.
	//
	// Used by ComputeStats when the GroupBy key is detector-only
	// (content_type / ext / dir / mtime_*) AND the CEL expression
	// doesn't need attribute fields. Don't set this directly from
	// search.Walk callers — the search and find_matches tools
	// always need the parse.
	SkipAttributesParse bool
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
	//
	// RankExpr graceful default: when set, override Sort to "rank"
	// and Order to "desc" (the more expressive primitive wins; users
	// can pass --order asc to flip). Composes with --sort: rank still
	// wins because it's evaluated per file and isn't a fixed attribute.
	if opts.RankExpr != "" {
		opts.Sort = "rank"
		if opts.Order == "" {
			opts.Order = "desc"
		}
	}
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

	// Compile the optional rank expression against the same env.
	// nil rankEvaluator → no per-file rank evaluation in the worker.
	var rankEvaluator *celexpr.RankEvaluator
	if opts.RankExpr != "" {
		rankEvaluator, err = evaluator.NewRank(opts.RankExpr)
		if err != nil {
			return err
		}
	}

	// Resolve which root(s) we're walking. opts.Roots takes
	// precedence; falling back to opts.Root preserves the
	// single-root (and opts.FS-override) test path.
	type rootSpec struct {
		root     string
		fsys     fs.FS
		exc      *excluder
		resolver *projecttype.ProjectResolver
	}
	var specs []rootSpec
	makeResolver := func(r string) *projecttype.ProjectResolver {
		if !opts.ResolveProjects {
			return nil
		}
		return projecttype.NewResolver(r, nil)
	}
	// excludesFor returns the user's --exclude list plus any
	// project-aware build-artefact excludes collected from r's
	// subtree. When PruneBuildArtefacts is off this is a no-op
	// returning opts.Excludes as-is. Errors from the pre-walk are
	// swallowed: a broken filesystem during pre-walk shouldn't
	// hard-fail the search; we just skip the auto-prune.
	excludesFor := func(r string) []string {
		if !opts.PruneBuildArtefacts {
			return opts.Excludes
		}
		extra, err := projecttype.CollectBuildExcludes(ctx, r)
		if err != nil || len(extra) == 0 {
			return opts.Excludes
		}
		merged := make([]string, 0, len(opts.Excludes)+len(extra))
		merged = append(merged, opts.Excludes...)
		merged = append(merged, extra...)
		return merged
	}
	if len(opts.Roots) > 0 {
		// Multi-root: ignore opts.FS (it can't represent multiple
		// roots) and build a per-root os.DirFS + excluder so each
		// root's .gitignore is honoured independently.
		for _, r := range opts.Roots {
			rfs := os.DirFS(r)
			specs = append(specs, rootSpec{
				root:     r,
				fsys:     rfs,
				exc:      newExcluder(rfs, excludesFor(r), opts.RespectGitignore),
				resolver: makeResolver(r),
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
			root:     root,
			fsys:     fsys,
			exc:      newExcluder(fsys, excludesFor(root), opts.RespectGitignore),
			resolver: makeResolver(root),
		})
	}

	// Jobs carry their own fsys + root so workers know which
	// filesystem to read from (multi-root walks have different
	// fs.FS per match). Resolver is per-root for the same reason —
	// projects don't span roots.
	type job struct {
		fsys        fs.FS
		fsPath      string
		displayPath string
		resolver    *projecttype.ProjectResolver
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
						Index:                  opts.Index,
						IncludeBody:            opts.IncludeBody,
						BodyMaxBytes:           opts.BodyMaxBytes,
						ProjectResolver:        j.resolver,
						SkipAttributesParse:    opts.SkipAttributesParse,
						ComputeHashes:          opts.ComputeHashes,
						CheckDisguised:         opts.CheckDisguised,
						ReadExtendedAttributes: opts.ReadExtendedAttributes,
						Allowlist:              opts.Allowlist,
						Denylist:               opts.Denylist,
						Embedder:               opts.Embedder,
						SemanticQueryEmbedding: opts.SemanticQueryEmbedding,
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
					// Rank evaluation — per file, after the filter
					// passed. Errors zero the rank rather than dropping
					// the file (partial data beats missing matches).
					if rankEvaluator != nil {
						if v, err := rankEvaluator.Eval(attrs); err == nil {
							r.Rank = v
						}
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

	// walkSymlinkDir is the FollowSymlinks=true descent. When entry
	// at osPath is a symlink-to-dir, walks the resolved target and
	// queues each file under the original symlink-anchored path.
	// handled=true means the caller should skip its normal queue
	// step. Closure (not package-level fn) because jobs / rootSpec
	// types are declared inline inside WalkStream. Loop detection
	// is deliberately not done — per issue #128 we rely on the OS
	// to surface ELOOP through Go's WalkDir.
	walkSymlinkDir := func(spec rootSpec, fsPath, osPath string) (handled bool, err error) {
		lstatInfo, lerr := os.Lstat(osPath)
		if lerr != nil || lstatInfo.Mode()&os.ModeSymlink == 0 {
			return false, nil
		}
		statInfo, terr := os.Stat(osPath)
		if terr != nil || !statInfo.IsDir() {
			return false, nil
		}
		target, eerr := filepath.EvalSymlinks(osPath)
		if eerr != nil || target == "" {
			return false, nil
		}
		subFsys := os.DirFS(target)
		werr := fs.WalkDir(subFsys, ".", func(subPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if subPath == "." {
				return nil
			}
			virtualFSPath := filepath.ToSlash(filepath.Join(filepath.FromSlash(fsPath), filepath.FromSlash(subPath)))
			if spec.exc.Match(virtualFSPath, d.IsDir()) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				return nil
			}
			displayPath := filepath.Join(spec.root, filepath.FromSlash(fsPath), filepath.FromSlash(subPath))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case jobs <- job{fsys: subFsys, fsPath: subPath, displayPath: displayPath, resolver: spec.resolver}:
			}
			return nil
		})
		return true, werr
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

			// FollowSymlinks: when the entry is a symlink-to-dir,
			// recurse into the resolved target instead of queueing
			// the symlink as a single leaf. Path rewriting keeps
			// search results anchored under the original symlink so
			// users see the "user-facing" location rather than the
			// resolved target. Symlinks-to-files fall through to the
			// regular queue path — fs.Stat already follows them.
			if opts.FollowSymlinks && spec.root != "" {
				osPath := filepath.Join(spec.root, filepath.FromSlash(fsPath))
				if walked, werr := walkSymlinkDir(spec, fsPath, osPath); walked {
					return werr
				}
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
			case jobs <- job{fsys: spec.fsys, fsPath: fsPath, displayPath: displayPath, resolver: spec.resolver}:
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

