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

// ChurnOwnersCmd is the directory-ownership / bus-factor subcommand
// (issue #407). It aggregates git authorship per directory to surface
// single-maintainer subtrees.
type ChurnOwnersCmd struct {
	Dir      []string `short:"d" help:"Directory to analyse. Repeatable for a multi-root report." default:"."`
	Expr     string   `name:"expr" help:"CEL pre-filter for which files are counted. Defaults to is_git_tracked. Narrow with e.g. is_source for code-only ownership."`
	MinFiles int      `name:"min-files" default:"1" help:"Drop directories with fewer than this many matching files."`

	Workers             int           `short:"w" help:"Parallel workers." default:"0"`
	IndexPath           string        `name:"index-path" help:"Persistent attribute index file (bbolt)."`
	NoIndex             bool          `name:"no-index" help:"Disable the on-disk index; in-memory cache only."`
	Timeout             time.Duration `name:"timeout" help:"Maximum duration."`
	Exclude             []string      `name:"exclude" help:"Glob matched against basenames; matches are pruned. Repeatable."`
	RespectGitignore    bool          `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks      bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories."`
	PruneBuildArtefacts bool          `name:"prune-build-artefacts" help:"Prune canonical build-artefact dirs (vendor / node_modules / target / …)."`

	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *ChurnOwnersCmd) Run(ctx context.Context) error {
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

	res, err := search.ChurnOwners(effectiveCtx, search.Options{
		Roots:               c.Dir,
		Expr:                c.Expr,
		Workers:             c.Workers,
		Index:               idx,
		Excludes:            c.Exclude,
		RespectGitignore:    c.RespectGitignore,
		FollowSymlinks:      c.FollowSymlinks,
		PruneBuildArtefacts: c.PruneBuildArtefacts,
	}, c.MinFiles, contentpkg.DefaultRegistry())

	if res != nil {
		if c.Output == "json" {
			_ = writeJSON(os.Stdout, res)
		} else {
			printChurnOwnersTable(os.Stdout, res)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("churn-owners failed: %w", err)
	}
	if res != nil && res.Cancelled {
		fmt.Fprintln(os.Stderr, "churn-owners timed out; results above may be incomplete")
		return &exitCodeError{code: 124, msg: "timeout"}
	}
	return nil
}

func printChurnOwnersTable(w *os.File, res *search.ChurnOwnersResult) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "AUTHORS\tFILES\tCOMMITS\tTOP AUTHOR\tSHARE\tDIR")
	for _, d := range res.Dirs {
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%d\t%s\t%.0f%%\t%s\n",
			d.DistinctAuthors, d.Files, d.TotalCommits, d.TopAuthor, d.TopAuthorShare*100, d.Dir)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(w, "\n%d director(ies), %d file(s). Single-author subtrees ranked first.\n",
		len(res.Dirs), res.TotalFiles)
}
