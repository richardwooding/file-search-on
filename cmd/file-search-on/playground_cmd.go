package main

import (
	"context"
	"fmt"

	"github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/playground"
	"github.com/richardwooding/file-search-on/internal/search"
)

// PlaygroundCmd launches the interactive CEL-filtering TUI: type a CEL
// expression up top and watch a one-shot snapshot of the directory's files
// filter live as you type, over the same attribute vocabulary search uses.
// On exit it prints the final expression to stdout so a query built here is
// directly reusable with `search`.
type PlaygroundCmd struct {
	Expr             string   `arg:"" optional:"" help:"Initial CEL expression to pre-fill the input with (optional). e.g. 'is_source && max_complexity > 15'."`
	Dir              []string `short:"d" default:"." help:"Directory to search in. Repeatable — pass -d ./docs -d ./posts to snapshot multiple roots."`
	Exclude          []string `name:"exclude" help:"Glob pattern matched against the basename of each file/directory; matches are skipped. Repeatable."`
	RespectGitignore bool     `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	PruneArtefacts   bool     `name:"prune-build-artefacts" help:"Union canonical build-artefact basenames (vendor / node_modules / target / …) into --exclude before snapshotting."`
	Workers          int      `short:"w" default:"0" help:"Number of parallel workers for the initial snapshot. 0 uses NumCPU."`
	Limit            int      `name:"limit" default:"5000" help:"Cap on the number of files snapshotted for in-memory filtering. Keeps eval instant on large trees; surfaced as 'first N shown' when hit."`
	Body             bool     `name:"body" help:"Make file bodies available to the CEL expression as the 'body' string variable so body.contains(...) works. Expensive: reads every candidate file's body during the snapshot."`
	BodyMaxBytes     int      `name:"body-max-bytes" default:"0" help:"Cap on the body string read per file in bytes. 0 uses the 1 MiB default."`
}

func (p *PlaygroundCmd) Run(ctx context.Context) error {
	opts := search.Options{
		Roots:               p.Dir,
		Excludes:            p.Exclude,
		RespectGitignore:    p.RespectGitignore,
		PruneBuildArtefacts: p.PruneArtefacts,
		Workers:             p.Workers,
		IncludeBody:         p.Body,
		BodyMaxBytes:        p.BodyMaxBytes,
	}
	final, err := playground.Run(ctx, playground.RunOptions{
		Opts:     opts,
		Registry: content.DefaultRegistry(),
		Initial:  p.Expr,
		Limit:    p.Limit,
	})
	if err != nil {
		return err
	}
	// Print the final expression so a query authored in the TUI is reusable.
	if final != "" {
		fmt.Println(final)
	}
	return nil
}
