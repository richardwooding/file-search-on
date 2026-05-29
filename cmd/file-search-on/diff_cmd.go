package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// DiffCmd is the cross-tree set-difference subcommand (issue #210).
// Read-only: it walks two trees, hashes every file, and reports the
// requested set operation by sha256 (or by relative path for mismatch).
type DiffCmd struct {
	TreeA string `arg:"" name:"tree-a" help:"First tree (the 'A' side)."`
	TreeB string `arg:"" name:"tree-b" help:"Second tree (the 'B' side)."`

	Op   string `name:"op" enum:"a-minus-b,b-minus-a,intersect,union,mismatch" default:"a-minus-b" help:"Set operation by sha256 content hash: a-minus-b (in A not B) | b-minus-a (in B not A) | intersect (in both) | union (all distinct) | mismatch (same relative path, different content)."`
	Expr string `name:"expr" help:"Optional CEL expression to scope which files are considered in both trees (e.g. 'size > 1000000' or 'is_image'). Defaults to every file."`

	Workers          int           `short:"w" help:"Parallel workers per tree walk." default:"0"`
	IndexPath        string        `name:"index-path" help:"Persistent attribute index (bbolt). Overrides the default per-cwd index at <UserCacheDir>/file-search-on/indexes/. Caches sha256 hashes alongside other attributes; two warm trees diff in seconds."`
	NoIndex          bool          `name:"no-index" help:"Disable the on-disk index entirely; use only in-memory caching for the process lifetime."`
	Timeout          time.Duration `name:"timeout" help:"Maximum duration. On expiry, the partial result is still printed and the process exits 124."`
	Exclude          []string      `name:"exclude" help:"Glob pattern matched against file/dir basenames; matches are pruned from both trees. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at each tree root and skip matching paths."`
	FollowSymlinks   bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories. Off by default."`
	MinSize          int64         `name:"min-size" default:"0" help:"Skip files smaller than this many bytes in both trees."`

	Output string `short:"o" name:"output" enum:"json,table" default:"json" help:"Output format: json (NDJSON, one record per line) | table (human-readable)."`
}

func (c *DiffCmd) Run(ctx context.Context) error {
	parentCtx := ctx
	effectiveCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	idx, _, err := openIndex(c.IndexPath, c.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	res, err := search.DiffTrees(effectiveCtx, c.TreeA, c.TreeB, search.DiffOp(c.Op), search.Options{
		Expr:             c.Expr,
		Workers:          c.Workers,
		Index:            idx,
		Excludes:         c.Exclude,
		RespectGitignore: c.RespectGitignore,
		FollowSymlinks:   c.FollowSymlinks,
		MinSize:          c.MinSize,
	}, contentpkg.DefaultRegistry())

	if res != nil {
		if c.Output == "table" {
			printDiffTable(os.Stdout, res)
		} else {
			if perr := printDiffNDJSON(os.Stdout, res); perr != nil {
				return perr
			}
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("diff failed: %w", err)
	}
	if res != nil && res.Cancelled {
		switch {
		case errors.Is(parentCtx.Err(), context.Canceled):
			fmt.Fprintln(os.Stderr, "diff interrupted; results above may be incomplete")
			return &exitCodeError{code: 130, msg: "interrupted"}
		case c.Timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
			fmt.Fprintf(os.Stderr, "diff timed out after %s; results above may be incomplete\n", c.Timeout)
			return &exitCodeError{code: 124, msg: "timeout"}
		}
	}
	return nil
}

func printDiffNDJSON(w *os.File, res *search.DiffResult) error {
	enc := json.NewEncoder(w)
	for i := range res.Records {
		if err := enc.Encode(res.Records[i]); err != nil {
			return err
		}
	}
	return nil
}

func printDiffTable(w *os.File, res *search.DiffResult) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "STATUS\tPATH A\tPATH B\tSHA256")
	for _, r := range res.Records {
		sha := r.SHA256
		if len(sha) > 12 {
			sha = sha[:12]
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Status, r.PathA, r.PathB, sha)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(w, "\n%d record(s) for op=%s (%d file(s) in A, %d in B)\n",
		res.Count, res.Op, res.TotalA, res.TotalB)
}
