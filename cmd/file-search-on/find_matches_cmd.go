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
