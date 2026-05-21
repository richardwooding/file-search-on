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
