package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	contentpkg "github.com/richardwooding/file-search-on/internal/content"
	"github.com/richardwooding/file-search-on/internal/index"
	"github.com/richardwooding/file-search-on/internal/search"
)

// codeGraphWalkFlags is the shared walk-scoping flag set for the three
// cross-file code-graph subcommands.
type codeGraphWalkFlags struct {
	Dir              []string      `short:"d" help:"Directory to analyse. Repeatable for a multi-root graph." default:"."`
	Workers          int           `short:"w" help:"Parallel workers." default:"0"`
	IndexPath        string        `name:"index-path" help:"Persistent attribute index file (bbolt). Repeat runs on an unchanged tree skip re-parsing."`
	NoIndex          bool          `name:"no-index" help:"Disable the on-disk index; in-memory cache only."`
	Timeout          time.Duration `name:"timeout" help:"Maximum duration. On expiry the partial graph is still printed and the process exits 124."`
	Exclude          []string      `name:"exclude" help:"Glob matched against basenames; matches are pruned. Repeatable."`
	RespectGitignore bool          `name:"respect-gitignore" help:"Parse a .gitignore at each walk root and skip matching paths."`
	FollowSymlinks   bool          `name:"follow-symlinks" help:"Descend through symbolic links to directories."`
}

func (f *codeGraphWalkFlags) build(ctx context.Context, expr string) (*search.CodeGraph, context.Context, context.Context, func(), error) {
	parentCtx := ctx
	effectiveCtx := ctx
	cancel := func() {}
	if f.Timeout > 0 {
		effectiveCtx, cancel = context.WithTimeout(ctx, f.Timeout)
	}
	idx, _, err := openIndex(f.IndexPath, f.NoIndex, index.BodyCacheCap{})
	if err != nil {
		cancel()
		return nil, parentCtx, effectiveCtx, func() {}, err
	}
	g, gerr := search.BuildCodeGraph(effectiveCtx, search.Options{
		Roots:            f.Dir,
		Expr:             expr,
		Workers:          f.Workers,
		Index:            idx,
		Excludes:         f.Exclude,
		RespectGitignore: f.RespectGitignore,
		FollowSymlinks:   f.FollowSymlinks,
	}, contentpkg.DefaultRegistry())
	cleanup := func() { _ = idx.Close(); cancel() }
	if gerr != nil && !isCancellation(gerr) {
		cleanup()
		return nil, parentCtx, effectiveCtx, func() {}, fmt.Errorf("code graph failed: %w", gerr)
	}
	return g, parentCtx, effectiveCtx, cleanup, nil
}

func defaultSourceExpr(expr string) string {
	if expr == "" {
		return "is_source"
	}
	return expr
}

// ImportedByCmd — reverse-dependency lookup.
type ImportedByCmd struct {
	Module string `arg:"" help:"Import string to look up (e.g. 'github.com/spf13/cobra', 'numpy', 'react')."`
	codeGraphWalkFlags
	Mode   string `name:"mode" enum:"exact,prefix,regex" default:"exact" help:"Match mode: exact | prefix | regex (RE2)."`
	Expr   string `name:"expr" help:"CEL pre-filter for which files enter the graph. Defaults to is_source."`
	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *ImportedByCmd) Run(ctx context.Context) error {
	g, parentCtx, effectiveCtx, cleanup, err := c.build(ctx, defaultSourceExpr(c.Expr))
	if err != nil {
		return err
	}
	defer cleanup()
	if g == nil {
		return nil
	}
	importers, err := g.ImportedBy(c.Module, c.Mode)
	if err != nil {
		return fmt.Errorf("imported-by: %w", err)
	}
	if c.Output == "json" {
		_ = writeJSON(os.Stdout, map[string]any{
			"module": c.Module, "mode": c.Mode, "importers": importers,
			"count": len(importers), "total_files": g.TotalFiles,
		})
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, im := range importers {
			_, _ = fmt.Fprintf(tw, "%s\t%s\n", im.Language, im.Path)
		}
		_ = tw.Flush()
		_, _ = fmt.Fprintf(os.Stderr, "%d file(s) import %q (of %d source files)\n", len(importers), c.Module, g.TotalFiles)
	}
	return codeGraphExit(c.Timeout, parentCtx, effectiveCtx, g, "imported-by")
}

// FindDefinitionCmd — symbol definition lookup.
type FindDefinitionCmd struct {
	Symbol string `arg:"" help:"Exact function or type name to locate."`
	codeGraphWalkFlags
	Kind   string `name:"kind" enum:",function,type" default:"" help:"Filter to a symbol class: function | type. Empty returns both."`
	Expr   string `name:"expr" help:"CEL pre-filter. Defaults to is_source."`
	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *FindDefinitionCmd) Run(ctx context.Context) error {
	g, parentCtx, effectiveCtx, cleanup, err := c.build(ctx, defaultSourceExpr(c.Expr))
	if err != nil {
		return err
	}
	defer cleanup()
	if g == nil {
		return nil
	}
	defs := g.FindDefinition(c.Symbol, c.Kind)
	if c.Output == "json" {
		_ = writeJSON(os.Stdout, map[string]any{
			"symbol": c.Symbol, "kind": c.Kind, "definitions": defs,
			"count": len(defs), "total_files": g.TotalFiles,
		})
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, d := range defs {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", d.Kind, d.Language, d.Path)
		}
		_ = tw.Flush()
		_, _ = fmt.Fprintf(os.Stderr, "%d definition(s) of %q (of %d source files)\n", len(defs), c.Symbol, g.TotalFiles)
	}
	return codeGraphExit(c.Timeout, parentCtx, effectiveCtx, g, "find-definition")
}

// CodeGraphCmd — project-wide overview.
type CodeGraphCmd struct {
	Expr string `arg:"" optional:"" help:"CEL pre-filter for which files enter the graph. Defaults to is_source."`
	codeGraphWalkFlags
	Top    int    `name:"top" default:"20" help:"Cap each ranked list (import hubs, high fan-out, duplicate defs)."`
	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *CodeGraphCmd) Run(ctx context.Context) error {
	g, parentCtx, effectiveCtx, cleanup, err := c.build(ctx, defaultSourceExpr(c.Expr))
	if err != nil {
		return err
	}
	defer cleanup()
	if g == nil {
		return nil
	}
	ov := g.Overview(c.Top)
	if c.Output == "json" {
		_ = writeJSON(os.Stdout, ov)
	} else {
		fmt.Printf("Files: %d   Modules: %d   Symbols: %d\n", ov.TotalFiles, ov.DistinctModules, ov.DistinctSymbols)
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "\nLANGUAGE\tFILES")
		for _, l := range ov.Languages {
			_, _ = fmt.Fprintf(tw, "%s\t%d\n", l.Language, l.Files)
		}
		_, _ = fmt.Fprintln(tw, "\nIMPORT HUB\tFAN-IN")
		for _, hub := range ov.ImportHubs {
			_, _ = fmt.Fprintf(tw, "%s\t%d\n", hub.Module, hub.Count)
		}
		_, _ = fmt.Fprintln(tw, "\nHIGH FAN-OUT FILE\tIMPORTS")
		for _, f := range ov.HighFanOut {
			_, _ = fmt.Fprintf(tw, "%s\t%d\n", f.Path, f.Imports)
		}
		if len(ov.DuplicateDefs) > 0 {
			_, _ = fmt.Fprintln(tw, "\nDUPLICATE DEFINITION\tKIND\tFILES")
			for _, d := range ov.DuplicateDefs {
				_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\n", d.Symbol, d.Kind, len(d.Paths))
			}
		}
		_ = tw.Flush()
	}
	return codeGraphExit(c.Timeout, parentCtx, effectiveCtx, g, "code-graph")
}

// codeGraphExit maps a partial (cancelled) graph to the conventional
// exit codes used by the other walk subcommands.
func codeGraphExit(timeout time.Duration, parentCtx, effectiveCtx context.Context, g *search.CodeGraph, name string) error {
	if g == nil || !g.Cancelled {
		return nil
	}
	switch {
	case errors.Is(parentCtx.Err(), context.Canceled):
		_, _ = fmt.Fprintf(os.Stderr, "%s interrupted; results above may be incomplete\n", name)
		return &exitCodeError{code: 130, msg: "interrupted"}
	case timeout > 0 && errors.Is(effectiveCtx.Err(), context.DeadlineExceeded):
		_, _ = fmt.Fprintf(os.Stderr, "%s timed out after %s; results above may be incomplete\n", name, timeout)
		return &exitCodeError{code: 124, msg: "timeout"}
	}
	return nil
}

func writeJSON(w *os.File, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// WhoCallsCmd — reverse call lookup.
type WhoCallsCmd struct {
	Symbol string `arg:"" help:"Exact function/method name to find callers of."`
	codeGraphWalkFlags
	Expr   string `name:"expr" help:"CEL pre-filter. Defaults to is_source."`
	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *WhoCallsCmd) Run(ctx context.Context) error {
	g, parentCtx, effectiveCtx, cleanup, err := c.build(ctx, defaultSourceExpr(c.Expr))
	if err != nil {
		return err
	}
	defer cleanup()
	if g == nil {
		return nil
	}
	callers := g.WhoCalls(c.Symbol)
	if c.Output == "json" {
		_ = writeJSON(os.Stdout, map[string]any{
			"symbol": c.Symbol, "callers": callers, "count": len(callers), "total_files": g.TotalFiles,
		})
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, im := range callers {
			_, _ = fmt.Fprintf(tw, "%s\t%s\n", im.Language, im.Path)
		}
		_ = tw.Flush()
		_, _ = fmt.Fprintf(os.Stderr, "%d file(s) call %q (of %d source files)\n", len(callers), c.Symbol, g.TotalFiles)
	}
	return codeGraphExit(c.Timeout, parentCtx, effectiveCtx, g, "who-calls")
}

// DeadCodeCmd — candidate unreferenced definitions.
type DeadCodeCmd struct {
	Expr string `arg:"" optional:"" help:"CEL pre-filter for which files enter the graph. Defaults to is_source."`
	codeGraphWalkFlags
	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *DeadCodeCmd) Run(ctx context.Context) error {
	g, parentCtx, effectiveCtx, cleanup, err := c.build(ctx, defaultSourceExpr(c.Expr))
	if err != nil {
		return err
	}
	defer cleanup()
	if g == nil {
		return nil
	}
	candidates := g.DeadCode()
	if c.Output == "json" {
		_ = writeJSON(os.Stdout, map[string]any{
			"candidates": candidates, "count": len(candidates), "total_files": g.TotalFiles,
		})
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		for _, d := range candidates {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", d.Kind, d.Symbol, d.Path)
		}
		_ = tw.Flush()
		_, _ = fmt.Fprintf(os.Stderr, "%d candidate(s) — heuristic; exported API / entry points / dynamic dispatch may be false positives\n", len(candidates))
	}
	return codeGraphExit(c.Timeout, parentCtx, effectiveCtx, g, "dead-code")
}

// CallsCmd — forward call lookup (what does a function call).
type CallsCmd struct {
	Symbol string `arg:"" help:"Exact function/method name whose callees to list."`
	codeGraphWalkFlags
	Expr   string `name:"expr" help:"CEL pre-filter. Defaults to is_source."`
	Output string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *CallsCmd) Run(ctx context.Context) error {
	g, parentCtx, effectiveCtx, cleanup, err := c.build(ctx, defaultSourceExpr(c.Expr))
	if err != nil {
		return err
	}
	defer cleanup()
	if g == nil {
		return nil
	}
	callees := g.Calls(c.Symbol)
	if c.Output == "json" {
		_ = writeJSON(os.Stdout, map[string]any{
			"symbol": c.Symbol, "callees": callees, "count": len(callees), "total_files": g.TotalFiles,
		})
	} else {
		for _, callee := range callees {
			fmt.Println(callee)
		}
		_, _ = fmt.Fprintf(os.Stderr, "%q calls %d distinct function(s) (of %d source files)\n", c.Symbol, len(callees), g.TotalFiles)
	}
	return codeGraphExit(c.Timeout, parentCtx, effectiveCtx, g, "calls")
}
