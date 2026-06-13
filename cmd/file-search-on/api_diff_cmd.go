package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// APIDiffCmd is the exported-symbol breaking-change subcommand (issue #406).
// It builds a code graph over each tree and reports which exported
// functions/types vanished (the breaking set) or were added between them.
type APIDiffCmd struct {
	TreeA string `arg:"" name:"tree-a" help:"Baseline tree — the 'before' / released side."`
	TreeB string `arg:"" name:"tree-b" help:"Candidate tree — the 'after' / proposed side."`

	Expr string `name:"expr" help:"CEL pre-filter for which files enter each graph. Defaults to is_source. Narrow to one language for accuracy, e.g. 'is_source && language == \"go\"'."`

	Workers             int           `short:"w" help:"Parallel workers per tree walk." default:"0"`
	IndexPath           string        `name:"index-path" help:"Persistent attribute index file (bbolt). Repeat runs on an unchanged tree skip re-parsing."`
	NoIndex             bool          `name:"no-index" help:"Disable the on-disk index; in-memory cache only."`
	Timeout             time.Duration `name:"timeout" help:"Maximum duration."`
	Exclude             []string      `name:"exclude" help:"Glob matched against basenames; matches are pruned from both trees. Repeatable."`
	RespectGitignore    bool          `name:"respect-gitignore" help:"Parse a .gitignore at each tree root and skip matching paths."`
	FollowSymlinks      bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories."`
	PruneBuildArtefacts bool          `name:"prune-build-artefacts" help:"Prune canonical build-artefact dirs (vendor / node_modules / target / …) from both trees."`

	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *APIDiffCmd) Run(ctx context.Context) error {
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

	expr := defaultSourceExpr(c.Expr)
	mkOpts := func(root string) search.Options {
		return search.Options{
			Root:                root,
			Expr:                expr,
			Workers:             c.Workers,
			Index:               idx,
			Excludes:            c.Exclude,
			RespectGitignore:    c.RespectGitignore,
			FollowSymlinks:      c.FollowSymlinks,
			PruneBuildArtefacts: c.PruneBuildArtefacts,
		}
	}

	res, err := search.APIDiff(effectiveCtx, mkOpts(c.TreeA), mkOpts(c.TreeB), contentpkg.DefaultRegistry())
	if err != nil {
		if isCancellation(err) {
			return &exitCodeError{code: 124, msg: "timeout"}
		}
		return fmt.Errorf("api-diff failed: %w", err)
	}

	if c.Output == "json" {
		_ = writeJSON(os.Stdout, res)
	} else {
		printAPIDiffTable(os.Stdout, res)
	}
	return nil
}

func printAPIDiffTable(w *os.File, res *search.APIDiffResult) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, s := range res.Removed {
		_, _ = fmt.Fprintf(tw, "- removed\t%s\t%s\n", s.Kind, s.Symbol)
	}
	for _, s := range res.Added {
		_, _ = fmt.Fprintf(tw, "+ added\t%s\t%s\n", s.Kind, s.Symbol)
	}
	_ = tw.Flush()
	verdict := "no breaking changes"
	if res.Breaking {
		verdict = "BREAKING — exported symbols removed"
	}
	_, _ = fmt.Fprintf(w, "\n%s: %d removed, %d added (exported: %d → %d)\n",
		verdict, res.RemovedCount, res.AddedCount, res.ExportedA, res.ExportedB)
}
