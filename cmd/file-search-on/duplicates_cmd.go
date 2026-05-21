package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

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
