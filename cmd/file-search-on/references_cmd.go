package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// ReferencesCmd is the find-all-usages subcommand (issue #408): every site
// that references a symbol — calls, type usages, value passing — with line.
type ReferencesCmd struct {
	Symbol string `arg:"" help:"Exact function or type name to find all usages of."`
	Kind   string `name:"kind" enum:",call,type,value" default:"" help:"Filter usage kind: call | type | value. Empty returns all."`
	Expr   string `name:"expr" help:"CEL pre-filter for which files enter the graph. Defaults to is_source."`

	Dir                 []string      `short:"d" help:"Directory to analyse. Repeatable for a multi-root graph." default:"."`
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

func (c *ReferencesCmd) Run(ctx context.Context) error {
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

	res, err := search.References(effectiveCtx, search.Options{
		Roots:               c.Dir,
		Expr:                defaultSourceExpr(c.Expr),
		Workers:             c.Workers,
		Index:               idx,
		Excludes:            c.Exclude,
		RespectGitignore:    c.RespectGitignore,
		FollowSymlinks:      c.FollowSymlinks,
		PruneBuildArtefacts: c.PruneBuildArtefacts,
	}, c.Symbol, c.Kind, contentpkg.DefaultRegistry())

	if res != nil {
		if c.Output == "json" {
			_ = writeJSON(os.Stdout, res)
		} else {
			printReferencesTable(os.Stdout, res)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("references failed: %w", err)
	}
	if res != nil && res.Cancelled {
		fmt.Fprintln(os.Stderr, "references timed out; results above may be incomplete")
		return &exitCodeError{code: 124, msg: "timeout"}
	}
	return nil
}

func printReferencesTable(w *os.File, res *search.ReferencesResult) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, r := range res.References {
		_, _ = fmt.Fprintf(tw, "%s:%d\t%s\n", r.Path, r.Line, r.Kind)
	}
	_ = tw.Flush()
	if len(res.DefinedOn) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "%q is a method on: %s\n", res.Symbol, strings.Join(res.DefinedOn, ", "))
	}
	_, _ = fmt.Fprintf(os.Stderr, "%d usage(s) of %q (of %d source files)\n", res.Count, res.Symbol, res.TotalFiles)
}
