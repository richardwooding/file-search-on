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

// CouplingCmd is the coupling subcommand (issue #410, #467): afferent/
// efferent coupling + instability over first-party nodes under a project
// root. Granularity is picked by the manifest at the root — Go (go.mod) →
// packages, Rust (Cargo.toml) → crates.
type CouplingCmd struct {
	Dir  string `short:"d" default:"." help:"Project root (holds go.mod for Go packages, or Cargo.toml for Rust crates)."`
	Expr string `name:"expr" help:"CEL pre-filter for which files enter the graph. Defaults to is_source."`
	Top  int    `name:"top" default:"0" help:"Cap the packages shown (ranked most-depended-upon then most unstable). 0 = all."`

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

func (c *CouplingCmd) Run(ctx context.Context) error {
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

	res, err := search.Coupling(effectiveCtx, search.Options{
		Root:                c.Dir,
		Expr:                c.Expr,
		Workers:             c.Workers,
		Index:               idx,
		Excludes:            c.Exclude,
		RespectGitignore:    c.RespectGitignore,
		FollowSymlinks:      c.FollowSymlinks,
		PruneBuildArtefacts: c.PruneBuildArtefacts,
	}, c.Top, contentpkg.DefaultRegistry())

	if res != nil {
		if c.Output == "json" {
			_ = writeJSON(os.Stdout, res)
		} else {
			printCouplingTable(os.Stdout, res)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("coupling failed: %w", err)
	}
	if res != nil && res.Cancelled {
		fmt.Fprintln(os.Stderr, "coupling timed out; results above may be incomplete")
		return &exitCodeError{code: 124, msg: "timeout"}
	}
	return nil
}

func printCouplingTable(w *os.File, res *search.CouplingResult) {
	if res.Module == "" {
		_, _ = fmt.Fprintln(w, "no go.mod found at the root — coupling resolves first-party Go packages only")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "CA\tCE\tINSTABILITY\tPACKAGE")
	for _, p := range res.Packages {
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%.2f\t%s\n", p.Afferent, p.Efferent, p.Instability, p.Package)
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(w, "\n%d package(s) in %s. Ca=afferent (depended-upon), Ce=efferent (depends-on), I=Ce/(Ca+Ce).\n",
		len(res.Packages), res.Module)
}
