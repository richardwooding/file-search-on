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
