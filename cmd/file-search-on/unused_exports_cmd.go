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

// UnusedExportsCmd is the unexport-candidate subcommand (issue #409):
// exported Go symbols referenced only from within their own package.
type UnusedExportsCmd struct {
	Dir  string `short:"d" default:"." help:"Module root (the directory holding go.mod)."`
	Expr string `name:"expr" help:"CEL pre-filter for which files enter the graph. Defaults to is_source."`

	Workers             int           `short:"w" help:"Parallel workers." default:"0"`
	IndexPath           string        `name:"index-path" help:"Persistent attribute index file (bbolt)."`
	NoIndex             bool          `name:"no-index" help:"Disable the on-disk index; in-memory cache only."`
	Timeout             time.Duration `name:"timeout" help:"Maximum duration."`
	Exclude             []string      `name:"exclude" help:"Glob matched against basenames; matches are pruned. Repeatable."`
	RespectGitignore    bool          `name:"respect-gitignore" help:"Parse a .gitignore at the walk root and skip matching paths."`
	FollowSymlinks      bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories."`
	PruneBuildArtefacts bool          `name:"prune-build-artefacts" help:"Prune canonical build-artefact dirs (vendor / node_modules / target / …)."`

	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *UnusedExportsCmd) Run(ctx context.Context) error {
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

	res, err := search.UnusedExports(effectiveCtx, search.Options{
		Root:                c.Dir,
		Expr:                c.Expr,
		Workers:             c.Workers,
		Index:               idx,
		Excludes:            c.Exclude,
		RespectGitignore:    c.RespectGitignore,
		FollowSymlinks:      c.FollowSymlinks,
		PruneBuildArtefacts: c.PruneBuildArtefacts,
	}, contentpkg.DefaultRegistry())

	if res != nil {
		if c.Output == "json" {
			_ = writeJSON(os.Stdout, res)
		} else {
			printUnusedExportsTable(os.Stdout, res)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("unused-exports failed: %w", err)
	}
	if res != nil && res.Cancelled {
		fmt.Fprintln(os.Stderr, "unused-exports timed out; results above may be incomplete")
		return &exitCodeError{code: 124, msg: "timeout"}
	}
	return nil
}

func printUnusedExportsTable(w *os.File, res *search.UnusedExportsResult) {
	if res.Module == "" {
		_, _ = fmt.Fprintln(w, "no go.mod found at the root — unused-exports resolves first-party Go packages only")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "KIND\tSYMBOL\tPATH")
	for _, c := range res.Candidates {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", c.Kind, c.Symbol, c.Path)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(w, "\n%d unexport candidate(s) in %s — referenced only intra-package. HEURISTIC; review before unexporting.\n",
		len(res.Candidates), res.Module)
}
