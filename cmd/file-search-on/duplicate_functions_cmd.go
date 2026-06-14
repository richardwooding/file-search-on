package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

type DuplicateFunctionsCmd struct {
	Dir              []string      `short:"d" help:"Directory to walk. Repeatable for multi-root scanning." default:"."`
	Expr             string        `arg:"" help:"Optional CEL pre-filter scoping which source files are scanned (e.g. 'language == \"go\"'). Defaults to 'is_source'." optional:""`
	Threshold        float64       `name:"threshold" default:"0" help:"Minimum SimHash similarity (0..1) for two functions to cluster. 0 (default) uses 0.92 — code SimHash sits high even for unrelated functions, so this is tighter than the prose default."`
	MinLines         int           `name:"min-lines" default:"0" help:"Skip functions shorter than this many lines. 0 uses the default 5 — filters trivial getters / one-liners that fingerprint alike."`
	Workers          int           `short:"w" help:"Parallel workers. 0 = runtime.NumCPU()." default:"0"`
	BodyMaxBytes     int           `name:"body-max-bytes" default:"0" help:"Cap on the body read per file in bytes. 0 uses the 1 MiB default."`
	IndexPath        string        `name:"index-path" help:"Persistent attribute index file (bbolt). Overrides the default per-cwd index."`
	NoIndex          bool          `name:"no-index" help:"Disable the on-disk index entirely; in-memory caching only."`
	Timeout          time.Duration `name:"timeout" help:"Maximum duration. On expiry the partial result is printed and the process exits 124."`
	Exclude          []string      `name:"exclude" help:"Glob pattern matched against file/dir basenames; matches are skipped. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks   bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories. Off by default."`
	Output           string        `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table (default) | json."`
}

func (d *DuplicateFunctionsCmd) Run(ctx context.Context) error {
	effectiveCtx := ctx
	if d.Timeout > 0 {
		var cancel context.CancelFunc
		effectiveCtx, cancel = context.WithTimeout(ctx, d.Timeout)
		defer cancel()
	}

	idx, _, err := openIndex(d.IndexPath, d.NoIndex, index.BodyCacheCap{})
	if err != nil {
		return err
	}
	defer func() { _ = idx.Close() }()

	dups, err := search.FindDuplicateFunctions(effectiveCtx, search.Options{
		Roots:               d.Dir,
		Expr:                d.Expr,
		Workers:             d.Workers,
		BodyMaxBytes:        d.BodyMaxBytes,
		Index:               idx,
		Excludes:            d.Exclude,
		RespectGitignore:    d.RespectGitignore,
		FollowSymlinks:      d.FollowSymlinks,
		SimilarityThreshold: d.Threshold,
		DupFuncMinLines:     d.MinLines,
	}, contentpkg.DefaultRegistry())

	if dups != nil {
		if d.Output == "json" {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(dups); err != nil {
				return err
			}
		} else {
			printDuplicateFunctionsTable(dups)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("duplicate-functions failed: %w", err)
	}
	if dups != nil && dups.Cancelled {
		fmt.Fprintln(os.Stderr, "duplicate-functions interrupted; results above may be incomplete")
		if dups.CancellationReason == "timeout" {
			return &exitCodeError{code: 124, msg: "timeout"}
		}
		return &exitCodeError{code: 130, msg: "interrupted"}
	}
	return nil
}

func printDuplicateFunctionsTable(d *search.DuplicateFunctions) {
	if len(d.Groups) == 0 {
		fmt.Printf("No duplicate functions found (scanned %d functions across %d files, threshold %.2f, min %d lines).\n",
			d.FunctionsScanned, d.TotalFiles, d.Threshold, d.MinLines)
		return
	}
	for i, g := range d.Groups {
		fmt.Printf("Group %d — %d functions (≈%s):\n", i+1, g.Count, g.Fingerprint)
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  SIMILARITY\tLINES\tSYMBOL\tLOCATION")
		for _, m := range g.Members {
			_, _ = fmt.Fprintf(tw, "  %.2f\t%d\t%s\t%s:%d-%d\n", m.Similarity, m.Lines, m.Symbol, m.Path, m.StartLine, m.EndLine)
		}
		_ = tw.Flush()
		fmt.Println()
	}
	fmt.Printf("%d duplicate-function group(s); %d functions scanned across %d files (threshold %.2f, min %d lines).\n",
		d.GroupCount, d.FunctionsScanned, d.TotalFiles, d.Threshold, d.MinLines)
	if d.Hint != "" {
		_, _ = fmt.Fprintln(os.Stderr, d.Hint)
	}
}
