package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// CircularCmd is the circular-dependency subcommand (issue #481): strongly-
// connected components (size > 1) in the first-party import graph. Language /
// granularity selection mirrors `coupling` — the manifest at the root picks
// the adapter (go.mod → Go packages, Cargo.toml → Rust crates, JVM / C# /
// Python / JS-TS / PHP), so cycle detection is multi-language.
type CircularCmd struct {
	Dir  string `short:"d" default:"." help:"Project root (same manifest-based language selection as 'coupling': go.mod, Cargo.toml, Maven/Gradle/sbt, .sln/.csproj, pyproject.toml/setup.py, package.json/tsconfig.json, composer.json)."`
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

func (c *CircularCmd) Run(ctx context.Context) error {
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

	res, err := search.Cycles(effectiveCtx, search.Options{
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
			printCyclesTable(os.Stdout, res)
		}
	}

	if err != nil && !isCancellation(err) {
		return fmt.Errorf("circular failed: %w", err)
	}
	if res != nil && res.Cancelled {
		fmt.Fprintln(os.Stderr, "circular timed out; results above may be incomplete")
		return &exitCodeError{code: 124, msg: "timeout"}
	}
	return nil
}

func printCyclesTable(w *os.File, res *search.CyclesResult) {
	if res.Module == "" {
		_, _ = fmt.Fprintln(w, "no recognised build manifest at the root — circular resolves first-party nodes only (see 'coupling' for the manifest list)")
		return
	}
	if len(res.Cycles) == 0 {
		_, _ = fmt.Fprintf(w, "no circular dependencies in %s\n", res.Module)
		return
	}
	for _, cy := range res.Cycles {
		// Render the cycle as a loop: a → b → c → a.
		_, _ = fmt.Fprintf(w, "[%d] %s → %s\n", cy.Length, strings.Join(cy.Nodes, " → "), cy.Nodes[0])
	}
	_, _ = fmt.Fprintf(w, "\n%d circular dependenc%s in %s.\n",
		res.Count, plural(res.Count, "y", "ies"), res.Module)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
