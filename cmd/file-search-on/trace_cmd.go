package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/richardwooding/file-search-on/internal/search"
)

// TraceCmd is the unified call-chain subcommand (issue #482): both directions
// of a symbol's call graph — its callers (who-calls) and callees (calls) — in
// one report, optionally with the transitive caller closure (impact). A
// convenience over running who-calls / calls / impact separately; reuses the
// same CodeGraph.
type TraceCmd struct {
	Symbol string `arg:"" help:"Function / method name to trace (e.g. ServeHTTP). Name-based — same-name symbols across packages are conflated."`
	codeGraphWalkFlags
	Expr        string `name:"expr" help:"CEL pre-filter. Defaults to is_source."`
	ImpactDepth int    `name:"impact-depth" default:"0" help:"Also include the transitive caller closure (blast radius) up to this many hops. 0 (default) omits it — use the 'impact' command for the full closure."`
	Output      string `short:"o" name:"output" enum:"table,json" default:"table" help:"Output format: table | json."`
}

func (c *TraceCmd) Run(ctx context.Context) error {
	g, parentCtx, effectiveCtx, cleanup, err := c.build(ctx, defaultSourceExpr(c.Expr))
	if err != nil {
		return err
	}
	defer cleanup()
	if g == nil {
		return nil
	}

	callers := g.WhoCalls(c.Symbol) // []Importer
	callees := g.Calls(c.Symbol)    // []string
	definedOn := g.OwnersOf(c.Symbol)
	var impact []search.ImpactNode
	if c.ImpactDepth > 0 {
		impact = g.Impact(c.Symbol, c.ImpactDepth)
	}

	if c.Output == "json" {
		out := map[string]any{
			"symbol":        c.Symbol,
			"defined_on":    definedOn,
			"callers":       callers,
			"callees":       callees,
			"callers_count": len(callers),
			"callees_count": len(callees),
			"total_files":   g.TotalFiles,
		}
		if c.ImpactDepth > 0 {
			out["impact"] = impact
			out["impact_count"] = len(impact)
		}
		if g.Cancelled {
			out["cancelled"] = true
			out["cancellation_reason"] = g.CancellationReason
		}
		_ = writeJSON(os.Stdout, out)
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintf(tw, "CALLERS of %s (%d):\n", c.Symbol, len(callers))
		for _, im := range callers {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\n", im.Language, im.Path)
		}
		_, _ = fmt.Fprintf(tw, "CALLEES of %s (%d):\n", c.Symbol, len(callees))
		for _, name := range callees {
			_, _ = fmt.Fprintf(tw, "  %s\n", name)
		}
		if c.ImpactDepth > 0 {
			_, _ = fmt.Fprintf(tw, "IMPACT (transitive callers, depth ≤ %d, %d):\n", c.ImpactDepth, len(impact))
			for _, n := range impact {
				_, _ = fmt.Fprintf(tw, "  d%d\t%s\n", n.Depth, n.Symbol)
			}
		}
		_ = tw.Flush()
		if len(definedOn) > 0 {
			_, _ = fmt.Fprintf(os.Stderr, "%q is a method on: %s (name-based)\n", c.Symbol, strings.Join(definedOn, ", "))
		}
		_, _ = fmt.Fprintf(os.Stderr, "traced %q across %d source files\n", c.Symbol, g.TotalFiles)
	}
	return codeGraphExit(c.Timeout, parentCtx, effectiveCtx, g, "trace")
}
